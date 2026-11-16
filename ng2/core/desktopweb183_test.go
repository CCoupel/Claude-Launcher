package core

import (
	"reflect"
	"strings"
	"testing"
)

// #183 (LOT-2.1) — commutateur `desktop_web.enabled`. Contrat : contracts/desktop-web.md
// (« Réglage desktop_web de backends.yaml »). Tests écrits depuis le contrat (TDD).

// Table `enabled` × `listen` du contrat.
func TestDesktopWebEnabledListenTable(t *testing.T) {
	cases := []struct {
		name, block string
		off, bad    bool
		listen      string
	}{
		{"enabled absent, listen vide", "{}", true, false, ""},
		{"enabled absent, listen off", "{ listen: off }", true, false, ""},
		{"enabled absent, listen renseigné (v6.2.0)", "{ listen: '8787' }", false, false, "127.0.0.1:8787"},
		{"enabled false, listen renseigné", "{ enabled: false, listen: '8787' }", true, false, ""},
		{"enabled false, listen off", "{ enabled: false, listen: off }", true, false, ""},
		{"enabled false, listen vide", "{ enabled: false }", true, false, ""},
		{"enabled true, listen renseigné", "{ enabled: true, listen: '8787' }", false, false, "127.0.0.1:8787"},
		{"enabled true, listen hôte:port", "{ enabled: true, listen: '127.0.0.1:8788' }", false, false, "127.0.0.1:8788"},
		{"enabled true, listen vide", "{ enabled: true }", true, true, ""},
		{"enabled true, listen off", "{ enabled: true, listen: off }", true, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := dwFile(t, "defaults: { browser_server: { listen: off }, desktop_web: "+c.block+" }")
			got, err := resolveDesktopWebFor(f, "PC", "linux")
			if c.bad {
				if err == nil || !strings.Contains(err.Error(), "sans adresse d'écoute") {
					t.Fatalf("attendu l'erreur « interface web activée sans adresse d'écoute » : %+v %v", got, err)
				}
				if !got.Off {
					t.Errorf("en erreur l'interface doit rester éteinte : %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if got.Off != c.off || got.Listen != c.listen {
				t.Errorf("Off=%v Listen=%q, attendu Off=%v Listen=%q", got.Off, got.Listen, c.off, c.listen)
			}
		})
	}
}

// `enabled: false` éteint sans perdre `listen` : l'écriture conserve l'adresse, rallumer la retrouve.
func TestDesktopWebToggleKeepsListen(t *testing.T) {
	src := "hubs:\n  mon-pc:\n    desktop_web: { listen: '127.0.0.1:8787' }\n"
	off, err := ApplyHubChanges([]byte(src), "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"desktop_web", "enabled"}, Value: false}})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ReadHubModel(off, "mon-pc", "linux")
	v := hubJSON(t, m)
	if dig(v, "settings", "desktop_web", "enabled") != false || dig(v, "settings", "desktop_web", "listen") != "127.0.0.1:8787" {
		t.Fatalf("éteint : enabled/listen = %v / %v\n%s", dig(v, "settings", "desktop_web", "enabled"), dig(v, "settings", "desktop_web", "listen"), off)
	}
	if c, err := resolveDesktopWebFor(readFileOf(t, off), "mon-pc", "linux"); err != nil || !c.Off {
		t.Errorf("enabled:false doit éteindre : %+v %v", c, err)
	}
	on, err := ApplyHubChanges(off, "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"desktop_web", "enabled"}, Value: true}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := resolveDesktopWebFor(readFileOf(t, on), "mon-pc", "linux")
	if err != nil || c.Off || c.Listen != "127.0.0.1:8787" {
		t.Errorf("rallumé : %+v %v, attendu 127.0.0.1:8787", c, err)
	}
}

func readFileOf(t *testing.T, b []byte) backendsFile { t.Helper(); return dwFile(t, string(b)) }

// Aller-retour du modèle « Ce poste » : settings.desktop_web.enabled, absent / true / false, et surcharge par système.
func TestHubModelDesktopWebEnabledRoundTrip(t *testing.T) {
	read := func(src, goos string) map[string]any {
		m, err := ReadHubModel([]byte(src), "mon-pc", goos)
		if err != nil {
			t.Fatal(err)
		}
		return hubJSON(t, m)
	}
	if got := dig(read("hubs:\n  mon-pc:\n    desktop_web: { listen: '8787' }\n", "linux"), "settings", "desktop_web", "enabled"); got != nil {
		t.Errorf("enabled absent du fichier : %v, attendu absent (nil)", got)
	}
	if got := dig(read("hubs:\n  mon-pc:\n    desktop_web: { enabled: true, listen: '8787' }\n", "linux"), "settings", "desktop_web", "enabled"); got != true {
		t.Errorf("enabled:true relu : %v", got)
	}
	if got := dig(read("hubs:\n  mon-pc:\n    desktop_web: { enabled: false, listen: '8787' }\n", "linux"), "settings", "desktop_web", "enabled"); got != false {
		t.Errorf("enabled:false relu : %v (false ne doit pas se confondre avec absent)", got)
	}
	ov := read("hubs:\n  mon-pc:\n    desktop_web: { enabled: true, listen: '8787' }\n    windows:\n      desktop_web: { enabled: false }\n", "windows")
	if got := dig(ov, "overrides", "windows", "settings", "desktop_web", "enabled"); got != false {
		t.Errorf("surcharge windows enabled : %v, attendu false", got)
	}
	if dig(ov, "effective", "desktop_web") != "windows" {
		t.Errorf("effective.desktop_web = %v, attendu windows", dig(ov, "effective", "desktop_web"))
	}
}

// Priorité champ par champ : `enabled` de la machine l'emporte sur celui des défauts, et sur celui du système la machine.
func TestDesktopWebEnabledPrecedence(t *testing.T) {
	f := dwFile(t, `
defaults: { browser_server: { listen: off }, desktop_web: { enabled: false, listen: '8790' } }
hubs:
  PC:
    desktop_web: { enabled: true }
    windows: { desktop_web: { enabled: false } }
`)
	if c, err := resolveDesktopWebFor(f, "PC", "linux"); err != nil || c.Off || c.Listen != "127.0.0.1:8790" {
		t.Errorf("machine enabled:true + listen des défauts : %+v %v", c, err)
	}
	if c, err := resolveDesktopWebFor(f, "PC", "windows"); err != nil || !c.Off {
		t.Errorf("système enabled:false : %+v %v", c, err)
	}
	if c, err := resolveDesktopWebFor(f, "AUTRE", "linux"); err != nil || !c.Off {
		t.Errorf("défauts enabled:false : %+v %v", c, err)
	}
}

// Indépendance : le commutateur n'altère ni browser_server (réglage effectif identique) ni son bloc dans le fichier.
func TestDesktopWebEnabledIndependentOfBrowserServer(t *testing.T) {
	base := "defaults:\n  browser_server: { listen: '127.0.0.1:8765' }\nhubs:\n  mon-pc:\n    desktop_web: { listen: '8787' }\n"
	before, _ := resolveBrowserServerFor(dwFile(t, base), "mon-pc", "linux")
	for _, v := range []bool{false, true} {
		out, err := ApplyHubChanges([]byte(base), "MON-PC", []ConfigChange{
			{Op: "set", At: []string{"desktop_web", "enabled"}, Value: v}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "browser_server: {listen: '127.0.0.1:8765'}") {
			t.Errorf("bloc browser_server altéré (enabled=%v) :\n%s", v, out)
		}
		after, err := resolveBrowserServerFor(dwFile(t, string(out)), "mon-pc", "linux")
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Errorf("browser_server effectif modifié (enabled=%v) : %+v → %+v %v", v, before, after, err)
		}
	}
}

// Validation de backends.yaml : enabled:true sans adresse = erreur de réglage ; enabled:false avec adresse = rien.
func TestValidateBackendsDesktopWebEnabled(t *testing.T) {
	val := func(y string) []ConfigProblem { return ValidateConfig(ConfBackends, []byte(y)) }
	if e := problemsOf(val("defaults:\n  browser_server: { listen: off }\n  desktop_web: { enabled: true }\n"), SevError); len(e) == 0 {
		t.Error("enabled:true sans listen accepté")
	}
	if ps := val("defaults:\n  browser_server: { listen: off }\n  desktop_web: { enabled: false, listen: '8787' }\n"); len(problemsOf(ps, SevError)) != 0 {
		t.Errorf("enabled:false avec listen refusé : %v", ps)
	}
	if ps := val("defaults:\n  browser_server: { listen: off }\n  desktop_web: { enabled: true, listen: '8787' }\n"); len(problemsOf(ps, SevError)) != 0 {
		t.Errorf("enabled:true avec listen refusé : %v", ps)
	}
}

// Cohérence (#183) : avec une surcharge hubs.<machine>.windows.<clé>, le modèle expose la valeur effective et sa
// provenance (effective.<clé> = « windows »), et une modification faite à la source indiquée change la valeur
// réellement appliquée — aucune valeur masquée par la surcharge.
func TestHubModelOverrideEffectiveAndEditApplies(t *testing.T) {
	src := "hubs:\n  mon-pc:\n    browser_server: { listen: '8765' }\n    desktop_web: { listen: '8787' }\n" +
		"    windows:\n      browser_server: { listen: '9765' }\n      desktop_web: { listen: '9787' }\n"
	m, err := ReadHubModel([]byte(src), "mon-pc", "windows")
	if err != nil {
		t.Fatal(err)
	}
	v := hubJSON(t, m)
	for key, want := range map[string]string{"browser_server": "9765", "desktop_web": "9787"} {
		if got := dig(v, "effective", key); got != "windows" {
			t.Errorf("effective.%s = %v, attendu windows (provenance de la valeur appliquée)", key, got)
		}
		if got := dig(v, "overrides", "windows", "settings", key, "listen"); got != want {
			t.Errorf("overrides.windows.settings.%s.listen = %v, attendu %s (valeur effective exposée)", key, got, want)
		}
	}
	// la valeur de base reste exposée à part, sans être confondue avec l'effective
	if dig(v, "settings", "browser_server", "listen") != "8765" || dig(v, "settings", "desktop_web", "listen") != "8787" {
		t.Errorf("valeurs communes altérées : %v", dig(v, "settings"))
	}
	// modification à la source de provenance : la valeur appliquée sous Windows change
	out, err := ApplyHubChanges([]byte(src), "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"windows", "browser_server", "listen"}, Value: "9766"},
		{Op: "set", At: []string{"windows", "desktop_web", "listen"}, Value: "9788"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := dwFile(t, string(out))
	if bs, err := resolveBrowserServerFor(f, "mon-pc", "windows"); err != nil || bs.Listen != "127.0.0.1:9766" {
		t.Errorf("browser_server appliqué sous Windows : %+v %v, attendu 127.0.0.1:9766", bs, err)
	}
	if dw, err := resolveDesktopWebFor(f, "mon-pc", "windows"); err != nil || dw.Listen != "127.0.0.1:9788" {
		t.Errorf("desktop_web appliqué sous Windows : %+v %v, attendu 127.0.0.1:9788", dw, err)
	}
	// la valeur commune, elle, n'a pas bougé (Linux)
	if bs, _ := resolveBrowserServerFor(f, "mon-pc", "linux"); bs.Listen != "127.0.0.1:8765" {
		t.Errorf("valeur Linux modifiée à tort : %+v", bs)
	}
	// sous Linux, la surcharge windows ne vaut pas : provenance vide
	lm, _ := ReadHubModel([]byte(src), "mon-pc", "linux")
	if dig(hubJSON(t, lm), "effective", "browser_server") != "" || dig(hubJSON(t, lm), "effective", "desktop_web") != "" {
		t.Errorf("provenance sous Linux : %v", dig(hubJSON(t, lm), "effective"))
	}
	// éteindre l'interface depuis la source effective (surcharge) l'éteint réellement sous Windows
	off, err := ApplyHubChanges([]byte(src), "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"windows", "desktop_web", "enabled"}, Value: false}})
	if err != nil {
		t.Fatal(err)
	}
	if dw, err := resolveDesktopWebFor(dwFile(t, string(off)), "mon-pc", "windows"); err != nil || !dw.Off {
		t.Errorf("enabled:false de la surcharge sans effet : %+v %v", dw, err)
	}
}
