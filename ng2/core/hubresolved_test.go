package core

import "testing"

const resolvedYAML = `defaults:
  browser_server: { listen: "7000", allow: [/d] }
hubs:
  MSI:
    browser_server: { listen: "9999", url: "http://commun" }
    desktop_web: { listen: "8787", enabled: true }
    windows:
      browser_server: { listen: "8081" }
      desktop_web: { enabled: false }
`

// #183 : le modèle porte les valeurs EFFECTIVES (champ par champ) et leur provenance.
func TestHubModelResolvedFieldByField(t *testing.T) {
	m, err := ReadHubModel([]byte(resolvedYAML), "MSI", "windows")
	if err != nil {
		t.Fatal(err)
	}
	r := m.Resolved
	if r.BrowserServer.Listen != "8081" || r.Sources["browser_server.listen"] != "windows" {
		t.Errorf("listen effectif : %q (%s), attendu 8081 (windows)", r.BrowserServer.Listen, r.Sources["browser_server.listen"])
	}
	if r.BrowserServer.URL != "http://commun" || r.Sources["browser_server.url"] != "hub" {
		t.Errorf("url : %q (%s)", r.BrowserServer.URL, r.Sources["browser_server.url"])
	}
	if len(r.BrowserServer.Allow) != 1 || r.Sources["browser_server.allow"] != "defaults" {
		t.Errorf("allow : %v (%s)", r.BrowserServer.Allow, r.Sources["browser_server.allow"])
	}
	if e := r.DesktopWeb.Enabled; e == nil || *e || r.Sources["desktop_web.enabled"] != "windows" {
		t.Errorf("enabled effectif : %v (%s), attendu false (windows)", e, r.Sources["desktop_web.enabled"])
	}
	if r.DesktopWeb.Listen != "8787" || r.Sources["desktop_web.listen"] != "hub" {
		t.Errorf("desktop_web.listen : %q (%s)", r.DesktopWeb.Listen, r.Sources["desktop_web.listen"])
	}
	// vu de Linux : la surcharge Windows ne s'applique pas
	m, _ = ReadHubModel([]byte(resolvedYAML), "MSI", "linux")
	if m.Resolved.BrowserServer.Listen != "9999" || m.Resolved.Sources["browser_server.listen"] != "hub" {
		t.Errorf("linux : %q (%s), attendu 9999 (hub)", m.Resolved.BrowserServer.Listen, m.Resolved.Sources["browser_server.listen"])
	}
}

// L'écriture vise la couche qui gagne : la valeur affichée (effective) n'est jamais masquée.
func TestApplyHubChangesForWritesWinningLayer(t *testing.T) {
	out, err := ApplyHubChangesFor([]byte(resolvedYAML), "MSI", "windows", []ConfigChange{
		{Op: "set", At: []string{"browser_server", "listen"}, Value: "8090"},        // surchargé par windows
		{Op: "set", At: []string{"browser_server", "url"}, Value: "http://nouveau"}, // seulement dans hub
		{Op: "set", At: []string{"desktop_web", "enabled"}, Value: true},            // surchargé par windows
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ReadHubModel(out, "MSI", "windows")
	r := m.Resolved
	if r.BrowserServer.Listen != "8090" || r.Sources["browser_server.listen"] != "windows" {
		t.Errorf("listen : %q (%s)", r.BrowserServer.Listen, r.Sources["browser_server.listen"])
	}
	if hub, _ := ReadHubModel(out, "MSI", "linux"); hub.Resolved.BrowserServer.Listen != "9999" {
		t.Errorf("la couche commune a changé : %q (le hub Linux doit garder 9999)", hub.Resolved.BrowserServer.Listen)
	}
	if r.BrowserServer.URL != "http://nouveau" || r.Sources["browser_server.url"] != "hub" {
		t.Errorf("url : %q (%s)", r.BrowserServer.URL, r.Sources["browser_server.url"])
	}
	if e := r.DesktopWeb.Enabled; e == nil || !*e || r.Sources["desktop_web.enabled"] != "windows" {
		t.Errorf("enabled : %v (%s)", e, r.Sources["desktop_web.enabled"])
	}
}

// Remplacement de l'objet effectif (forme de l'interface) : routage champ par champ ; champ absent retiré de la couche qui le pose.
func TestApplyHubChangesForObjectReplace(t *testing.T) {
	out, err := ApplyHubChangesFor([]byte(resolvedYAML), "MSI", "windows", []ConfigChange{
		{Op: "set", At: []string{"browser_server"}, Value: map[string]any{"listen": "8082"}}, // url absent : retiré de hub
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ReadHubModel(out, "MSI", "windows")
	r := m.Resolved
	if r.BrowserServer.Listen != "8082" || r.Sources["browser_server.listen"] != "windows" {
		t.Errorf("listen : %q (%s)", r.BrowserServer.Listen, r.Sources["browser_server.listen"])
	}
	if r.BrowserServer.URL != "" {
		t.Errorf("url retirée attendue : %q", r.BrowserServer.URL)
	}
}

// Sans surcharge pour ce système : comportement inchangé (couche commune).
func TestApplyHubChangesForWithoutOverrideUnchanged(t *testing.T) {
	out, err := ApplyHubChangesFor([]byte(resolvedYAML), "MSI", "linux", []ConfigChange{
		{Op: "set", At: []string{"browser_server", "listen"}, Value: "9000"}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ReadHubModel(out, "MSI", "linux")
	if m.Resolved.BrowserServer.Listen != "9000" || m.Resolved.Sources["browser_server.listen"] != "hub" {
		t.Errorf("listen linux : %q (%s)", m.Resolved.BrowserServer.Listen, m.Resolved.Sources["browser_server.listen"])
	}
	if w, _ := ReadHubModel(out, "MSI", "windows"); w.Resolved.BrowserServer.Listen != "8081" {
		t.Errorf("la surcharge windows a changé : %q", w.Resolved.BrowserServer.Listen)
	}
}
