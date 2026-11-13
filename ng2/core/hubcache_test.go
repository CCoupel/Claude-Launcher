package core

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// #127 : hub_cache (#125) est un chemin vu d'un système ; il se déclare dans
// hubs.<machine> ou, surtout, dans sa surcharge par système (.windows), sans
// être signalé comme clé inconnue ; la surcharge l'emporte pour ce système.
func TestHubCachePerOS(t *testing.T) {
	var f backendsFile
	src := `hubs:
  MSI:
    hub_cache: /commun
    windows:
      hub_cache: '\\wsl.localhost\Ubuntu\home\moi\.cache\claude-launcher-ng2'
      browser_server: { listen: 127.0.0.1:8766 }
`
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if ws := hubConfigWarnings(f); len(ws) != 0 {
		t.Errorf("hub_cache signalé : %v", ws)
	}
	if got := hubCacheOf(f.Hubs, "MSI", "windows"); !strings.HasPrefix(got, `\\wsl.localhost\`) {
		t.Errorf("windows : %q", got)
	}
	if got := hubCacheOf(f.Hubs, "msi", "linux"); got != "/commun" {
		t.Errorf("linux (sans surcharge) : %q", got)
	}
	if got := hubCacheOf(f.Hubs, "AUTRE", "windows"); got != "" {
		t.Errorf("autre machine : %q", got)
	}
	// une vraie faute de frappe reste signalée
	var g backendsFile
	_ = yaml.Unmarshal([]byte("hubs:\n  MSI:\n    windows:\n      hub_cahce: x\n"), &g)
	if ws := hubConfigWarnings(g); len(ws) != 1 || !strings.Contains(ws[0], "hub_cache") {
		t.Errorf("faute de frappe : %v", ws)
	}
}
