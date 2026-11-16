package core

import (
	"strings"
	"testing"
)

// #183 : table enabled × listen de contracts/desktop-web.md.
func TestDesktopWebEnabledTable(t *testing.T) {
	cases := []struct {
		name, y      string
		off, enabled bool
		listen, kept string
		errHas       string
	}{
		{"absent, sans listen", `defaults: { desktop_web: {} }`, true, false, "", "", ""},
		{"absent, listen off", `defaults: { desktop_web: { listen: off } }`, true, false, "", "", ""},
		{"absent, listen (v6.2.0)", `defaults: { desktop_web: { listen: "8787" } }`, false, true, "127.0.0.1:8787", "", ""},
		{"false, listen gardé", `defaults: { desktop_web: { enabled: false, listen: "8787" } }`, true, false, "", "8787", ""},
		{"false, sans listen", `defaults: { desktop_web: { enabled: false } }`, true, false, "", "", ""},
		{"true, listen", `defaults: { desktop_web: { enabled: true, listen: "8787" } }`, false, true, "127.0.0.1:8787", "", ""},
		{"true, sans listen", `defaults: { desktop_web: { enabled: true } }`, true, true, "", "", "activée sans adresse d'écoute"},
		{"true, listen off", `defaults: { desktop_web: { enabled: true, listen: off } }`, true, true, "", "", "activée sans adresse d'écoute"},
		{"machine l'emporte sur les défauts", "defaults: { desktop_web: { enabled: true, listen: \"8787\" } }\nhubs: { PC: { desktop_web: { enabled: false } } }", true, false, "", "8787", ""},
	}
	for _, c := range cases {
		got, err := resolveDesktopWebFor(dwFile(t, c.y), "PC", "linux")
		if c.errHas != "" {
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Errorf("%s : erreur %v, attendu « %s »", c.name, err, c.errHas)
			}
		} else if err != nil {
			t.Errorf("%s : erreur %v", c.name, err)
		}
		if got.Off != c.off || got.Enabled != c.enabled || got.Listen != c.listen || got.Kept != c.kept {
			t.Errorf("%s : %+v, attendu off=%v enabled=%v listen=%q kept=%q", c.name, got, c.off, c.enabled, c.listen, c.kept)
		}
	}
}

// enabled fait partie de l'empreinte d'exposition : un client web ne peut pas l'allumer.
func TestDesktopWebEnabledIsWebExposure(t *testing.T) {
	a := []byte("defaults:\n  desktop_web: { listen: \"8787\" }\n")
	b := []byte("defaults:\n  desktop_web: { listen: \"8787\", enabled: false }\n")
	if !WebExposureChanged(a, b) {
		t.Error("desktop_web.enabled modifié sans que l'exposition soit vue comme changée")
	}
}

func TestHubModelDesktopWebEnabled(t *testing.T) {
	m, err := ReadHubModel([]byte("hubs:\n  PC:\n    desktop_web: { listen: \"8787\", enabled: false }\n"), "PC", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if e := m.Settings.DesktopWeb.Enabled; e == nil || *e {
		t.Errorf("enabled du modèle : %v, attendu false", e)
	}
	m, _ = ReadHubModel([]byte("hubs:\n  PC:\n    desktop_web: { listen: \"8787\" }\n"), "PC", "linux")
	if m.Settings.DesktopWeb.Enabled != nil {
		t.Error("enabled absent du fichier doit rester null")
	}
}
