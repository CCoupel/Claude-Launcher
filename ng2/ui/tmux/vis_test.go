package tmux

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #136 — application de la règle des panes cachés par la coque (watcher, bascule
// de la carte Team, fermeture) sur un serveur intérieur isolé (CL_INNER_SOCKET),
// faux claude = sleep, jamais le hub réel. Nommés *Vis* pour ne pas se mêler aux
// tests boîte noire de panehide_tmux_test.go.

func isoInner(t *testing.T) (marker string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	sock := fmt.Sprintf("cl-test-vis-%d", os.Getpid())
	marker = fmt.Sprintf("7%08d", os.Getpid())
	t.Setenv("CL_INNER_SOCKET", sock)
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	d, err := os.MkdirTemp("", "cltmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", d)
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	// dernier enregistré, premier exécuté : avant la suppression de TMUX_TMPDIR
	t.Cleanup(func() {
		if out, err := dtmux("list-panes", "-a", "-F", "#{pane_pid}").Output(); err == nil {
			for _, p := range strings.Fields(string(out)) {
				_ = exec.Command("kill", "-TERM", "--", "-"+p).Run()
			}
		}
		_ = dtmux("kill-server").Run()
		deadline := time.Now().Add(5 * time.Second)
		for {
			out, _ := exec.Command("pgrep", "-f", "sleep "+marker).Output()
			left := strings.TrimSpace(string(out))
			if left == "" {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("processus restants après le test : %s", left)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return marker
}

func dt(t *testing.T, args ...string) string {
	t.Helper()
	out, err := dtmux(args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %v : %v (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// visProject : fenêtre « key » du hub avec un leader et 3 teammates inactifs
// (a, b, c) ; fichier de team où a est actif. Rend la fenêtre et les panes.
func visProject(t *testing.T, marker string) (key, win, teamPath, proj, leader string, mates []string) {
	t.Helper()
	key = "proj"
	if out, err := dtmux("-f", "/dev/null", "new-session", "-d", "-s", hubSession, "-n", key, "-x", "200", "-y", "50", "sleep "+marker).CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	win = dt(t, "display-message", "-p", "-t", exactW(hubSession, key), "#{window_id}")
	leader = dt(t, "display-message", "-p", "-t", win, "#{pane_id}")
	for i := 0; i < 3; i++ {
		mates = append(mates, dt(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sleep "+marker))
	}
	proj = t.TempDir()
	teamPath = core.ClaudeDir() + "/teams/s1/config.json"
	_ = os.MkdirAll(core.ClaudeDir()+"/teams/s1", 0o755)
	cfg := fmt.Sprintf(`{"members":[
	 {"name":"team-lead","agentType":"team-lead","cwd":%q},
	 {"name":"dev-a","agentType":"dev-a","tmuxPaneId":%q,"isActive":true},
	 {"name":"dev-b","agentType":"dev-b","tmuxPaneId":%q},
	 {"name":"dev-c","agentType":"dev-c","tmuxPaneId":%q}]}`, proj, mates[0], mates[1], mates[2])
	if err := os.WriteFile(teamPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func visible(t *testing.T, win string) string {
	return dt(t, "list-panes", "-t", win, "-F", "#{pane_id}")
}

func TestApplyVisHidesAndRelays(t *testing.T) {
	marker := isoInner(t)
	key, win, teamPath, proj, leader, mates := visProject(t, marker)
	cfg := visCfg()
	pw := projectOf(win)
	_, _ = key, proj
	now := time.Now()
	if applyVis(cfg, pw, leader, teamPath, now) {
		t.Fatal("masquage au premier tour")
	}
	if !applyVis(cfg, pw, leader, teamPath, now.Add(31*time.Second)) {
		t.Fatal("rien de caché après le délai")
	}
	vis := visible(t, win)
	if !strings.Contains(vis, leader) || !strings.Contains(vis, mates[0]) || strings.Contains(vis, mates[1]) || strings.Contains(vis, mates[2]) {
		t.Fatalf("visibles : %q", vis)
	}
	// remise en page : le teammate visible prend toute la largeur de la fenêtre
	if w, pw2 := dt(t, "display-message", "-p", "-t", win, "#{window_width}"),
		dt(t, "display-message", "-p", "-t", mates[0], "#{pane_width}"); w != pw2 {
		t.Fatalf("teammate visible sur %s colonnes, fenêtre de %s", pw2, w)
	}
	// compté avec les cachés (carte, fermeture)
	panes, _, err := core.ProjectPanes(tmuxFn(hub), win, core.ProjectHome(tmuxFn(hub), win, false), false)
	if err != nil || len(panes) != 4 {
		t.Fatalf("recensement : %v %v", panes, err)
	}
}

func TestToggleCmd(t *testing.T) {
	marker := isoInner(t)
	key, win, teamPath, proj, leader, mates := visProject(t, marker)
	cfg := visCfg()
	sel := Selection{Name: key, Key: key, Path: proj}
	run := func(pane string) toggleMsg {
		t.Helper()
		return toggleCmd(sel, cfg, pane)().(toggleMsg)
	}
	// refus : actif, leader, pane inconnu
	for _, p := range []string{mates[0], leader, "%999"} {
		if m := run(p); m.msg == "" {
			t.Fatalf("bascule permise sur %s", p)
		}
	}
	// inactif visible → caché à la main (sans délai), effet immédiat : le pane a
	// quitté la fenêtre et la carte relue au retour de l'appel le dit caché
	t0 := time.Now()
	if m := run(mates[1]); m.msg != "" {
		t.Fatalf("bascule refusée : %q", m.msg)
	}
	if d := time.Since(t0); d > 300*time.Millisecond {
		t.Errorf("bascule en %v, attendu < 300 ms", d)
	}
	if strings.Contains(visible(t, win), mates[1]) {
		t.Fatal("pane non caché")
	}
	pd := loadPanesCmd(sel, visCfg())().(paneData)
	hiddenRow := false
	for _, l := range pd.lines {
		if l.Pane == mates[1] && l.Hidden {
			hiddenRow = true
		}
	}
	if !hiddenRow {
		t.Fatalf("carte : %+v", pd.lines)
	}
	// second double-clic : visible fixé
	if m := run(mates[1]); m.msg != "" {
		t.Fatal(m.msg)
	}
	if !strings.Contains(visible(t, win), mates[1]) {
		t.Fatal("pane non ramené")
	}
	// lecture seule : refus et rien ne bouge
	dt(t, "set-window-option", "-t", win, "@cl_readonly", "1")
	if m := run(mates[2]); !strings.Contains(m.msg, "lecture seule") {
		t.Fatalf("lecture seule : %q", m.msg)
	}
	if !strings.Contains(visible(t, win), mates[2]) {
		t.Fatal("pane caché malgré la lecture seule")
	}
	_ = teamPath
}

func visCfg() core.Config {
	return core.Config{TeamHideIdle: 30, Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2, Layout3RowDen: 7,
		BrightenStep: 1, DarkenStep: 1, Palette: []string{"17", "52"}, ProjectColors: map[string]string{}}
}

// Carte Team : une ligne d'écran par instance, double-clic (450 ms) sur un
// teammate lancé = bascule ; un seul clic, une ligne non lancée ou un autre
// pane ne basculent pas.
func TestTeamCardDoubleClick(t *testing.T) {
	for _, w := range []int{100, 40} { // carte encadrée, puis pane étroit
		m := stateModel{cfg: visCfg(), w: w, h: 60, loaded: true, hasSel: true,
			sel: Selection{Name: "p", Key: "p", Path: t.TempDir()}}
		m.panes = paneData{path: m.sel.Path, open: true, lines: []core.TeamLine{
			{Name: "leader", Pane: "%1", Started: true},
			{Name: "dev-a", Pane: "%2", Started: true, Active: true},
			{Name: "dev-b", Pane: "%3", Started: true, Hidden: true},
			{Name: "qa"},
		}}
		top := m.teamTop()
		if top <= 0 {
			t.Fatalf("w=%d : début de la liste = %d", w, top)
		}
		rows := strings.Split(m.teamBody(min(w-2, 110)), "\n")
		if len(rows) != 4 || !strings.Contains(rows[2], "caché") || !strings.Contains(rows[1], "actif") {
			t.Fatalf("w=%d : lignes %q", w, rows)
		}
		view := strings.Split(m.View(), "\n")
		if !strings.Contains(view[top+2], "dev-b") {
			t.Fatalf("w=%d : ligne %d de l'écran = %q, attendu dev-b", w, top+2, view[top+2])
		}
		click := func(row int) tea.Cmd {
			mm, cmd := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: top + row})
			m = mm.(stateModel)
			return cmd
		}
		if click(2) != nil {
			t.Fatal("un seul clic bascule")
		}
		if click(2) == nil {
			t.Fatal("double-clic sans bascule")
		}
		if click(2) != nil { // le compteur est remis à zéro après la bascule
			t.Fatal("triple clic : seconde bascule")
		}
		click(1)
		if click(0) != nil {
			t.Fatal("clics sur deux panes différents pris pour un double-clic")
		}
		click(3)
		if click(3) != nil {
			t.Fatal("double-clic sur un teammate non lancé")
		}
	}
}

// critical : fermer un projet ferme aussi ses teammates cachés — plus aucun
// processus, et la réserve est supprimée ; le nettoyage du test le contrôle.
func TestCloseProjectKillsHiddenTeammates(t *testing.T) {
	marker := isoInner(t)
	key, win, teamPath, _, leader, mates := visProject(t, marker)
	old := closeWait
	closeWait = time.Second
	t.Cleanup(func() { closeWait = old })
	now := time.Now()
	pw := projectOf(win)
	applyVis(visCfg(), pw, leader, teamPath, now)
	if !applyVis(visCfg(), pw, leader, teamPath, now.Add(time.Minute)) {
		t.Fatal("rien de caché")
	}
	var steps []string
	if err := stopSession(tmuxOut, exactW(hubSession, key), false, func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(steps, "|"), "2 pane(s) caché(s) fermé(s)") {
		t.Fatalf("étapes : %v", steps)
	}
	if dtmux("has-session", "-t", "="+core.HiddenSession).Run() == nil {
		t.Fatal("réserve conservée")
	}
	for _, p := range mates[1:] {
		if out, _ := dtmux("list-panes", "-a", "-F", "#{pane_id}").Output(); strings.Contains(string(out), p) {
			t.Fatalf("pane caché %s encore vivant", p)
		}
	}
}
