package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// LOT-3 v6.2.0 (#151, #152) — tests unitaires de core, écrits depuis les contrats
// (contracts/desktop-dispatch.md : teamHead, odd, panes[].state ; contracts/tmux-options.md :
// @cl_state, @cl_name, pane-border-*). Complètent paneheader_test.go.

// ── état d'un pane ────────────────────────────────────────────────────────

func TestPaneStateRules(t *testing.T) {
	cases := []struct {
		name   string
		m      *TeamMember
		leader bool
		want   string
	}{
		{"le leader prime sur son membre inactif", &TeamMember{Name: "x"}, true, PaneLead},
		{"membre team-lead hors pane leader", &TeamMember{Name: "team-lead"}, false, PaneLead},
		{"cdp inactif", &TeamMember{Name: "cdp", AgentType: "cdp"}, false, PaneLead},
		{"cdp actif", &TeamMember{Name: "cdp", AgentType: "cdp", IsActive: true}, false, PaneLead},
		{"teammate actif", &TeamMember{Name: "dev-a", IsActive: true}, false, PaneActive},
		{"teammate inactif", &TeamMember{Name: "dev-b"}, false, PaneIdle},
		{"pane sans agent", nil, false, PaneNone},
	}
	for _, c := range cases {
		if got := PaneState(c.m, c.leader); got != c.want {
			t.Errorf("%s : %q, attendu %q", c.name, got, c.want)
		}
	}
	// valeurs du contrat
	if PaneLead != "lead" || PaneActive != "active" || PaneIdle != "idle" || PaneNone != "" {
		t.Errorf("valeurs de @cl_state : %q %q %q %q", PaneLead, PaneActive, PaneIdle, PaneNone)
	}
}

func TestPaneHeadersNamesAndOrder(t *testing.T) {
	tc := &TeamConfig{Members: []TeamMember{
		{Name: "team-lead"}, // le leader n'a pas de PaneID
		{Name: "cdp", AgentType: "cdp", PaneID: "%2"},
		{Name: "dev-a", PaneID: "%3", IsActive: true},
		{Name: "dev-b", PaneID: "%4"},
	}}
	hs := PaneHeaders([]string{"%1", "%2", "%3", "%4", "%9"}, "%1", tc)
	want := []PaneHeader{
		{"%1", PaneLead, "team-lead"}, {"%2", PaneLead, "cdp"}, {"%3", PaneActive, "dev-a"},
		{"%4", PaneIdle, "dev-b"}, {"%9", PaneNone, ""},
	}
	if fmt.Sprint(hs) != fmt.Sprint(want) {
		t.Errorf("en-têtes %v, attendu %v (ordre des panes conservé, pane sans agent sans nom)", hs, want)
	}
	// aucune team connue : le leader seul porte un état
	hs = PaneHeaders([]string{"%1", "%2"}, "%1", nil)
	if fmt.Sprint(hs) != fmt.Sprint([]PaneHeader{{"%1", PaneLead, "team-lead"}, {"%2", PaneNone, ""}}) {
		t.Errorf("sans team : %v", hs)
	}
	if len(PaneHeaders(nil, "", nil)) != 0 {
		t.Error("aucun pane : liste vide attendue")
	}
}

func TestHeaderSignatureChangesWithState(t *testing.T) {
	tc := &TeamConfig{Members: []TeamMember{{Name: "dev-a", PaneID: "%2", IsActive: true}}}
	a := HeaderSignature(PaneHeaders([]string{"%1", "%2"}, "%1", tc))
	tc.Members[0].IsActive = false
	b := HeaderSignature(PaneHeaders([]string{"%1", "%2"}, "%1", tc))
	c := HeaderSignature(PaneHeaders([]string{"%1", "%2", "%3"}, "%1", tc))
	tc.Members[0].Name = "dev-z"
	d := HeaderSignature(PaneHeaders([]string{"%1", "%2"}, "%1", tc))
	if a == b || b == c || b == d {
		t.Errorf("signature insensible à un changement d'état (%q / %q), de panes (%q) ou de nom (%q)", a, b, c, d)
	}
	if b != HeaderSignature(PaneHeaders([]string{"%1", "%2"}, "%1", &TeamConfig{Members: []TeamMember{{Name: "dev-a", PaneID: "%2"}}})) {
		t.Error("signature instable pour un même état")
	}
}

// ── en-tête de la carte TEAM et écarts par ligne ──────────────────────────

func TestApplyTeamHeadRules(t *testing.T) {
	t.Run("la cible passée prime sur celle des lignes", func(t *testing.T) {
		out, h := ApplyTeamHead([]TeamLine{{Name: "dev", Tag: "v5.9.0", TplTarget: "v5.8.0"}}, "v5.9.4")
		if h.Tpl != "v5.9.4" || len(out[0].Odd) != 1 || out[0].Odd[0] != OddTpl {
			t.Errorf("en-tête %+v, odd %v", h, out[0].Odd)
		}
	})
	t.Run("cible inconnue : aucun écart de template", func(t *testing.T) {
		out, h := ApplyTeamHead([]TeamLine{{Name: "dev", Tag: "v5.9.0"}, {Name: "qa", Tag: "v5.1.0"}}, "")
		if h.Tpl != "" || h.Odd != 0 || len(out[0].Odd)+len(out[1].Odd) != 0 {
			t.Errorf("en-tête %+v, odd %v %v", h, out[0].Odd, out[1].Odd)
		}
	})
	t.Run("tag vide : pas d'écart", func(t *testing.T) {
		out, h := ApplyTeamHead([]TeamLine{{Name: "dev"}}, "v5.9.4")
		if h.Odd != 0 || len(out[0].Odd) != 0 {
			t.Errorf("%+v %v", h, out[0].Odd)
		}
	})
	t.Run("claude : seulement un teammate démarré dont la version diffère du leader", func(t *testing.T) {
		lines := []TeamLine{
			{Name: "leader", Started: true, Claude: "2.1.87"},
			{Name: "a", Started: true, Claude: "2.1.87"},
			{Name: "b", Started: true, Claude: "2.1.80"},
			{Name: "c", Started: false, Claude: "2.1.50"}, // non démarré : ignoré
			{Name: "d", Started: true},                    // version inconnue : ignoré
		}
		out, h := ApplyTeamHead(lines, "")
		if h.Claude != "2.1.87" || h.Odd != 1 {
			t.Errorf("en-tête %+v, attendu claude 2.1.87 et 1 écart", h)
		}
		for i, want := range []string{"", "", OddClaude, "", ""} {
			if got := strings.Join(out[i].Odd, ","); got != want {
				t.Errorf("ligne %s : odd %q, attendu %q", out[i].Name, got, want)
			}
		}
	})
	t.Run("leader non démarré (projet fermé) : claude vide, aucun écart claude", func(t *testing.T) {
		_, h := ApplyTeamHead([]TeamLine{{Name: "leader"}, {Name: "a", Started: true, Claude: "2.1.80"}}, "")
		if h.Claude != "" || h.Odd != 0 {
			t.Errorf("%+v", h)
		}
	})
	t.Run("deux écarts sur une ligne : compté une seule fois", func(t *testing.T) {
		out, h := ApplyTeamHead([]TeamLine{
			{Name: "leader", Started: true, Claude: "2.1.87"},
			{Name: "dev", Started: true, Claude: "2.1.80", Tag: "v5.8.0"},
		}, "v5.9.4")
		if h.Odd != 1 || strings.Join(out[1].Odd, ",") != OddTpl+","+OddClaude {
			t.Errorf("en-tête %+v, odd %v", h, out[1].Odd)
		}
	})
	t.Run("rejouable : les écarts sont recalculés, pas cumulés", func(t *testing.T) {
		lines := []TeamLine{{Name: "dev", Tag: "v5.8.0"}}
		ApplyTeamHead(lines, "v5.9.4")
		out, h := ApplyTeamHead(lines, "v5.8.0") // la cible change : plus d'écart
		if h.Odd != 0 || len(out[0].Odd) != 0 {
			t.Errorf("écarts périmés conservés : %+v %v", h, out[0].Odd)
		}
	})
	if OddTpl != "tpl" || OddClaude != "claude" {
		t.Errorf("valeurs de team[].odd : %q %q", OddTpl, OddClaude)
	}
}

// ── format pane-border et pose des options (tmux isolé) ───────────────────

// tmuxFor : tmux isolé (serveur et dossier de sockets dédiés), jamais celui du hub.
type tmuxFor struct {
	sock    string
	sets    atomic.Int32 // set-option exécutés
	execErr error
}

func (x *tmuxFor) cmd(args ...string) *exec.Cmd {
	c := exec.Command("tmux", append([]string{"-L", x.sock}, args...)...)
	c.Env = EnvWithout("TMUX", "TMUX_PANE")
	return c
}
func (x *tmuxFor) Run(args ...string) error {
	if len(args) > 0 && args[0] == "set-option" {
		x.sets.Add(1)
	}
	return x.cmd(args...).Run()
}
func (x *tmuxFor) Output(args ...string) ([]byte, error) { return x.cmd(args...).Output() }

func isolatedTmux(t *testing.T) *tmuxFor {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	d, err := os.MkdirTemp("", "cltmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", d)
	x := &tmuxFor{sock: fmt.Sprintf("cl-core-l3-%d", os.Getpid())}
	t.Cleanup(func() { _ = x.cmd("kill-server").Run(); _ = os.RemoveAll(d) })
	if out, err := x.cmd("-f", "/dev/null", "new-session", "-d", "-s", "p", "-x", "120", "-y", "40", "sleep 120").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return x
}

func (x *tmuxFor) split(t *testing.T) string {
	t.Helper()
	out, err := x.Output("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "p:", "sleep 120")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (x *tmuxFor) paneOpt(pane, opt string) string {
	b, _ := x.Output("show-options", "-pqv", "-t", pane, opt)
	return strings.TrimSpace(string(b))
}

// Le format réel (évalué par tmux) rend « nom · actif », « nom · inactif », « nom · leader », et la
// commande du pane seule sans agent ; trois styles distincts par état.
func TestPaneBorderFormatRendering(t *testing.T) {
	x := isolatedTmux(t)
	panes := map[string]string{"active": x.split(t), "idle": x.split(t), "lead": x.split(t), "": x.split(t)}
	for st, p := range panes {
		if st != "" {
			_ = x.Run("set-option", "-p", "-t", p, "@cl_state", st)
			_ = x.Run("set-option", "-p", "-t", p, "@cl_name", "nom-"+st)
		}
	}
	render := func(p string) string {
		out, err := x.Output("display-message", "-p", "-t", p, "-F", PaneBorderFormat())
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	for st, want := range map[string]string{"active": "nom-active · actif", "idle": "nom-idle · inactif", "lead": "nom-lead · leader"} {
		if got := render(panes[st]); !strings.Contains(got, want) {
			t.Errorf("état %s : %q, attendu %q", st, got, want)
		}
	}
	if got := render(panes[""]); !strings.Contains(got, "sleep") || strings.Contains(got, "·") {
		t.Errorf("pane sans agent : %q, attendu la commande seule (sleep)", got)
	}
	styles := map[string]string{}
	for _, st := range []string{"active", "idle", "lead"} {
		m := regexpStyle(render(panes[st]))
		if m == "" {
			t.Errorf("état %s : aucun style #[…] dans %q", st, render(panes[st]))
		}
		styles[st] = m
	}
	if styles["active"] == styles["idle"] || styles["idle"] == styles["lead"] || styles["active"] == styles["lead"] {
		t.Errorf("styles non distincts : %v", styles)
	}
}

func regexpStyle(s string) string {
	i := strings.Index(s, "#[")
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "]")
	if j < 0 {
		return ""
	}
	return s[i : i+j+1]
}

// SetWindowHeaders : une ligne de titre par pane (top) et le format ; rejouée, elle n'écrit plus rien.
func TestSetWindowHeadersIdempotent(t *testing.T) {
	x := isolatedTmux(t)
	SetWindowHeaders(x, "p:")
	if v := func() string {
		b, _ := x.Output("show-options", "-wqv", "-t", "p:", "pane-border-status")
		return strings.TrimSpace(string(b))
	}(); v != PaneBorderStatus {
		t.Errorf("pane-border-status = %q, attendu %q", v, PaneBorderStatus)
	}
	if b, _ := x.Output("show-options", "-wqv", "-t", "p:", "pane-border-format"); strings.TrimSpace(string(b)) != PaneBorderFormat() {
		t.Errorf("pane-border-format = %q", b)
	}
	before := x.sets.Load()
	SetWindowHeaders(x, "p:")
	if n := x.sets.Load() - before; n != 0 {
		t.Errorf("%d écriture(s) à la seconde application : seules les différences sont écrites", n)
	}
	_ = x.Run("set-option", "-w", "-t", "p:", "pane-border-status", "off") // dérive : reposée
	SetWindowHeaders(x, "p:")
	if b, _ := x.Output("show-options", "-wqv", "-t", "p:", "pane-border-status"); strings.TrimSpace(string(b)) != PaneBorderStatus {
		t.Error("pane-border-status non reposé après dérive")
	}
}

// SyncPaneHeaders : n'écrit que les écarts, rend le nombre de panes modifiés, retire les deux options d'un
// pane sans agent, suit un changement d'état d'un seul pane.
func TestSyncPaneHeaders(t *testing.T) {
	x := isolatedTmux(t)
	a, b, c := x.split(t), x.split(t), x.split(t)
	lead := strings.TrimSpace(string(func() []byte { o, _ := x.Output("display-message", "-p", "-t", "p:.0", "#{pane_id}"); return o }()))
	want := []PaneHeader{{lead, PaneLead, "team-lead"}, {a, PaneActive, "dev-a"}, {b, PaneIdle, "dev-b"}, {c, PaneNone, ""}}
	if n := SyncPaneHeaders(x, "p:", want); n != 3 { // le pane sans agent n'a rien à retirer : pas modifié
		t.Errorf("premier passage : %d pane(s) modifié(s), attendu 3", n)
	}
	for _, h := range want[:3] {
		if x.paneOpt(h.Pane, "@cl_state") != h.State || x.paneOpt(h.Pane, "@cl_name") != h.Name {
			t.Errorf("pane %s : %q/%q, attendu %q/%q", h.Pane, x.paneOpt(h.Pane, "@cl_state"), x.paneOpt(h.Pane, "@cl_name"), h.State, h.Name)
		}
	}
	if x.paneOpt(c, "@cl_state") != "" || x.paneOpt(c, "@cl_name") != "" {
		t.Error("pane sans agent : options posées")
	}
	before := x.sets.Load()
	if n := SyncPaneHeaders(x, "p:", want); n != 0 || x.sets.Load() != before {
		t.Errorf("second passage : %d modifié(s), %d écriture(s) — état déjà conforme", n, x.sets.Load()-before)
	}
	// un seul pane change d'état
	want[1].State, want[2].State = PaneIdle, PaneActive
	if n := SyncPaneHeaders(x, "p:", want); n != 2 {
		t.Errorf("changement d'état : %d modifié(s), attendu 2", n)
	}
	if x.paneOpt(a, "@cl_state") != PaneIdle || x.paneOpt(b, "@cl_state") != PaneActive {
		t.Errorf("états après bascule : %q %q", x.paneOpt(a, "@cl_state"), x.paneOpt(b, "@cl_state"))
	}
	// un pane perd son agent : les deux options retirées
	want[2] = PaneHeader{b, PaneNone, ""}
	if n := SyncPaneHeaders(x, "p:", want); n != 1 || x.paneOpt(b, "@cl_state") != "" || x.paneOpt(b, "@cl_name") != "" {
		t.Errorf("pane sans agent : %d modifié(s), options %q/%q", n, x.paneOpt(b, "@cl_state"), x.paneOpt(b, "@cl_name"))
	}
	// pane inconnu de la fenêtre : ignoré
	if n := SyncPaneHeaders(x, "p:", []PaneHeader{{"%999", PaneActive, "x"}}); n != 0 {
		t.Errorf("pane absent de la fenêtre : %d modifié(s)", n)
	}
}
