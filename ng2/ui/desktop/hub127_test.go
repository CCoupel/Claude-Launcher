package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #127 : réglages par machine ignorés (clé inconnue de hubs:) annoncés au
// démarrage (bandeau, un clic ouvre le diagnostic), masquables.
func TestHubConfigWarningsBanner(t *testing.T) {
	sandbox(t, "", "alpha")
	_ = os.WriteFile(core.BackendsPath(), []byte("hubs:\n  "+hostForTest()+":\n    windows:\n      browser_sever: { listen: 127.0.0.1:9 }\n"), 0o644)
	var r recorder
	a := NewApp(r.emit)
	var found *Alert
	for _, al := range a.Snapshot().Alerts {
		if al.Kind == "hubcfg" {
			al := al
			found = &al
		}
	}
	if found == nil || !found.Click || !strings.Contains(found.Text, "browser_server") {
		t.Fatalf("bandeau : %+v", a.Snapshot().Alerts)
	}
	if _, err := a.Dispatch("dismiss", json.RawMessage(`{"kind":"hubcfg"}`)); err != nil {
		t.Fatal(err)
	}
	for _, al := range a.Snapshot().Alerts {
		if al.Kind == "hubcfg" {
			t.Error("bandeau non masqué")
		}
	}
}

func hostForTest() string {
	h, _ := os.Hostname()
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	return h
}
