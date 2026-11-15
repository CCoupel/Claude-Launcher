package core

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func dwFile(t *testing.T, y string) backendsFile {
	t.Helper()
	var f backendsFile
	if err := yaml.Unmarshal([]byte(y), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDesktopWebPrecedenceAndForms(t *testing.T) {
	f := dwFile(t, `
defaults: { desktop_web: { listen: "8770" } }
hubs:
  PC:
    desktop_web: { listen: "127.0.0.1:8771" }
    windows: { desktop_web: { listen: "8772" } }
`)
	cases := []struct{ host, goos, want string }{
		{"AUTRE", "linux", "127.0.0.1:8770"}, // défauts : port seul = boucle locale
		{"PC", "linux", "127.0.0.1:8771"},    // machine
		{"PC", "windows", "127.0.0.1:8772"},  // machine + système
	}
	for _, c := range cases {
		got, err := resolveDesktopWebFor(f, c.host, c.goos)
		if err != nil || got.Off || got.Listen != c.want || !got.Loopback || got.Warn != "" {
			t.Errorf("%s/%s : %+v %v, attendu %s", c.host, c.goos, got, err, c.want)
		}
	}
	if got, _ := resolveDesktopWebFor(backendsFile{}, "PC", "linux"); !got.Off {
		t.Errorf("défaut : %+v, attendu off", got)
	}
	if got, _ := resolveDesktopWebFor(dwFile(t, "defaults: { desktop_web: { listen: off } }"), "PC", "linux"); !got.Off {
		t.Errorf("off explicite : %+v", got)
	}
}

func TestDesktopWebRefusals(t *testing.T) {
	for _, y := range []string{
		"defaults: { desktop_web: { listen: \"abc\" } }",
		"defaults: { desktop_web: { listen: \"99999\" } }",
		"defaults: { browser_server: { listen: \"127.0.0.1:8770\" }, desktop_web: { listen: \"8770\" } }", // = browser_server
		"defaults: { desktop_web: { listen: \"8765\" } }",                                               // = défaut de browser_server
	} {
		if _, err := resolveDesktopWebFor(dwFile(t, y), "PC", "linux"); err == nil {
			t.Errorf("accepté : %s", y)
		}
	}
	// browser_server off : plus de conflit
	if _, err := resolveDesktopWebFor(dwFile(t, "defaults: { browser_server: { listen: off }, desktop_web: { listen: \"8765\" } }"), "PC", "linux"); err != nil {
		t.Errorf("browser_server off : %v", err)
	}
}

func TestDesktopWebWarnsOutsideLoopbackAndAllowedHosts(t *testing.T) {
	y := "hubs:\n  PC:\n    desktop_web: { listen: \"0.0.0.0:8770\" }\n"
	c, err := resolveDesktopWebFor(dwFile(t, y), "PC", "linux")
	if err != nil || c.Loopback || !strings.Contains(c.Warn, "hors de la boucle locale") {
		t.Fatalf("%+v %v", c, err)
	}
	if !c.AllowedHosts("PC")["pc"] || !c.AllowedHosts("PC")["localhost"] {
		t.Errorf("hôtes autorisés : %v", c.AllowedHosts("PC"))
	}
}
