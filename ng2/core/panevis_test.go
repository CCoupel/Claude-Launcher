package core

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseFormatPaneVis(t *testing.T) {
	m := ParsePaneVis("%9:s %7:h bidon %3:x 5:h")
	if !reflect.DeepEqual(m, map[string]string{"%7": "h", "%9": "s"}) {
		t.Fatalf("ParsePaneVis = %v", m)
	}
	if s := FormatPaneVis(m); s != "%7:h %9:s" {
		t.Fatalf("FormatPaneVis = %q", s)
	}
}

func TestDecidePaneVis(t *testing.T) {
	const now = 1000
	tests := []struct {
		name     string
		p        VisPane
		choice   string
		hideIdle int
		hide     bool
		keep     string // choix conservé
		idleAt   int64  // changement attendu (-1 : aucun)
	}{
		{"leader jamais caché", VisPane{ID: "%1", Protected: true}, "h", 30, false, "", -1},
		{"actif visible, choix retirés", VisPane{ID: "%2", Active: true, IdleAt: 5}, "h", 30, false, "", 0},
		{"inactif récent visible", VisPane{ID: "%3", IdleAt: now - 10}, "", 30, false, "", -1},
		{"inactif au délai caché", VisPane{ID: "%3", IdleAt: now - 30}, "", 30, true, "", -1},
		{"inactif inconnu : début noté", VisPane{ID: "%3"}, "", 30, false, "", now},
		{"délai 0 : jamais", VisPane{ID: "%3", IdleAt: 1}, "", 0, false, "", -1},
		{"fixé visible", VisPane{ID: "%3", IdleAt: 1}, "s", 30, false, "s", -1},
		{"caché à la main même récent", VisPane{ID: "%3", IdleAt: now}, "h", 30, true, "h", -1},
		{"caché à la main, délai 0", VisPane{ID: "%3", IdleAt: now}, "h", 0, true, "h", -1},
		{"focus : pas caché par le délai", VisPane{ID: "%3", IdleAt: 1, Focus: true}, "", 30, false, "", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := map[string]string{}
			if tt.choice != "" {
				ch[tt.p.ID] = tt.choice
			}
			d := DecidePaneVis([]VisPane{tt.p}, ch, tt.hideIdle, now)
			if d.Hide[tt.p.ID] != tt.hide {
				t.Fatalf("caché = %v, attendu %v", d.Hide[tt.p.ID], tt.hide)
			}
			if d.Choices[tt.p.ID] != tt.keep {
				t.Fatalf("choix = %q, attendu %q", d.Choices[tt.p.ID], tt.keep)
			}
			at, changed := d.IdleAt[tt.p.ID]
			if tt.idleAt < 0 && changed || tt.idleAt >= 0 && (!changed || at != tt.idleAt) {
				t.Fatalf("idleAt = %v (%v), attendu %d", at, changed, tt.idleAt)
			}
		})
	}
	// choix d'un pane disparu : nettoyé
	d := DecidePaneVis(nil, map[string]string{"%8": "h"}, 30, now)
	if len(d.Choices) != 0 {
		t.Fatalf("choix périmé conservé : %v", d.Choices)
	}
}

func TestToggleRules(t *testing.T) {
	if ToggleChoice(true) != "s" || ToggleChoice(false) != "h" {
		t.Fatal("ToggleChoice")
	}
	for _, p := range []VisPane{{Protected: true}, {Active: true}} {
		if ToggleDenied(p, true) == "" {
			t.Fatalf("bascule permise sur %+v", p)
		}
	}
	if ToggleDenied(VisPane{}, false) == "" || ToggleDenied(VisPane{}, true) != "" {
		t.Fatal("pane inconnu / inactif")
	}
}

func TestParseLiveHidden(t *testing.T) {
	l, ok := ParseLive("|0|2\nP %1 claude\nP %2 claude\nH %5 claude\nX %9 bash")
	if !ok || l.Panes != 3 || l.Hidden != 1 || !l.Claude {
		t.Fatalf("ParseLive = %+v", l)
	}
	if l, _ := ParseLive("|0|2\n%1 claude\n%2 claude"); l.Panes != 2 || l.Hidden != 0 {
		t.Fatalf("sans marqueur : %+v", l)
	}
}

func TestTeamHideIdleConfig(t *testing.T) {
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	for _, tt := range []struct {
		conf string
		want int
	}{{"", 30}, {"TEAM_HIDE_IDLE=0\n", 0}, {"TEAM_HIDE_IDLE=45\n", 45}, {"TEAM_HIDE_IDLE=abc\n", 30}} {
		p := d + "/c.conf"
		_ = os.WriteFile(p, []byte(tt.conf), 0o644)
		if got := loadConfigAt(p).TeamHideIdle; got != tt.want {
			t.Errorf("%q → %d, attendu %d", tt.conf, got, tt.want)
		}
		var lite string
		for _, l := range strings.Split(confLite(tt.conf, "/h"), "\n") {
			if strings.HasPrefix(l, "hideidle\t") {
				lite = strings.TrimPrefix(l, "hideidle\t")
			}
		}
		if lite != fmt.Sprint(map[bool]int{true: tt.want, false: 30}[tt.conf != "TEAM_HIDE_IDLE=abc\n"]) && tt.conf != "TEAM_HIDE_IDLE=abc\n" {
			t.Errorf("confLite %q : %q", tt.conf, lite)
		}
	}
}

// ── tmux isolé ────────────────────────────────────────────────────────────

type isoTmux struct {
	sock, marker string
}

func (i isoTmux) run(args ...string) (string, error) {
	out, err := exec.Command("tmux", append([]string{"-L", i.sock, "-f", "/dev/null"}, args...)...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// newIsoTmux : serveur tmux isolé (socket dédiée, TMUX_TMPDIR court) ; l'arrêt
// (panes puis serveur) est déclaré en dernier, donc exécuté en premier, et le
// test échoue s'il reste un processus portant le marqueur des panes (#128).
func newIsoTmux(t *testing.T) isoTmux {
	t.Helper()
	unixOnly(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	d, err := os.MkdirTemp("", "cltmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", d)
	t.Setenv("TMUX", "")
	i := isoTmux{sock: fmt.Sprintf("cl-test-vis-%d", os.Getpid()), marker: fmt.Sprintf("6%08d", os.Getpid())}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	t.Cleanup(func() {
		if out, err := i.run("list-panes", "-a", "-F", "#{pane_pid}"); err == nil {
			for _, p := range strings.Fields(out) {
				_ = exec.Command("kill", "-TERM", "--", "-"+p).Run()
			}
		}
		_, _ = i.run("kill-server")
		deadline := time.Now().Add(5 * time.Second)
		for {
			out, _ := exec.Command("pgrep", "-f", "sleep "+i.marker).Output()
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
	return i
}

func (i isoTmux) must(t *testing.T, args ...string) string {
	t.Helper()
	out, err := i.run(args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func (i isoTmux) sleepCmd() string { return "sleep " + i.marker }

func TestApplyPaneVisReal(t *testing.T) {
	i := newIsoTmux(t)
	if _, err := i.run("new-session", "-d", "-s", "hub", "-x", "120", "-y", "40", "-n", "proj", i.sleepCmd()); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	win := "=hub:=proj"
	leader := i.must(t, "display-message", "-p", "-t", win, "#{pane_id}")
	mk := func() string {
		return i.must(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", leader, i.sleepCmd())
	}
	a, b, c := mk(), mk(), mk() // a actif, b inactif, c inactif
	pidOf := func(p string) string { return i.must(t, "display-message", "-p", "-t", p, "#{pane_pid}") }
	pids := map[string]string{a: pidOf(a), b: pidOf(b), c: pidOf(c)}
	tc := &TeamConfig{Members: []TeamMember{
		{Name: "team-lead", AgentType: "team-lead", PaneID: leader},
		{Name: "dev-a", AgentType: "dev-a", PaneID: a, IsActive: true},
		{Name: "dev-b", AgentType: "dev-b", PaneID: b},
		{Name: "dev-c", AgentType: "dev-c", PaneID: c},
	}}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	home := ProjectHome(i.run, win, false)
	if !strings.HasPrefix(home, "@") {
		t.Fatalf("home = %q", home)
	}
	apply := func(now int64) ApplyResult {
		t.Helper()
		r, err := ApplyPaneVis(i.run, win, home, tc, 30, now, false)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	inWin := func() string { return i.must(t, "list-panes", "-t", win, "-F", "#{pane_id}") }
	// 1er tour : début d'inactivité noté, rien de caché
	if r := apply(1000); r.Changed() {
		t.Fatalf("masquage immédiat : %+v", r)
	}
	if got := i.must(t, "show-options", "-pqv", "-t", b, optIdleAt); got != "1000" {
		t.Fatalf("@cl_idle_at = %q", got)
	}
	// délai écoulé : b et c cachés, a et leader visibles
	r := apply(1030)
	if len(r.Hid) != 2 || len(r.Shown) != 0 {
		t.Fatalf("masquage : %+v", r)
	}
	if w := inWin(); strings.Contains(w, b) || strings.Contains(w, c) || !strings.Contains(w, a) || !strings.Contains(w, leader) {
		t.Fatalf("fenêtre après masquage : %q", w)
	}
	// %id et PID conservés, rattachement par @cl_home
	panes, _, err := ProjectPanes(i.run, win, home, false)
	if err != nil || len(panes) != 4 {
		t.Fatalf("recensement : %v %v", panes, err)
	}
	for _, p := range panes {
		if want, ok := pids[p.ID]; ok && p.PID != want {
			t.Fatalf("PID de %s changé : %s → %s", p.ID, want, p.PID)
		}
		if p.Hidden != (p.ID == b || p.ID == c) {
			t.Fatalf("Hidden de %s = %v", p.ID, p.Hidden)
		}
	}
	// recensement d'un autre projet : rien de la réserve
	if other, _, _ := ProjectPanes(i.run, win, "@999", false); len(other) != 2 {
		t.Fatalf("panes d'un autre projet : %v", other)
	}
	// bascule manuelle : b caché → visible fixé ; refus sur a et le leader
	if res := TogglePane(i.run, win, home, tc, a, false); res.OK || res.Msg == "" {
		t.Fatalf("bascule d'un actif permise : %+v", res)
	}
	if res := TogglePane(i.run, win, home, tc, leader, false); res.OK {
		t.Fatal("bascule du leader permise")
	}
	if res := TogglePane(i.run, win, home, tc, b, false); !res.OK {
		t.Fatalf("bascule de b : %+v", res)
	}
	if r := apply(1031); len(r.Shown) != 1 || r.Shown[0] != b || len(r.Hid) != 0 {
		t.Fatalf("retour de b : %+v", r)
	}
	if !strings.Contains(inWin(), b) {
		t.Fatal("b absent de la fenêtre")
	}
	if got := i.must(t, "show-options", "-wqv", "-t", win, optVis); got != b+":s" {
		t.Fatalf("@cl_panevis = %q", got)
	}
	if got := i.must(t, "show-options", "-pqv", "-t", b, optHome); got != "" {
		t.Fatalf("@cl_home non retiré : %q", got)
	}
	// second double-clic : redevient caché à la main, même sans délai
	if res := TogglePane(i.run, win, home, tc, b, false); !res.OK {
		t.Fatal(res.Msg)
	}
	if r := apply(1032); len(r.Hid) != 1 || r.Hid[0] != b {
		t.Fatalf("recachage de b : %+v", r)
	}
	// c redevient actif : ramené, choix et idle_at retirés ; b actif aussi
	tc.Members[3].IsActive, tc.Members[2].IsActive = true, true
	r = apply(1033)
	if len(r.Shown) != 2 {
		t.Fatalf("retour des actifs : %+v", r)
	}
	if got := i.must(t, "show-options", "-wqv", "-t", win, optVis); got != "" {
		t.Fatalf("choix non nettoyés : %q", got)
	}
	// aucun masquage du pane qui a le focus
	tc.Members[3].IsActive, tc.Members[2].IsActive = false, false
	i.must(t, "select-pane", "-t", c)
	apply(2000)
	if r := apply(2100); len(r.Hid) != 1 || r.Hid[0] != b {
		t.Fatalf("masquage avec focus sur c : %+v", r)
	}
	// carte TEAM : instances, caché, version
	lines := TeamLines([]Teammate{{Name: "dev-a"}, {Name: "dev-b"}, {Name: "qa", Tag: "v1"}}, "/nulle/part", panes, map[string]string{}, 30, 2100)
	if len(lines) == 0 {
		t.Fatal("carte vide")
	}
}

// Fermeture d'un projet : les panes cachés sont tués, processus compris, et la
// réserve disparaît (critique : aucun processus restant).
func TestCloseKillsHiddenPanes(t *testing.T) {
	i := newIsoTmux(t)
	if _, err := i.run("new-session", "-d", "-s", "hub", "-x", "120", "-y", "40", "-n", "proj", i.sleepCmd()); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	win := "=hub:=proj"
	leader := i.must(t, "display-message", "-p", "-t", win, "#{pane_id}")
	b := i.must(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", leader, i.sleepCmd())
	tc := &TeamConfig{Members: []TeamMember{{Name: "team-lead", PaneID: leader}, {Name: "dev-b", AgentType: "dev-b", PaneID: b}}}
	home := ProjectHome(i.run, win, false)
	if _, err := ApplyPaneVis(i.run, win, home, tc, 1, time.Now().Unix()+10, false); err != nil {
		t.Fatal(err)
	}
	// 2e tour : idle_at posé au 1er, le délai de 1 s est atteint
	if r, err := ApplyPaneVis(i.run, win, home, tc, 1, time.Now().Unix()+20, false); err != nil || len(r.Hid) != 1 {
		t.Fatalf("masquage : %+v %v", r, err)
	}
	if out, _ := exec.Command("pgrep", "-fc", "^sleep "+i.marker+"$").Output(); strings.TrimSpace(string(out)) != "2" {
		t.Fatalf("processus avant fermeture : %s", out)
	}
	if n := KillHiddenPanes(i.run, home); n != 1 {
		t.Fatalf("panes cachés tués = %d", n)
	}
	if _, err := i.run("has-session", "-t", "="+HiddenSession); err == nil {
		t.Fatal("réserve conservée sans pane caché")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command("pgrep", "-fc", "^sleep "+i.marker+"$").Output()
		if strings.TrimSpace(string(out)) == "1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("le processus du pane caché survit : %s", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Panes orphelins : la fenêtre d'origine n'existe plus.
func TestKillOrphanHidden(t *testing.T) {
	i := newIsoTmux(t)
	if _, err := i.run("new-session", "-d", "-s", "hub", "-x", "120", "-y", "40", "-n", "proj", i.sleepCmd()); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	i.must(t, "new-window", "-d", "-t", "=hub:", "-n", "autre", i.sleepCmd())
	win := "=hub:=autre"
	leader := i.must(t, "display-message", "-p", "-t", win, "#{pane_id}")
	b := i.must(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", leader, i.sleepCmd())
	home := ProjectHome(i.run, win, false)
	if err := hidePane(i.run, win, b, home); err != nil {
		t.Fatal(err)
	}
	if n := KillOrphanHidden(i.run); n != 0 {
		t.Fatalf("pane non orphelin tué : %d", n)
	}
	i.must(t, "kill-window", "-t", win)
	if n := KillOrphanHidden(i.run); n != 1 {
		t.Fatalf("orphelin non tué : %d", n)
	}
}

func TestTeamLines(t *testing.T) {
	unixOnly(t)
	root, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	_ = os.MkdirAll(root+"/teams/s1", 0o755)
	_ = os.WriteFile(root+"/teams/s1/config.json", []byte(`{"members":[
		{"name":"team-lead","agentType":"team-lead","tmuxPaneId":"%1","cwd":"`+proj+`"},
		{"name":"dev-a","agentType":"dev-a","tmuxPaneId":"%2","isActive":true},
		{"name":"dev-b","agentType":"dev-b","tmuxPaneId":"%3"},
		{"name":"dev-c","agentType":"dev-c","tmuxPaneId":"%4"},
		{"name":"dev-d","agentType":"dev-d","tmuxPaneId":"%5"}]}`), 0o644)
	pane := func(id string, hidden bool, idleAt int64) ProjPane {
		return ProjPane{PaneInfo: PaneInfo{ID: id, Cmd: "2.1.87"}, Hidden: hidden, IdleAt: idleAt}
	}
	panes := []ProjPane{pane("%5", true, 10), pane("%1", false, 0), pane("%3", false, 90), pane("%2", false, 0),
		pane("%4", false, 0), {PaneInfo: PaneInfo{ID: "%9", Cmd: "bash"}}}
	reg := []Teammate{{Name: "dev-a", Tag: "v1"}, {Name: "dev-b"}, {Name: "qa", Tag: "v2", Model: "sonnet"}}
	got := TeamLines(reg, proj, panes, map[string]string{"%4": "s"}, 30, 100)
	byName := map[string]TeamLine{}
	var order []string
	for _, l := range got {
		byName[l.Name] = l
		order = append(order, l.Name)
	}
	if !reflect.DeepEqual(order, []string{"leader", "dev-a", "dev-b", "dev-c", "dev-d", "qa"}) {
		t.Fatalf("ordre : %v", order)
	}
	if l := byName["leader"]; l.Pane != "%1" || !l.Started || l.Hidden {
		t.Fatalf("leader : %+v", l)
	}
	if l := byName["dev-a"]; !l.Active || l.Tag != "v1" || l.IdleLeft != 0 {
		t.Fatalf("dev-a : %+v", l)
	}
	if l := byName["dev-b"]; l.Active || l.IdleLeft != 20 {
		t.Fatalf("dev-b : %+v", l)
	}
	if l := byName["dev-c"]; !l.Pinned || l.IdleLeft != 0 {
		t.Fatalf("dev-c : %+v", l)
	}
	if l := byName["dev-d"]; !l.Hidden || l.IdleLeft != 0 {
		t.Fatalf("dev-d : %+v", l)
	}
	if l := byName["qa"]; l.Started || l.Pane != "" || l.Tag != "v2" {
		t.Fatalf("qa : %+v", l)
	}
}

// Fenêtre compacte (120x40, leader et 4 panes coupés depuis le leader, qui n'a
// plus que quelques lignes) : le retour d'un teammate redevenu actif réussit
// toujours (« create pane failed: pane too small » en coupant le leader).
func TestApplyPaneVisReturnInCompactWindow(t *testing.T) {
	i := newIsoTmux(t)
	if _, err := i.run("new-session", "-d", "-s", "hub", "-x", "120", "-y", "40", "-n", "proj", i.sleepCmd()); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	win := "=hub:=proj"
	leader := i.must(t, "display-message", "-p", "-t", win, "#{pane_id}")
	var mates []string
	for n := 0; n < 4; n++ {
		mates = append(mates, i.must(t, "split-window", "-d", "-v", "-P", "-F", "#{pane_id}", "-t", leader, i.sleepCmd()))
	}
	members := []TeamMember{{Name: "team-lead", AgentType: "team-lead", PaneID: leader}}
	for k, m := range mates {
		members = append(members, TeamMember{Name: fmt.Sprintf("dev-%d", k), AgentType: fmt.Sprintf("dev-%d", k), PaneID: m})
	}
	tc := &TeamConfig{Members: members}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	home := ProjectHome(i.run, win, false)
	hidden := mates[0]
	i.must(t, "set-option", "-w", "-t", win, optVis, hidden+":h")
	if r, err := ApplyPaneVis(i.run, win, home, tc, 0, 1000, false); err != nil || len(r.Hid) != 1 {
		t.Fatalf("masquage : %+v %v", r, err)
	}
	h := i.must(t, "display-message", "-p", "-t", leader, "#{pane_height}")
	t.Logf("hauteur du leader : %s", h)
	tc.Members[1].IsActive = true // redevenu actif : retour obligatoire
	r, err := ApplyPaneVis(i.run, win, home, tc, 0, 1001, false)
	if err != nil || len(r.Shown) != 1 || r.Shown[0] != hidden {
		t.Fatalf("retour impossible : %+v %v", r, err)
	}
	if !strings.Contains(i.must(t, "list-panes", "-t", win, "-F", "#{pane_id}"), hidden) {
		t.Fatal("pane non revenu dans la fenêtre")
	}
}
