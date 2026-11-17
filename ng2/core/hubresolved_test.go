package core

import (
	"strings"
	"testing"
)

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

const resolvedPathsYAML = `defaults:
  browser_paths: [ { container: /d, host: "D:\\d", os: windows } ]
hubs:
  MSI:
    browser_device: dev-hub
    browser_paths: [ { container: /h, host: "H:\\h", os: windows } ]
    windows:
      browser_device: dev-win
      browser_paths: [ { container: /w, host: "W:\\w", os: windows } ]
`

// browser_paths et browser_device : valeur effective par système, provenance, écriture sur la couche gagnante.
func TestHubResolvedBrowserPathsAndDevice(t *testing.T) {
	m, _ := ReadHubModel([]byte(resolvedPathsYAML), "MSI", "windows")
	r := m.Resolved
	if len(r.BrowserPaths) != 1 || r.BrowserPaths[0].Container != "/w" || r.Sources["browser_paths"] != "windows" {
		t.Errorf("browser_paths windows : %v (%s)", r.BrowserPaths, r.Sources["browser_paths"])
	}
	if r.BrowserDevice != "dev-win" || r.Sources["browser_device"] != "windows" {
		t.Errorf("browser_device windows : %q (%s)", r.BrowserDevice, r.Sources["browser_device"])
	}
	l, _ := ReadHubModel([]byte(resolvedPathsYAML), "MSI", "linux")
	if l.Resolved.BrowserPaths[0].Container != "/h" || l.Resolved.Sources["browser_paths"] != "hub" || l.Resolved.BrowserDevice != "dev-hub" {
		t.Errorf("linux : %+v", l.Resolved)
	}
	d, _ := ReadHubModel([]byte("defaults:\n  browser_paths: [ { container: /d, host: /d, os: linux } ]\n"), "MSI", "linux")
	if d.Resolved.Sources["browser_paths"] != "defaults" || len(d.Resolved.BrowserPaths) != 1 {
		t.Errorf("defaults : %+v", d.Resolved)
	}
	// écriture : la surcharge Windows est modifiée, la couche commune (hub Linux) ne bouge pas
	out, err := ApplyHubChangesFor([]byte(resolvedPathsYAML), "MSI", "windows", []ConfigChange{
		{Op: "set", At: []string{"browser_device"}, Value: "dev-win2"},
		{Op: "set", At: []string{"browser_paths"}, Value: []map[string]any{{"container": "/w2", "host": "W:\\w2", "os": "windows"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := ReadHubModel(out, "MSI", "windows")
	if w.Resolved.BrowserDevice != "dev-win2" || w.Resolved.BrowserPaths[0].Container != "/w2" || w.Resolved.Sources["browser_device"] != "windows" {
		t.Errorf("windows après écriture : %+v", w.Resolved)
	}
	if h, _ := ReadHubModel(out, "MSI", "linux"); h.Resolved.BrowserDevice != "dev-hub" || h.Resolved.BrowserPaths[0].Container != "/h" {
		t.Errorf("la couche commune a changé : %+v", h.Resolved)
	}
	// retour au défaut : retiré de la surcharge et de la couche commune
	out, err = ApplyHubChangesFor([]byte(resolvedPathsYAML), "MSI", "windows", []ConfigChange{{Op: "delete", At: []string{"browser_device"}}})
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := ReadHubModel(out, "MSI", "windows"); w.Resolved.BrowserDevice != "" {
		t.Errorf("browser_device après delete : %q", w.Resolved.BrowserDevice)
	}
}

// M1 : browser_paths et browser_device font partie de l'empreinte des rubriques web (un client web ne les modifie pas).
func TestWebExposureCoversBrowserPathsAndDevice(t *testing.T) {
	base := resolvedPathsYAML
	for name, changed := range map[string]string{
		"browser_device": strings.Replace(base, "dev-win", "dev-autre", 1),
		"browser_paths":  strings.Replace(base, "/w,", "/w2,", 1),
		"defaults":       strings.Replace(base, "/d,", "/d2,", 1),
	} {
		if changed == base {
			t.Fatalf("%s : le test ne modifie rien", name)
		}
		if !WebExposureChanged([]byte(base), []byte(changed)) {
			t.Errorf("%s modifié : exposition vue comme inchangée", name)
		}
	}
	if WebExposureChanged([]byte(base), []byte(base)) {
		t.Error("contenu identique vu comme changé")
	}
}

// M2 : seul enabled: false explicite donne le libellé « enabled: false » ; listen: off garde celui de v6.2.0.
func TestDesktopWebExplicitOffOnlyWhenEnabledFalse(t *testing.T) {
	a, _ := resolveDesktopWebFor(dwFile(t, `defaults: { desktop_web: { listen: off } }`), "PC", "linux")
	if !a.Off || a.ExplicitOff {
		t.Errorf("listen: off sans enabled : %+v", a)
	}
	b, _ := resolveDesktopWebFor(dwFile(t, `defaults: { desktop_web: { enabled: false, listen: "8787" } }`), "PC", "linux")
	if !b.Off || !b.ExplicitOff || b.Kept != "8787" {
		t.Errorf("enabled: false : %+v", b)
	}
}

// M3 : delete d'un champ présent dans la machine ET dans la surcharge : retiré des deux, rien ne réapparaît.
func TestApplyHubChangesForDeleteRemovesAllLayers(t *testing.T) {
	out, err := ApplyHubChangesFor([]byte(resolvedYAML), "MSI", "windows", []ConfigChange{
		{Op: "delete", At: []string{"browser_server", "listen"}}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ReadHubModel(out, "MSI", "windows")
	// hub (9999) et windows (8081) retirés ; reste defaults (7000)
	if m.Resolved.BrowserServer.Listen != "7000" || m.Resolved.Sources["browser_server.listen"] != "defaults" {
		t.Errorf("listen après delete : %q (%s), attendu 7000 (defaults) : aucune valeur masquée ne doit réapparaître",
			m.Resolved.BrowserServer.Listen, m.Resolved.Sources["browser_server.listen"])
	}
	// champ venant seulement de defaults : sans effet, sans erreur
	if _, err := ApplyHubChangesFor([]byte(resolvedYAML), "MSI", "windows", []ConfigChange{
		{Op: "delete", At: []string{"browser_server", "allow"}}}); err != nil {
		t.Errorf("delete d'un champ de defaults seulement : %v", err)
	}
}
