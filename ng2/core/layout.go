package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Mise en page des teams, commune aux interfaces (coque tmux : ui/tmux/layout.go,
// interface native : ui/desktop) : le teamleader et les agents « cdp » en haut,
// les autres teammates dessous (actifs d'abord, ordre MRU, puis inactifs par
// nom), sur une ligne jusqu'à 5 teammates, deux au-delà. Les teammates actifs
// sont éclaircis, les inactifs assombris (couleur du projet).

type TeamMember struct {
	Name      string `json:"name"`
	AgentType string `json:"agentType"`
	PaneID    string `json:"tmuxPaneId"`
	IsActive  bool   `json:"isActive"`
	Cwd       string `json:"cwd"`
}

type TeamConfig struct {
	Members []TeamMember `json:"members"`
}

func ReadTeam(path string) (TeamConfig, error) {
	var tc TeamConfig
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &tc)
	}
	return tc, err
}

func (tc TeamConfig) LeaderCwd() string {
	for _, m := range tc.Members {
		if m.Name == "team-lead" {
			return m.Cwd
		}
	}
	return ""
}

func NormPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(strings.TrimRight(p, "/"))
}

// FindTeamConfig : config.json de la team du projet. Une session coupée laisse
// sa team quand la reprise en crée une autre : la plus récemment modifiée gagne.
func FindTeamConfig(projectDir string) string {
	files, _ := filepath.Glob(filepath.Join(ClaudeDir(), "teams", "*", "config.json"))
	want := NormPath(ClaudePath(projectDir)) // cwd vu par claude (conteneur : chemin des backends)
	best, bestT := "", time.Time{}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		tc, err := ReadTeam(f)
		if err != nil {
			continue
		}
		if cwd := tc.LeaderCwd(); cwd == "" || NormPath(cwd) != want {
			continue
		}
		if best == "" || st.ModTime().After(bestT) {
			best, bestT = f, st.ModTime()
		}
	}
	return best
}

// BrightenColor / DarkenColor : variante plus claire / plus sombre d'une couleur
// tmux 256 en gardant sa teinte. Éclaircir n'allume pas un canal éteint (saut
// disproportionné) ; assombrir descend jusqu'à 0 (palette déjà proche du noir).
func BrightenColor(c string, step int) string { return ShiftColor(c, step, false) }
func DarkenColor(c string, step int) string   { return ShiftColor(c, -step, true) }

func ShiftColor(c string, step int, toZero bool) string {
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

// Colour256Hex : « #rrggbb » d'une couleur tmux 256 (« 24 », « colour24 »),
// pour les interfaces hors terminal ; "" si la couleur n'est pas reconnue.
func Colour256Hex(c string) string {
	n, err := strconv.Atoi(strings.TrimPrefix(c, "colour"))
	if err != nil || n < 0 || n > 255 {
		return ""
	}
	var r, g, b int
	switch {
	case n < 16:
		base := [16][3]int{{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
			{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}
		r, g, b = base[n][0], base[n][1], base[n][2]
	case n < 232:
		lv := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + 40*v
		}
		i := n - 16
		r, g, b = lv(i/36), lv(i/6%6), lv(i%6)
	default:
		v := 8 + 10*(n-232)
		r, g, b = v, v, v
	}
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// ── Géométrie ─────────────────────────────────────────────────────────────

// TeamRow : une ligne de la mise en page : N panes côte à côte, sur Num/Den de
// la hauteur (Den = 0 : le reste de la hauteur, après les autres lignes).
type TeamRow struct {
	N, Num, Den int
}

// TeamRows : lignes de la mise en page d'une team de nTop panes en haut (leader
// et cdp) et nBot teammates : ≤ 5 teammates, le haut sur Layout2RowLeaderPct %
// de la hauteur et les teammates sur une ligne ; au-delà, trois lignes
// (Layout3RowLeaderNum/Den, Layout3RowMidNum/Den, le reste), la ligne du milieu
// recevant ⌈n/2⌉ teammates. C'est la règle de la coque tmux (LayoutString) ;
// l'interface native l'applique aussi.
func TeamRows(cfg Config, nTop, nBot int) []TeamRow {
	switch {
	case nBot == 0:
		return []TeamRow{{N: nTop}}
	case nBot <= 5:
		return []TeamRow{{N: nTop, Num: cfg.Layout2RowLeaderPct, Den: 100}, {N: nBot}}
	}
	nMid := (nBot + 1) / 2
	return []TeamRow{{N: nTop, Num: cfg.Layout3RowLeaderNum, Den: cfg.Layout3RowDen},
		{N: nMid, Num: cfg.Layout3RowMidNum, Den: cfg.Layout3RowDen}, {N: nBot - nMid}}
}

// LayoutChecksum : somme de contrôle des chaînes de layout tmux (layout_checksum).
func LayoutChecksum(s string) string {
	csum := 0
	for i := 0; i < len(s); i++ {
		csum = ((csum >> 1) + ((csum & 1) << 15) + int(s[i])) & 0xFFFF
	}
	return fmt.Sprintf("%04x", csum)
}

// BuildRow : n panes côte à côte (chaque séparateur prend une colonne).
func BuildRow(w, h, x, y int, idxs []int) string {
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

// LayoutString : chaîne de layout tmux des lignes de TeamRows sur w×h cellules
// (chaque séparateur de lignes prend une ligne, la dernière reçoit le reliquat
// de hauteur) ; top et bot : index tmux des panes du haut et des teammates.
func LayoutString(cfg Config, w, h int, top, bot []int) string {
	rows := TeamRows(cfg, len(top), len(bot))
	var body string
	if len(rows) == 1 {
		body = BuildRow(w, h, 0, 0, top)
	} else {
		panes := append(append([]int{}, top...), bot...)
		cells := make([]string, len(rows))
		y, used := 0, 0
		for i, r := range rows {
			rh := h - used - (len(rows) - 1) // dernière ligne : le reste
			if r.Den > 0 {
				rh = h * r.Num / r.Den
			}
			cells[i] = BuildRow(w, rh, 0, y, panes[:r.N])
			panes = panes[r.N:]
			y += rh + 1
			used += rh
		}
		body = fmt.Sprintf("%dx%d,0,0[%s]", w, h, strings.Join(cells, ","))
	}
	return LayoutChecksum(body) + "," + body
}

// ── Classement ────────────────────────────────────────────────────────────

// Classify : panes du haut (leader puis cdp) et du bas (teammates actifs en
// ordre MRU — un agent qui devient actif passe en tête —, inactifs par nom, puis
// panes sans agent). panes : identifiants des panes de la fenêtre, par index
// croissant. L'ordre MRU est conservé entre deux appels dans orderFile.
func Classify(panes []string, leader string, tc *TeamConfig, orderFile string) (top, bot []string) {
	has := map[string]bool{}
	for _, p := range panes {
		has[p] = true
	}
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
			if m.PaneID == "" || m.PaneID == leader || !has[m.PaneID] || seen[m.PaneID] {
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
	for _, p := range panes {
		if !seen[p] {
			bot = append(bot, p) // panes ouverts à la main
		}
	}
	return top, bot
}

// ActiveSignature : panes des agents qui travaillent (vide : aucun ; l'onglet
// du projet est allumé tant qu'elle n'est pas vide).
func ActiveSignature(tc TeamConfig) string {
	var ids []string
	for _, m := range tc.Members {
		if m.PaneID != "" && m.IsActive {
			ids = append(ids, m.PaneID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// PaneShade : couleur tmux 256 d'un pane de la team : couleur du projet pour le
// leader (et les panes sans agent), éclaircie pour un teammate actif, assombrie
// pour un inactif.
func PaneShade(cfg Config, color string, m *TeamMember, leader bool) string {
	switch {
	case leader || m == nil:
		return color
	case m.IsActive:
		return BrightenColor(color, cfg.BrightenStep)
	}
	return DarkenColor(color, cfg.DarkenStep)
}
