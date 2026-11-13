package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// Mise en page des fenêtres projet (ex-modes --layout-watch / --do-layout /
// --relayout du script bash) : le teamleader et les agents « cdp » en haut, les
// autres teammates dessous (actifs d'abord, ordre MRU, puis inactifs par nom),
// sur 2 lignes jusqu'à 5 teammates, 3 au-delà. Les teammates actifs sont
// éclaircis, les inactifs assombris, et l'onglet du projet s'allume tant qu'un
// agent travaille.

// Team, classement, géométrie et teintes : core/layout.go (partagés avec
// l'interface native) ; ici, leur application au tmux de la coque.

type (
	teamMember = core.TeamMember
	teamConfig = core.TeamConfig
)

func readTeam(path string) (teamConfig, error) { return core.ReadTeam(path) }

// findTeamConfig : config.json de la team du projet (core.FindTeamConfig).
func findTeamConfig(projectDir string) string { return core.FindTeamConfig(projectDir) }

// winPane : un pane d'une fenêtre projet, vu en un seul list-panes.
type winPane struct {
	id    string
	index int
	style string // option window-style du pane
}

type winState struct {
	name, statusStyle string
	server            string // PID du serveur tmux du hub (un hub recréé redonne les mêmes @N)
	w, h              int
	panes             []winPane // par index croissant
}

const winFormat = "#{pane_id}|#{pane_index}|#{window-style}|#{window_width}|#{window_height}|#{window-status-style}|#{pid}|#{window_name}"

// readWin : panes et taille lus dans le tmux du projet ; nom et style de
// l'onglet lus dans le hub (même appel en exécution directe).
func readWin(p projWin) (winState, error) {
	out, err := tout(p.t, "list-panes", "-t", p.win, "-F", winFormat)
	if err != nil {
		return winState{}, err
	}
	var ws winState
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "|", 8)
		if len(f) != 8 || f[0] == "" {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		ws.panes = append(ws.panes, winPane{f[0], idx, f[2]})
		ws.w, _ = strconv.Atoi(f[3])
		ws.h, _ = strconv.Atoi(f[4])
		ws.statusStyle, ws.server, ws.name = f[5], f[6], f[7]
	}
	if len(ws.panes) == 0 {
		return ws, fmt.Errorf("fenêtre %s sans pane", p.win)
	}
	if !p.direct() {
		// display-message réussit même sur une fenêtre disparue (il répond alors
		// pour une autre) : l'identifiant renvoyé doit être celui demandé
		tab, err := tout(hub, "display-message", "-p", "-t", p.hubWin, "#{window_id}|#{window-status-style}|#{pid}|#{window_name}")
		if err != nil {
			return ws, err
		}
		f := strings.SplitN(tab, "|", 4)
		if len(f) != 4 || strings.HasPrefix(p.hubWin, "@") && f[0] != p.hubWin {
			return ws, fmt.Errorf("fenêtre %s fermée", p.hubWin)
		}
		ws.statusStyle, ws.server, ws.name = f[1], f[2], f[3]
	}
	sort.Slice(ws.panes, func(i, j int) bool { return ws.panes[i].index < ws.panes[j].index })
	return ws, nil
}

// leader : le teamleader est le premier pane créé de la fenêtre (plus petit
// %id) — ni le pane actif, ni l'index, qu'un teammate peut occuper (#90).
func (ws winState) leader() string {
	var b strings.Builder
	for _, p := range ws.panes {
		b.WriteString(p.id + "\n")
	}
	id, _ := core.LeaderOf(b.String())
	return id
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
func setPaneColor(p projWin, ws winState, pane, color string) {
	want := "bg=colour" + color
	if ws.styleOf(pane) != want {
		_ = p.t.Run("set-option", "-p", "-t", pane, "window-style", want)
	}
}

// brightenColor / darkenColor : core.BrightenColor / core.DarkenColor.
func brightenColor(c string, step int) string { return core.BrightenColor(c, step) }
func darkenColor(c string, step int) string   { return core.DarkenColor(c, step) }

// ── Géométrie ─────────────────────────────────────────────────────────────

// layoutChecksum : somme de contrôle des chaînes de layout tmux (core.LayoutChecksum).
func layoutChecksum(s string) string { return core.LayoutChecksum(s) }

// layoutString : leader (et cdp) en haut ; ≤ 5 teammates sur une 2e ligne,
// au-delà sur deux lignes (core.LayoutString).
func layoutString(cfg core.Config, w, h int, top, bot []int) string {
	return core.LayoutString(cfg, w, h, top, bot)
}

// ── Classement ────────────────────────────────────────────────────────────

func orderPath(win string) string {
	return filepath.Join(os.TempDir(), "claude_layout_"+strings.TrimPrefix(win, "@")+".order")
}

// classify : panes du haut (leader puis cdp) et du bas (core.Classify).
func classify(ws winState, leader string, tc *teamConfig, orderFile string) (top, bot []string) {
	ids := make([]string, len(ws.panes))
	for i, p := range ws.panes {
		ids[i] = p.id
	}
	return core.Classify(ids, leader, tc, orderFile)
}

// doLayout réordonne physiquement les panes (select-layout n'applique que la
// géométrie, dans l'ordre des index existants : il faut des swap-pane), puis
// applique la géométrie et la couleur de base du projet. Un verrou par fenêtre
// sérialise le watcher et un relayout manuel.
func doLayout(cfg core.Config, pw projWin, leader, teamPath string) {
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "claude_layout_"+strings.TrimPrefix(pw.hubWin, "@")+".lock"),
		os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer f.Close()
		_ = platform.LockFile(f)
	}
	ws, err := readWin(pw)
	if err != nil || len(ws.panes) <= 1 {
		return
	}
	var tc *teamConfig
	if teamPath != "" {
		if t, err := readTeam(teamPath); err == nil {
			tc = &t
		}
	}
	top, bot := classify(ws, leader, tc, orderPath(pw.hubWin))

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
		_ = pw.t.Run("swap-pane", "-d", "-s", p, "-t", cur)
		at[i], at[j], pos[p], pos[cur] = p, cur, i, j
	}

	topIdx, botIdx := idxs[:len(top)], idxs[len(top):len(want)]
	_ = pw.t.Run("select-layout", "-t", pw.win, layoutString(cfg, ws.w, ws.h, topIdx, botIdx))

	color := cfg.ProjectColor(ws.name)
	for _, p := range want {
		setPaneColor(pw, ws, p, color)
	}
}

// ── Surveillance ──────────────────────────────────────────────────────────

func activeSignature(ws winState, tc teamConfig) string { return core.ActiveSignature(tc) }

// styleBusy : titre de l'onglet allumé (couleur du projet éclaircie) tant qu'un
// agent travaille, indépendamment du style focus/non-focus.
func styleBusy(cfg core.Config, p projWin, ws winState, busy bool, color string) {
	want := "fg=colour242,bg=colour236"
	if busy {
		want = "bold,fg=colour232,bg=colour" + brightenColor(color, cfg.BrightenStep)
	}
	if ws.statusStyle != want {
		_ = hub.Run("set-window-option", "-t", p.hubWin, "window-status-style", want)
	}
}

// RunLayoutWatch tourne en arrière-plan pour une fenêtre projet (lancé par
// launchProject) et s'arrête avec elle.
func RunLayoutWatch(win, leader, projectDir string) {
	cfg := core.LoadConfig()
	pw := projectOf(win)
	prevCount, prevSig, prevRelayout := 1, "", ""
	beat := 0
	server := ""
	for {
		time.Sleep(time.Second)
		ws, err := readWin(pw)
		if err != nil {
			return // fenêtre fermée
		}
		// hub tué puis recréé : il redonne les mêmes @N à d'autres fenêtres (ou à
		// la même, rattachée, qui a son propre watcher) — ce watcher-ci s'arrête
		if server == "" {
			server = ws.server
		} else if ws.server != server {
			return
		}
		// le leader passé au lancement peut être faux (pane actif à
		// l'attachement) : relu de la fenêtre à chaque tour
		if l := ws.leader(); l != "" {
			leader = l
		}
		if !pw.direct() {
			// propriétaire (#48) : battement de cœur toutes les 5 s ; si un autre
			// launcher a pris la main, ce watcher s'efface
			if beat++; beat%5 == 1 && !heartbeat(pw) {
				return
			}
			// Ctrl+B R dans le tmux du conteneur incrémente @cl_relayout (bootstrap.sh)
			if r, _ := tout(pw.t, "show-options", "-wqv", "-t", pw.win, "@cl_relayout"); r != prevRelayout {
				if prevRelayout != "" || r != "" {
					if path := findTeamConfig(projectDir); path != "" && len(ws.panes) > 1 {
						doLayout(cfg, pw, leader, path)
					}
				}
				prevRelayout = r
			}
		}
		if path := findTeamConfig(projectDir); path != "" {
			tc, _ := readTeam(path)
			color := cfg.ProjectColor(ws.name)
			// avant la teinte : doLayout remet tous les panes à la couleur de base
			if sig := activeSignature(ws, tc); sig != prevSig {
				prevSig = sig
				doLayout(cfg, pw, leader, path)
				if ws, err = readWin(pw); err != nil {
					return
				}
			}
			for _, m := range tc.Members {
				if m.PaneID == "" || m.PaneID == leader || !ws.has(m.PaneID) {
					continue
				}
				if m.IsActive {
					setPaneColor(pw, ws, m.PaneID, brightenColor(color, cfg.BrightenStep))
				} else {
					setPaneColor(pw, ws, m.PaneID, darkenColor(color, cfg.DarkenStep))
				}
			}
			styleBusy(cfg, pw, ws, prevSig != "", color)
		} else {
			styleBusy(cfg, pw, ws, false, "")
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
		doLayout(cfg, pw, leader, team)
	}
}

// RunRelayout : mise en page forcée d'une fenêtre (liaison tmux, ex. Ctrl+b R :
// bind R run-shell "claude-launcher-ng2 relayout '#{window_id}'").
func RunRelayout(win string) {
	pw := projectOf(win)
	ws, err := readWin(pw)
	if err != nil {
		return
	}
	// messages dans le hub : c'est là que l'utilisateur regarde
	if len(ws.panes) <= 1 {
		_ = hub.Run("display-message", "-t", pw.hubWin, "Relayout : pas de team active (1 seul pane)")
		return
	}
	leader := ws.leader()
	dir, _ := tout(pw.t, "display-message", "-p", "-t", leader, "#{pane_current_path}")
	path := findTeamConfig(dir)
	if path == "" {
		_ = hub.Run("display-message", "-t", pw.hubWin, "Relayout : aucune team trouvée pour "+dir)
		return
	}
	doLayout(core.LoadConfig(), pw, leader, path)
	_ = hub.Run("display-message", "-t", pw.hubWin, "Relayout appliqué ("+filepath.Base(filepath.Dir(path))+")")
}
