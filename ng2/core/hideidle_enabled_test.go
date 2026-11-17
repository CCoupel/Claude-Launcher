package core

import (
	"os"
	"path/filepath"
	"testing"
)

// #188 : TEAM_HIDE_IDLE_ENABLED — défaut = v6.2.0, désactivation sans perdre le délai, règle de visibilité.
func TestHideIdleEnabledDefaultAndParsing(t *testing.T) {
	d := t.TempDir()
	read := func(content string) Config {
		p := filepath.Join(d, "config.conf")
		_ = os.WriteFile(p, []byte(content), 0o600)
		return loadConfigAt(p)
	}
	if c := read("TEAM_HIDE_IDLE=45\n"); c.TeamHideIdleDisabled || c.HideIdleArg() != 45 || !c.HideIdleState().Enabled {
		t.Errorf("absent : activé (v6.2.0) attendu : %+v / %d", c.HideIdleState(), c.HideIdleArg())
	}
	c := read("TEAM_HIDE_IDLE=45\nTEAM_HIDE_IDLE_ENABLED=0\n")
	if !c.TeamHideIdleDisabled || c.HideIdleArg() != -1 || c.TeamHideIdle != 45 || c.HideIdleState() != (HideIdleInfo{Enabled: false, Delay: 45}) {
		t.Errorf("désactivé : délai conservé, argument -1 attendus : %+v / %d", c.HideIdleState(), c.HideIdleArg())
	}
	if c := read("TEAM_HIDE_IDLE_ENABLED=1\nTEAM_HIDE_IDLE=0\n"); c.TeamHideIdleDisabled || c.HideIdleArg() != 0 {
		t.Errorf("activé, délai 0 (jamais) : %d", c.HideIdleArg())
	}
	// lecteur sans bash (Windows)
	if out := confLite("TEAM_HIDE_IDLE_ENABLED=0\n", "/h"); !containsLine(out, "hideidleon\t0") {
		t.Errorf("confLite : %q", out)
	}
	if out := confLite("", "/h"); !containsLine(out, "hideidleon\t1") {
		t.Errorf("confLite défaut : %q", out)
	}
	// modèle des Réglages : clé bool, défaut vrai
	found := false
	for _, s := range confSettings {
		if s.Key == "TEAM_HIDE_IDLE_ENABLED" && s.Type == "bool" && s.Default == true && s.Group == "teams" {
			found = true
		}
	}
	if !found {
		t.Error("clé TEAM_HIDE_IDLE_ENABLED absente du modèle des Réglages")
	}
}

func containsLine(out, want string) bool {
	for _, l := range splitLines(out) {
		if l == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	return append(out, cur)
}

func TestDecidePaneVisHideIdleDisabled(t *testing.T) {
	panes := []VisPane{
		{ID: "%1", Protected: true},
		{ID: "%2", IdleAt: 100, Hidden: true}, // caché à la main
		{ID: "%3", IdleAt: 100, Hidden: true}, // caché automatiquement
		{ID: "%4", IdleAt: 100},               // fixé visible
	}
	choices := map[string]string{"%2": visHidden, "%4": visPinned}
	// activé : comportement d'avant
	d := DecidePaneVis(panes, choices, 30, 1000)
	if !d.Hide["%2"] || !d.Hide["%3"] || d.Hide["%4"] || d.Choices["%2"] != visHidden {
		t.Errorf("activé : %+v", d)
	}
	// désactivé (-1) : rien n'est caché, la marque « h » est effacée, « s » reste
	d = DecidePaneVis(panes, choices, -1, 1000)
	if len(d.Hide) != 0 {
		t.Errorf("désactivé : panes cachés %v", d.Hide)
	}
	if _, ok := d.Choices["%2"]; ok || d.Choices["%4"] != visPinned {
		t.Errorf("désactivé : marques %v (h effacée, s conservée attendues)", d.Choices)
	}
	// délai 0 (jamais) mais activé : le choix « h » reste honoré (v6.2.0)
	if d := DecidePaneVis(panes, choices, 0, 1000); !d.Hide["%2"] {
		t.Error("activé avec délai 0 : le masquage à la main doit rester honoré")
	}
}
