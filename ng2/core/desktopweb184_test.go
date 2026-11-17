package core

import (
	"strings"
	"testing"
)

// LOT-4.4 — mode d'accès `access: token | none` du réglage desktop_web : aller-retour du modèle « Ce poste »,
// validation de backends.yaml. Plan : _work/reports/planner-web-sans-jeton-20261117-124406.md (§6). TDD.

func TestHubModelDesktopWebAccessRoundTrip(t *testing.T) {
	read := func(src, goos string) map[string]any {
		m, err := ReadHubModel([]byte(src), "mon-pc", goos)
		if err != nil {
			t.Fatal(err)
		}
		return hubJSON(t, m)
	}
	if got := dig(read("hubs:\n  mon-pc:\n    desktop_web: { listen: '8787' }\n", "linux"), "settings", "desktop_web", "access"); got != nil && got != "" {
		t.Errorf("access absent du fichier : %v, attendu absent", got)
	}
	for _, v := range []string{"none", "token"} {
		if got := dig(read("hubs:\n  mon-pc:\n    desktop_web: { listen: '8787', access: "+v+" }\n", "linux"), "settings", "desktop_web", "access"); got != v {
			t.Errorf("access: %s relu : %v", v, got)
		}
	}
	// surcharge par système : valeur effective et provenance
	ov := read("hubs:\n  mon-pc:\n    desktop_web: { listen: '8787', access: token }\n    windows:\n      desktop_web: { access: none }\n", "windows")
	if dig(ov, "overrides", "windows", "settings", "desktop_web", "access") != "none" || dig(ov, "effective", "desktop_web") != "windows" {
		t.Errorf("surcharge windows : %v / %v", dig(ov, "overrides", "windows", "settings", "desktop_web", "access"), dig(ov, "effective", "desktop_web"))
	}
	// écriture par « Ce poste »
	out, err := ApplyHubChanges([]byte("hubs:\n  mon-pc:\n    desktop_web: { listen: '8787' }\n"), "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"desktop_web", "access"}, Value: "none"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := dig(read(string(out), "linux"), "settings", "desktop_web", "access"); got != "none" {
		t.Errorf("access écrit puis relu : %v\n%s", got, out)
	}
}

func TestValidateBackendsDesktopWebAccess(t *testing.T) {
	val := func(y string) []ConfigProblem { return ValidateConfig(ConfBackends, []byte(y)) }
	base := "defaults:\n  browser_server: { listen: off }\n  desktop_web: "
	for _, ok := range []string{"{ listen: '8787', access: none }", "{ listen: '8787', access: token }", "{ listen: '8787' }"} {
		if e := problemsOf(val(base+ok+"\n"), SevError); len(e) != 0 {
			t.Errorf("%s refusé : %v", ok, e)
		}
	}
	e := problemsOf(val(base+"{ listen: '8787', access: anonyme }\n"), SevError)
	if len(e) == 0 || !strings.Contains(strings.Join(e, " "), "access") {
		t.Errorf("valeur inconnue acceptée ou erreur sans « access » : %v", e)
	}
	// none sur une écoute réseau sans adresse publique https : erreur de réglage
	e = problemsOf(val(base+"{ listen: '0.0.0.0:8787', allow_lan: true, access: none }\n"), SevError)
	if len(e) == 0 || !strings.Contains(strings.Join(e, " "), "sans jeton") {
		t.Errorf("none en écoute réseau sans https accepté : %v", e)
	}
	if e := problemsOf(val(base+"{ listen: '0.0.0.0:8787', allow_lan: true, access: none, url: 'https://launcher.test' }\n"), SevError); len(e) != 0 {
		t.Errorf("none avec adresse publique https refusé : %v", e)
	}
}
