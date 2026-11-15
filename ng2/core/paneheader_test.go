package core

import (
	"strings"
	"testing"
)

func TestPaneState(t *testing.T) {
	act, idle := &TeamMember{Name: "dev", IsActive: true}, &TeamMember{Name: "qa"}
	cases := []struct {
		name   string
		m      *TeamMember
		leader bool
		want   string
	}{
		{"leader", nil, true, PaneLead},
		{"cdp", &TeamMember{Name: "x", AgentType: "cdp", IsActive: true}, false, PaneLead},
		{"actif", act, false, PaneActive},
		{"inactif", idle, false, PaneIdle},
		{"sans agent", nil, false, PaneNone},
	}
	for _, c := range cases {
		if got := PaneState(c.m, c.leader); got != c.want {
			t.Errorf("%s : %q, attendu %q", c.name, got, c.want)
		}
	}
}

func TestPaneHeadersAndSignature(t *testing.T) {
	tc := &TeamConfig{Members: []TeamMember{{Name: "dev", PaneID: "%2", IsActive: true}, {Name: "qa", PaneID: "%3"}}}
	hs := PaneHeaders([]string{"%1", "%2", "%3", "%9"}, "%1", tc)
	want := "%1=lead:team-lead;%2=active:dev;%3=idle:qa;%9=:;"
	if got := HeaderSignature(hs); got != want {
		t.Errorf("signature %q, attendu %q", got, want)
	}
}

func TestPaneBorderFormatShape(t *testing.T) {
	f := PaneBorderFormat()
	for _, w := range []string{"#{@cl_name} · actif", "#{@cl_name} · inactif", "#{@cl_name} · leader", "#{pane_current_command}"} {
		if !strings.Contains(f, w) {
			t.Errorf("format sans %q : %s", w, f)
		}
	}
}

func TestApplyTeamHead(t *testing.T) {
	lines := []TeamLine{
		{Name: "leader", Started: true, Claude: "2.1.87", Tag: "v5.9.4", TplTarget: "v5.9.4"},
		{Name: "dev", Started: true, Claude: "2.1.80", Tag: "v5.9.3"},
		{Name: "qa", Started: true, Claude: "2.1.87", Tag: "v5.9.4"},
		{Name: "planner", Tag: "v5.9.0"}, // non démarré : seul l'écart de template compte
		{Name: "inconnu", Started: true}, // versions inconnues : aucun écart
	}
	out, h := ApplyTeamHead(lines, "")
	if h.Tpl != "v5.9.4" || h.Claude != "2.1.87" || h.Odd != 2 {
		t.Errorf("en-tête %+v, attendu tpl v5.9.4, claude 2.1.87, 2 écarts", h)
	}
	if got := strings.Join(out[1].Odd, ","); got != "tpl,claude" {
		t.Errorf("dev : %q", got)
	}
	if len(out[2].Odd) != 0 || len(out[4].Odd) != 0 || strings.Join(out[3].Odd, ",") != "tpl" {
		t.Errorf("écarts : qa %v, planner %v, inconnu %v", out[2].Odd, out[3].Odd, out[4].Odd)
	}
	// projet fermé : pas de claude de leader, donc pas d'écart claude
	_, h2 := ApplyTeamHead([]TeamLine{{Name: "dev", Started: true, Claude: "x", Tag: "v5.9.4"}}, "v5.9.4")
	if h2.Claude != "" || h2.Odd != 0 {
		t.Errorf("projet fermé : %+v", h2)
	}
}
