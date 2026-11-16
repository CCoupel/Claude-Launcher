package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #153 (LOT-5) — organisation « Leader à gauche » de la fenêtre d'un projet, coque Linux, sur un tmux isolé
// (CL_INNER_SOCKET, faux claude = sleep). Contrat : contracts/desktop-dispatch.md, section LOT-5.
// La mise en page est appliquée par `RunRelayout` (Ctrl+B R) ou par le surveillant, avec la configuration du
// launcher qui a la main (config.conf : LAYOUT_MODE, LAYOUT_LEFT_LEADER_PCT, PROJECT_LAYOUTS).

type rect struct {
	ID         string
	X, Y, W, H int
}

func (r rect) right() int  { return r.X + r.W }
func (r rect) bottom() int { return r.Y + r.H }

// l153Win : fenêtre « proj » du hub avec un leader, nCDP agents cdp et n teammates tous actifs (jamais cachés),
// la team écrite dans ~/.claude ; window-style / titres non posés (appelant). Rend la fenêtre, le leader, les
// panes des cdp et ceux des teammates (dans l'ordre de création) et le chemin de la team.
func l153Win(t *testing.T, marker string, nCDP, n int) (key, win, leader string, cdps, mates []string, teamPath, proj string) {
	t.Helper()
	key, proj = "proj", t.TempDir()
	if out, err := dtmux("-f", "/dev/null", "new-session", "-d", "-s", hubSession, "-n", key, "-c", proj, "-x", "200", "-y", "50", "sleep "+marker).CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	win = dt(t, "display-message", "-p", "-t", exactW(hubSession, key), "#{window_id}")
	leader = dt(t, "display-message", "-p", "-t", win, "#{pane_id}")
	split := func() string {
		id := dt(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sleep "+marker)
		dt(t, "select-layout", "-t", win, "tiled") // de la place pour le suivant (jusqu'à 12 panes)
		return id
	}
	for i := 0; i < nCDP; i++ {
		cdps = append(cdps, split())
	}
	for i := 0; i < n; i++ {
		mates = append(mates, split())
	}
	members := []string{fmt.Sprintf(`{"name":"team-lead","agentType":"team-lead","cwd":%q}`, proj)}
	for i, p := range cdps {
		members = append(members, fmt.Sprintf(`{"name":"cdp%d","agentType":"cdp","tmuxPaneId":%q}`, i, p))
	}
	for i, p := range mates {
		members = append(members, fmt.Sprintf(`{"name":"dev-%02d","agentType":"dev-%02d","tmuxPaneId":%q,"isActive":true}`, i, i, p))
	}
	teamPath = filepath.Join(core.ClaudeDir(), "teams", "s1", "config.json")
	if err := os.MkdirAll(filepath.Dir(teamPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(teamPath, []byte(`{"members":[`+strings.Join(members, ",")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

// l153Conf : config.conf du bac à sable (lue par core.LoadConfig).
func l153Conf(t *testing.T, content string) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	l153WriteConf(t, content)
}

func l153WriteConf(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(core.ConfigPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// date de modification strictement postérieure (granularité d'une seconde des disques réseau)
	now := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(core.ConfigPath(), now, now)
}

func geometry(t *testing.T, win string) (cols, rows int, rs map[string]rect) {
	t.Helper()
	out := dt(t, "list-panes", "-t", win, "-F", "#{pane_id} #{pane_left} #{pane_top} #{pane_width} #{pane_height} #{window_width} #{window_height}")
	rs = map[string]rect{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 7 {
			t.Fatalf("list-panes : %q", l)
		}
		n := func(i int) int { v, _ := strconv.Atoi(f[i]); return v }
		rs[f[0]] = rect{f[0], n(1), n(2), n(3), n(4)}
		cols, rows = n(5), n(6)
	}
	return
}

func noOverlap(t *testing.T, rs map[string]rect) {
	t.Helper()
	var all []rect
	for _, r := range rs {
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("pane vide %+v", r)
		}
		all = append(all, r)
	}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			a, b := all[i], all[j]
			if a.X < b.right() && b.X < a.right() && a.Y < b.bottom() && b.Y < a.bottom() {
				t.Errorf("panes %s et %s se chevauchent : %+v %+v", a.ID, b.ID, a, b)
			}
		}
	}
}

// checkLeft : disposition « leader à gauche » de n teammates (et des cdp empilés avec le leader), en tolérant
// la ligne de titre tmux (#152 : le pane du haut de chaque colonne perd sa première ligne).
func checkLeft(t *testing.T, win, leader string, cdps, mates []string, pct int) {
	t.Helper()
	cols, rows, g := geometry(t, win)
	noOverlap(t, g)
	n := len(mates)
	L := g[leader]
	if L.X != 0 || L.Y > 1 {
		t.Errorf("leader %+v : attendu en haut à gauche", L)
	}
	if want := cols * pct / 100; L.W < want-3 || L.W > want+3 {
		t.Errorf("leader de %d colonnes sur %d, attendu %d%% (≈ %d)", L.W, cols, pct, want)
	}
	if len(cdps) == 0 && L.bottom() < rows {
		t.Errorf("leader %+v ne couvre pas toute la hauteur (%d lignes)", L, rows)
	}
	// les cdp sont empilés sous le leader, dans la colonne de gauche
	prev := L
	for _, c := range cdps {
		r := g[c]
		if r.X != 0 || r.W != L.W || r.Y < prev.bottom() {
			t.Errorf("cdp %+v : attendu empilé sous %+v dans la colonne de gauche", r, prev)
		}
		prev = r
	}
	if len(cdps) > 0 && prev.bottom() < rows {
		t.Errorf("colonne de gauche s'arrête à %d, fenêtre de %d lignes", prev.bottom(), rows)
	}
	if n == 0 {
		return
	}
	// teammates à droite : une colonne jusqu'à 4, deux colonnes égales à partir de 5 (la première : ⌈n/2⌉)
	colsX := map[int][]rect{}
	for _, m := range mates {
		r := g[m]
		if r.X < L.right() {
			t.Errorf("teammate %+v à gauche du bord du leader (%d)", r, L.right())
		}
		colsX[r.X] = append(colsX[r.X], r)
	}
	wantCols, first := 1, n
	if n >= 5 {
		wantCols, first = 2, (n+1)/2
	}
	if len(colsX) != wantCols {
		t.Fatalf("%d teammates : %d colonne(s) %v, attendu %d", n, len(colsX), colsX, wantCols)
	}
	var xs []int
	for x := range colsX {
		xs = append(xs, x)
	}
	if len(xs) == 2 && xs[0] > xs[1] {
		xs[0], xs[1] = xs[1], xs[0]
	}
	if got := len(colsX[xs[0]]); got != first {
		t.Errorf("première colonne : %d teammates, attendu %d (n=%d)", got, first, n)
	}
	for _, x := range xs {
		col := colsX[x]
		// empilés sans trou, jusqu'au bas de la fenêtre
		maxB, minY := 0, rows
		for _, r := range col {
			maxB, minY = max(maxB, r.bottom()), min(minY, r.Y)
		}
		if minY > 1 || maxB < rows {
			t.Errorf("colonne x=%d : de %d à %d, fenêtre de %d lignes", x, minY, maxB, rows)
		}
	}
	if len(xs) == 2 {
		w0, w1 := colsX[xs[0]][0].W, colsX[xs[1]][0].W
		if d := w0 - w1; d < -2 || d > 2 {
			t.Errorf("deux colonnes inégales : %d et %d", w0, w1)
		}
		if r := colsX[xs[1]][len(colsX[xs[1]])-1]; r.right() < cols-1 {
			t.Errorf("la colonne de droite s'arrête à %d sur %d", r.right(), cols)
		}
	} else if r := colsX[xs[0]][0]; r.right() < cols-1 {
		t.Errorf("la colonne s'arrête à %d sur %d colonnes", r.right(), cols)
	}
}

// Chaîne « left » pour 0 à 10 teammates, sans puis avec un agent cdp, leader à 46 % (défaut) : géométrie lue
// sur un vrai tmux après Ctrl+B R.
func TestRelayoutLeftZeroToTenTeammates(t *testing.T) {
	for _, withCDP := range []int{0, 1} {
		for n := 0; n <= 10; n++ {
			if withCDP == 1 && n == 0 {
				continue
			}
			t.Run(fmt.Sprintf("cdp=%d/n=%d", withCDP, n), func(t *testing.T) {
				marker := isoInner(t)
				l153Conf(t, "LAYOUT_MODE=left\n")
				_, win, leader, cdps, mates, _, _ := l153Win(t, marker, withCDP, n)
				RunRelayout(win)
				if n == 0 && withCDP == 0 {
					_, rows, g := geometry(t, win)
					if r := g[leader]; r.H < rows-1 || r.X != 0 {
						t.Errorf("leader seul : %+v sur %d lignes, attendu toute la fenêtre", r, rows)
					}
					return
				}
				checkLeft(t, win, leader, cdps, mates, 46)
			})
		}
	}
}

// Largeur du leader selon LAYOUT_LEFT_LEADER_PCT (bornes comprises).
func TestRelayoutLeftLeaderWidth(t *testing.T) {
	for _, pct := range []int{20, 30, 60, 80} {
		t.Run(fmt.Sprintf("pct=%d", pct), func(t *testing.T) {
			marker := isoInner(t)
			l153Conf(t, fmt.Sprintf("LAYOUT_MODE=left\nLAYOUT_LEFT_LEADER_PCT=%d\n", pct))
			_, win, leader, cdps, mates, _, _ := l153Win(t, marker, 0, 3)
			RunRelayout(win)
			checkLeft(t, win, leader, cdps, mates, pct)
		})
	}
}

// `top` (défaut) inchangé : leader seul sur la première ligne, pleine largeur ; teammates dessous.
func TestRelayoutTopUnchangedByDefault(t *testing.T) {
	for _, conf := range []string{"", "LAYOUT_MODE=top\n"} {
		marker := isoInner(t)
		l153Conf(t, conf)
		_, win, leader, _, mates, _, _ := l153Win(t, marker, 0, 3)
		RunRelayout(win)
		cols, _, g := geometry(t, win)
		noOverlap(t, g)
		if L := g[leader]; L.X != 0 || L.W != cols || L.Y > 1 {
			t.Errorf("conf %q : leader %+v, attendu pleine largeur en haut (%d colonnes)", conf, L, cols)
		}
		for _, m := range mates {
			if g[m].Y < g[leader].bottom() {
				t.Errorf("conf %q : teammate %+v au-dessus de la fin du leader", conf, g[m])
			}
		}
		dtmux("kill-server").Run()
	}
}

// Organisation effective : PROJECT_LAYOUTS[projet] > LAYOUT_MODE > top, par nom de fenêtre de projet.
func TestRelayoutEffectiveLayoutPerProject(t *testing.T) {
	cases := []struct {
		name, conf string
		left       bool
	}{
		{"global top, exception left", "LAYOUT_MODE=top\ndeclare -A PROJECT_LAYOUTS=([\"proj\"]=\"left\")\n", true},
		{"global left, exception top", "LAYOUT_MODE=left\ndeclare -A PROJECT_LAYOUTS=([\"proj\"]=\"top\")\n", false},
		{"global left, exception d'un autre projet", "LAYOUT_MODE=left\ndeclare -A PROJECT_LAYOUTS=([\"autre\"]=\"top\")\n", true},
		{"global top, exception d'un autre projet", "LAYOUT_MODE=top\ndeclare -A PROJECT_LAYOUTS=([\"autre\"]=\"left\")\n", false},
		{"valeur inconnue : retombe sur le global", "LAYOUT_MODE=left\ndeclare -A PROJECT_LAYOUTS=([\"proj\"]=\"diagonal\")\n", true},
		{"mode inconnu : top", "LAYOUT_MODE=diagonal\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			marker := isoInner(t)
			l153Conf(t, c.conf)
			_, win, leader, cdps, mates, _, _ := l153Win(t, marker, 0, 3)
			RunRelayout(win)
			cols, _, g := geometry(t, win)
			if c.left {
				checkLeft(t, win, leader, cdps, mates, 46)
			} else if L := g[leader]; L.W != cols {
				t.Errorf("leader %+v : attendu pleine largeur (top), %d colonnes", L, cols)
			}
		})
	}
}

// Seuls les panes visibles sont disposés : les teammates cachés (réserve) ne comptent ni dans le nombre de
// colonnes ni dans la géométrie ; au retour d'un pane caché la disposition suit.
func TestRelayoutLeftOnlyVisiblePanes(t *testing.T) {
	marker := isoInner(t)
	l153Conf(t, "LAYOUT_MODE=left\n")
	_, win, leader, _, mates, teamPath, proj := l153Win(t, marker, 0, 6)
	// 4 teammates inactifs : cachés par la règle des panes cachés (TEAM_HIDE_IDLE)
	ms := []string{fmt.Sprintf(`{"name":"team-lead","agentType":"team-lead","cwd":%q}`, proj)}
	for i, p := range mates {
		ms = append(ms, fmt.Sprintf(`{"name":"dev-%02d","agentType":"dev-%02d","tmuxPaneId":%q,"isActive":%v}`, i, i, p, i < 2))
	}
	if err := os.WriteFile(teamPath, []byte(`{"members":[`+strings.Join(ms, ",")+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	pw := projectOf(win)
	applyVis(visCfg(), pw, leader, teamPath, now)
	if !applyVis(visCfg(), pw, leader, teamPath, now.Add(time.Minute)) {
		t.Fatal("aucun teammate caché")
	}
	RunRelayout(win)
	_, _, g := geometry(t, win)
	if len(g) != 3 {
		t.Fatalf("panes visibles : %d, attendu 3 (leader + 2 actifs)", len(g))
	}
	checkLeft(t, win, leader, nil, mates[:2], 46) // 2 teammates visibles : une colonne
}

// Ligne de titre par pane (#152 : pane-border-status top) : la disposition « left » en tient compte — rien ne
// déborde, aucun chevauchement, le leader reste en haut à gauche.
func TestRelayoutLeftWithPaneTitleLine(t *testing.T) {
	for _, n := range []int{2, 4, 5, 8} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			marker := isoInner(t)
			l153Conf(t, "LAYOUT_MODE=left\n")
			_, win, leader, cdps, mates, _, _ := l153Win(t, marker, 0, n)
			dt(t, "set-option", "-w", "-t", win, "pane-border-status", "top")
			RunRelayout(win)
			checkLeft(t, win, leader, cdps, mates, 46)
		})
	}
}

// Prise en compte sans relance : le surveillant de mise en page relit config.conf quand le fichier change et
// remet la fenêtre en page (top → left → top), sans Ctrl+B R.
func TestLayoutWatchFollowsConfigChange(t *testing.T) {
	marker := isoInner(t)
	l153Conf(t, "LAYOUT_MODE=top\n")
	_, win, leader, cdps, mates, _, proj := l153Win(t, marker, 0, 3)
	done := make(chan struct{})
	go func() { defer close(done); RunLayoutWatch(win, leader, proj) }()
	t.Cleanup(func() {
		_ = dtmux("kill-server").Run()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("le surveillant ne s'est pas arrêté")
		}
	})
	waitGeom := func(left bool, what string) {
		t.Helper()
		end := time.Now().Add(12 * time.Second)
		for time.Now().Before(end) {
			cols, _, g := geometry(t, win)
			if L := g[leader]; (L.W == cols) != left && L.W > 0 {
				if left && L.W < cols && L.X == 0 || !left {
					return
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s : mise en page non suivie en 12 s", what)
	}
	waitGeom(false, "départ top")
	l153WriteConf(t, "LAYOUT_MODE=left\n")
	waitGeom(true, "top → left")
	checkLeft(t, win, leader, cdps, mates, 46)
	l153WriteConf(t, "LAYOUT_MODE=left\ndeclare -A PROJECT_LAYOUTS=([\"proj\"]=\"top\")\n")
	waitGeom(false, "exception du projet : left → top")
}
