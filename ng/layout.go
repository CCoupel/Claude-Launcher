package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Mise en page des fenêtres projet (ex-modes --layout-watch / --do-layout /
// --relayout du script bash) : le teamleader et les agents « cdp » en haut, les
// autres teammates dessous (actifs d'abord, ordre MRU, puis inactifs par nom),
// sur 2 lignes jusqu'à 5 teammates, 3 au-delà. Les teammates actifs sont
// éclaircis, les inactifs assombris, et l'onglet du projet s'allume tant qu'un
// agent travaille.

type teamMember struct {
	Name      string `json:"name"`
	AgentType string `json:"agentType"`
	PaneID    string `json:"tmuxPaneId"`
	IsActive  bool   `json:"isActive"`
	Cwd       string `json:"cwd"`
}

type teamConfig struct {
	Members []teamMember `json:"members"`
}

func readTeam(path string) (teamConfig, error) {
	var tc teamConfig
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &tc)
	}
	return tc, err
}

func (tc teamConfig) leaderCwd() string {
	for _, m := range tc.Members {
		if m.Name == "team-lead" {
			return m.Cwd
		}
	}
	return ""
}

func normPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(strings.TrimRight(p, "/"))
}

// findTeamConfig : config.json de la team du projet. Une session coupée laisse
// sa team quand la reprise en crée une autre : la plus récemment modifiée gagne.
func findTeamConfig(projectDir string) string {
	home, _ := os.UserHomeDir()
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "teams", "*", "config.json"))
	want := normPath(projectDir)
	best, bestT := "", time.Time{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		tc, err := readTeam(f)
		if err != nil {
			continue
		}
		if cwd := tc.leaderCwd(); cwd == "" || normPath(cwd) != want {
			continue
		}
		if best == "" || st.ModTime().After(bestT) {
			best, bestT = f, st.ModTime()
		}
	}
	return best
}

// winPane : un pane d'une fenêtre projet, vu en un seul list-panes.
type winPane struct {
	id    string
	index int
	style string // option window-style du pane
}

type winState struct {
	name, statusStyle string
	w, h              int
	panes             []winPane // par index croissant
}

const winFormat = "#{pane_id}|#{pane_index}|#{window-style}|#{window_width}|#{window_height}|#{window-status-style}|#{window_name}"

func readWin(win string) (winState, error) {
	out, err := tmuxOut("list-panes", "-t", win, "-F", winFormat)
	if err != nil {
		return winState{}, err
	}
	var ws winState
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "|", 7)
		if len(f) != 7 || f[0] == "" {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		ws.panes = append(ws.panes, winPane{f[0], idx, f[2]})
		ws.w, _ = strconv.Atoi(f[3])
		ws.h, _ = strconv.Atoi(f[4])
		ws.statusStyle, ws.name = f[5], f[6]
	}
	if len(ws.panes) == 0 {
		return ws, fmt.Errorf("fenêtre %s sans pane", win)
	}
	sort.Slice(ws.panes, func(i, j int) bool { return ws.panes[i].index < ws.panes[j].index })
	return ws, nil
}

func (ws winState) has(id string) bool {
	for _, p := range ws.panes {
		if p.id == id {
			return true
		}
	}
	return false
}

func (ws winState) styleOf(id string) string {
	for _, p := range ws.panes {
		if p.id == id {
			return p.style
		}
	}
	return ""
}

// setPaneColor : ne réécrit que si l'état tmux réel diffère. set-option -p (et
// non select-pane -P) : colore le pane sans lui donner le focus.
func setPaneColor(ws winState, pane, color string) {
	want := "bg=colour" + color
	if ws.styleOf(pane) != want {
		_ = dtmux("set-option", "-p", "-t", pane, "window-style", want).Run()
	}
}

// brightenColor / darkenColor : variante plus claire / plus sombre d'une couleur
// tmux 256 en gardant sa teinte. Éclaircir n'allume pas un canal éteint (saut
// disproportionné) ; assombrir descend jusqu'à 0 (palette déjà proche du noir).
func brightenColor(c string, step int) string { return shiftColor(c, step, false) }
func darkenColor(c string, step int) string   { return shiftColor(c, -step, true) }

func shiftColor(c string, step int, toZero bool) string {
	n, err := strconv.Atoi(c)
	if err != nil {
		return c
	}
	clamp := func(v, lo, hi int) int { return min(hi, max(lo, v)) }
	switch {
	case n >= 16 && n <= 231:
		i := n - 16
		rgb := []int{i / 36, i / 6 % 6, i % 6}
		for k, v := range rgb {
			if v > 0 {
				lo := 1
				if toZero {
					lo = 0
				}
				rgb[k] = clamp(v+step, lo, 5)
			}
		}
		return strconv.Itoa(16 + 36*rgb[0] + 6*rgb[1] + rgb[2])
	case n >= 232 && n <= 255:
		return strconv.Itoa(clamp(n+2*step, 232, 255))
	}
	return c
}

// ── Géométrie ─────────────────────────────────────────────────────────────

// layoutChecksum : somme de contrôle des chaînes de layout tmux (layout_checksum).
func layoutChecksum(s string) string {
	csum := 0
	for i := 0; i < len(s); i++ {
		csum = ((csum >> 1) + ((csum & 1) << 15) + int(s[i])) & 0xFFFF
	}
	return fmt.Sprintf("%04x", csum)
}

// buildRow : n panes côte à côte (chaque séparateur prend une colonne).
func buildRow(w, h, x, y int, idxs []int) string {
	if len(idxs) == 1 {
		return fmt.Sprintf("%dx%d,%d,%d,%d", w, h, x, y, idxs[0])
	}
	n := len(idxs)
	colW := (w - n + 1) / n
	rem := w - colW*n - (n - 1)
	cells := make([]string, n)
	cx := x
	for i, idx := range idxs {
		pw := colW
		if i == n-1 {
			pw += rem
		}
		cells[i] = fmt.Sprintf("%dx%d,%d,%d,%d", pw, h, cx, y, idx)
		cx += pw + 1
	}
	return fmt.Sprintf("%dx%d,%d,%d{%s}", w, h, x, y, strings.Join(cells, ","))
}

// layoutString : leader (et cdp) en haut ; ≤ 5 teammates sur une 2e ligne,
// au-delà sur deux lignes (la dernière reçoit le reliquat de hauteur).
func layoutString(cfg Config, w, h int, top, bot []int) string {
	var body string
	switch {
	case len(bot) == 0:
		body = buildRow(w, h, 0, 0, top)
	case len(bot) <= 5:
		topH := h * cfg.Layout2RowLeaderPct / 100
		botH := h - topH - 1
		body = fmt.Sprintf("%dx%d,0,0[%s,%s]", w, h,
			buildRow(w, topH, 0, 0, top), buildRow(w, botH, 0, topH+1, bot))
	default:
		leadH := h * cfg.Layout3RowLeaderNum / cfg.Layout3RowDen
		midH := h * cfg.Layout3RowMidNum / cfg.Layout3RowDen
		botH := h - leadH - midH - 2
		nMid := (len(bot) + 1) / 2
		body = fmt.Sprintf("%dx%d,0,0[%s,%s,%s]", w, h,
			buildRow(w, leadH, 0, 0, top),
			buildRow(w, midH, 0, leadH+1, bot[:nMid]),
			buildRow(w, botH, 0, leadH+midH+2, bot[nMid:]))
	}
	return layoutChecksum(body) + "," + body
}

// ── Classement ────────────────────────────────────────────────────────────

func orderPath(win string) string {
	return filepath.Join(os.TempDir(), "claude_layout_"+strings.TrimPrefix(win, "@")+".order")
}

// classify : panes du haut (leader puis cdp) et du bas (teammates actifs en
// ordre MRU — un agent qui devient actif passe en tête —, inactifs par nom, puis
// panes sans agent). L'ordre MRU est conservé entre deux appels dans orderPath.
func classify(ws winState, leader string, tc *teamConfig, orderFile string) (top, bot []string) {
	top = []string{leader}
	seen := map[string]bool{leader: true}
	if tc != nil {
		var prev []string
		if b, err := os.ReadFile(orderFile); err == nil {
			prev = strings.Fields(string(b))
		}
		nameOf, activeOf := map[string]string{}, map[string]bool{}
		var bots []string
		for _, m := range tc.Members {
			if m.PaneID == "" || m.PaneID == leader || !ws.has(m.PaneID) || seen[m.PaneID] {
				continue
			}
			seen[m.PaneID] = true
			if m.AgentType == "cdp" {
				top = append(top, m.PaneID)
				continue
			}
			bots = append(bots, m.PaneID)
			nameOf[m.PaneID], activeOf[m.PaneID] = m.Name, m.IsActive
		}
		byName := func(ids []string) {
			sort.SliceStable(ids, func(i, j int) bool {
				a, b := nameOf[ids[i]]+"|"+ids[i], nameOf[ids[j]]+"|"+ids[j]
				return a < b
			})
		}
		wasActive := map[string]bool{}
		var kept []string
		for _, p := range prev {
			wasActive[p] = true
			if activeOf[p] {
				kept = append(kept, p)
			}
		}
		var fresh, idle []string
		for _, p := range bots {
			switch {
			case !activeOf[p]:
				idle = append(idle, p)
			case !wasActive[p]:
				fresh = append(fresh, p)
			}
		}
		byName(fresh)
		byName(idle)
		active := append(fresh, kept...)
		_ = os.WriteFile(orderFile, []byte(strings.Join(active, "\n")), 0o644)
		bot = append(active, idle...)
		for _, p := range bot {
			seen[p] = true
		}
	}
	for _, p := range ws.panes {
		if !seen[p.id] {
			bot = append(bot, p.id) // panes ouverts à la main
		}
	}
	return top, bot
}

// doLayout réordonne physiquement les panes (select-layout n'applique que la
// géométrie, dans l'ordre des index existants : il faut des swap-pane), puis
// applique la géométrie et la couleur de base du projet. Un verrou par fenêtre
// sérialise le watcher et un relayout manuel.
func doLayout(cfg Config, win, leader, teamPath string) {
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "claude_layout_"+strings.TrimPrefix(win, "@")+".lock"),
		os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer f.Close()
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	ws, err := readWin(win)
	if err != nil || len(ws.panes) <= 1 {
		return
	}
	var tc *teamConfig
	if teamPath != "" {
		if t, err := readTeam(teamPath); err == nil {
			tc = &t
		}
	}
	top, bot := classify(ws, leader, tc, orderPath(win))

	// échanges : la position i de l'ordre voulu reçoit l'index physique i
	at := make([]string, len(ws.panes)) // position -> pane
	pos := map[string]int{}
	idxs := make([]int, len(ws.panes))
	for i, p := range ws.panes {
		at[i], pos[p.id], idxs[i] = p.id, i, p.index
	}
	want := append(append([]string{}, top...), bot...)
	for i, p := range want {
		if i >= len(at) || at[i] == p {
			continue
		}
		cur, j := at[i], pos[p]
		// -d : ne jamais déplacer le focus (seul le teamleader le garde)
		_ = dtmux("swap-pane", "-d", "-s", p, "-t", cur).Run()
		at[i], at[j], pos[p], pos[cur] = p, cur, i, j
	}

	topIdx, botIdx := idxs[:len(top)], idxs[len(top):len(want)]
	_ = dtmux("select-layout", "-t", win, layoutString(cfg, ws.w, ws.h, topIdx, botIdx)).Run()

	color := cfg.projectColor(ws.name)
	for _, p := range want {
		setPaneColor(ws, p, color)
	}
}

// ── Surveillance ──────────────────────────────────────────────────────────

func activeSignature(ws winState, tc teamConfig) string {
	var ids []string
	for _, m := range tc.Members {
		if m.PaneID != "" && m.IsActive {
			ids = append(ids, m.PaneID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// styleBusy : titre de l'onglet allumé (couleur du projet éclaircie) tant qu'un
// agent travaille, indépendamment du style focus/non-focus.
func styleBusy(cfg Config, win string, ws winState, busy bool, color string) {
	want := "fg=colour242,bg=colour236"
	if busy {
		want = "bold,fg=colour232,bg=colour" + brightenColor(color, cfg.BrightenStep)
	}
	if ws.statusStyle != want {
		_ = dtmux("set-window-option", "-t", win, "window-status-style", want).Run()
	}
}

// runLayoutWatch tourne en arrière-plan pour une fenêtre projet (lancé par
// launchProject) et s'arrête avec elle.
func runLayoutWatch(win, leader, projectDir string) {
	cfg := loadConfig()
	prevCount, prevSig := 1, ""
	for {
		time.Sleep(time.Second)
		ws, err := readWin(win)
		if err != nil {
			return // fenêtre fermée
		}
		if path := findTeamConfig(projectDir); path != "" {
			tc, _ := readTeam(path)
			color := cfg.projectColor(ws.name)
			// avant la teinte : doLayout remet tous les panes à la couleur de base
			if sig := activeSignature(ws, tc); sig != prevSig {
				prevSig = sig
				doLayout(cfg, win, leader, path)
				if ws, err = readWin(win); err != nil {
					return
				}
			}
			for _, m := range tc.Members {
				if m.PaneID == "" || m.PaneID == leader || !ws.has(m.PaneID) {
					continue
				}
				if m.IsActive {
					setPaneColor(ws, m.PaneID, brightenColor(color, cfg.BrightenStep))
				} else {
					setPaneColor(ws, m.PaneID, darkenColor(color, cfg.DarkenStep))
				}
			}
			styleBusy(cfg, win, ws, prevSig != "", color)
		} else {
			styleBusy(cfg, win, ws, false, "")
		}

		n := len(ws.panes)
		if n == prevCount {
			continue
		}
		prevCount = n
		if n <= 1 {
			continue
		}
		// un pane de plus : attendre que la team ait inscrit tous les agents
		// (le leader n'a pas de tmuxPaneId)
		team := ""
		for try := 0; try < 10 && team == ""; try++ {
			time.Sleep(800 * time.Millisecond)
			if path := findTeamConfig(projectDir); path != "" {
				if tc, err := readTeam(path); err == nil {
					c := 0
					for _, m := range tc.Members {
						if m.PaneID != "" {
							c++
						}
					}
					if c >= n-1 {
						team = path
					}
				}
			}
		}
		doLayout(cfg, win, leader, team)
	}
}

// runRelayout : mise en page forcée d'une fenêtre (liaison tmux, ex. Ctrl+b R :
// bind R run-shell "claude-launcher-ng relayout '#{window_id}'").
func runRelayout(win string) {
	ws, err := readWin(win)
	if err != nil {
		return
	}
	if len(ws.panes) <= 1 {
		_ = dtmux("display-message", "-t", win, "Relayout : pas de team active (1 seul pane)").Run()
		return
	}
	leader := ws.panes[0].id
	dir, _ := tmuxOut("display-message", "-p", "-t", leader, "#{pane_current_path}")
	path := findTeamConfig(dir)
	if path == "" {
		_ = dtmux("display-message", "-t", win, "Relayout : aucune team trouvée pour "+dir).Run()
		return
	}
	doLayout(loadConfig(), win, leader, path)
	_ = dtmux("display-message", "-t", win, "Relayout appliqué ("+filepath.Base(filepath.Dir(path))+")").Run()
}
