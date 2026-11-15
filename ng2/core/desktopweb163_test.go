package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// #163 (LOT-4) — réglage `desktop_web` de backends.yaml (contracts/desktop-dispatch.md) : même emplacement et
// même priorité que browser_server (hubs.<machine>.<système> > hubs.<machine> > defaults > défaut `off`),
// modèle « Ce poste », validation. Tests par l'API publique (modèle JSON, ApplyHubChanges, ValidateConfig) : la
// résolution interne n'est pas nommée par le contrat ; la priorité effective est vérifiée côté ui/desktop
// (champ `webUI.listen` de l'instantané).

func hubJSON(t *testing.T, m HubModel) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	return v
}

func dig(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// Modèle « Ce poste » : settings.desktop_web.listen et overrides.<système>.settings.desktop_web.listen.
func TestHubModelDesktopWeb(t *testing.T) {
	src := "# postes\nhubs:\n  mon-pc:\n    desktop_web: { listen: '127.0.0.1:8770' }   # interface web\n    hub_cache: /x\n    windows:\n      desktop_web: { listen: '9001' }\n"
	m, err := ReadHubModel([]byte(src), "MON-PC.domaine", "windows")
	if err != nil {
		t.Fatal(err)
	}
	v := hubJSON(t, m)
	if got := dig(v, "settings", "desktop_web", "listen"); got != "127.0.0.1:8770" {
		t.Errorf("settings.desktop_web.listen = %v, attendu 127.0.0.1:8770", got)
	}
	if got := dig(v, "overrides", "windows", "settings", "desktop_web", "listen"); got != "9001" {
		t.Errorf("overrides.windows.settings.desktop_web.listen = %v, attendu 9001", got)
	}
	keys, _ := dig(v, "overrides", "windows", "keys").([]any)
	found := false
	for _, k := range keys {
		found = found || k == "desktop_web"
	}
	if !found {
		t.Errorf("overrides.windows.keys = %v, attendu desktop_web", keys)
	}
	if dig(v, "effective", "desktop_web") != "windows" {
		t.Errorf("effective.desktop_web = %v, attendu windows (la surcharge du système vaut ici)", dig(v, "effective", "desktop_web"))
	}
	lm, _ := ReadHubModel([]byte(src), "mon-pc", "linux")
	if dig(hubJSON(t, lm), "effective", "desktop_web") != "" {
		t.Errorf("sous Linux la surcharge windows ne vaut pas : %v", dig(hubJSON(t, lm), "effective", "desktop_web"))
	}
	// section sans réglage : un objet vide, jamais absent
	em, _ := ReadHubModel([]byte("backends: {}\n"), "AUTRE", "linux")
	if _, ok := dig(hubJSON(t, em), "settings", "desktop_web").(map[string]any); !ok {
		t.Errorf("settings.desktop_web absent du modèle vide : %v", dig(hubJSON(t, em), "settings"))
	}
	if got := dig(hubJSON(t, em), "settings", "desktop_web", "listen"); got != "" {
		t.Errorf("listen d'un poste sans réglage = %v, attendu vide (défaut off)", got)
	}
}

// Écriture par « Ce poste » : le réglage est enregistré sous la machine, commentaires conservés.
func TestApplyHubChangesDesktopWeb(t *testing.T) {
	src := "# postes\nhubs:\n  mon-pc:\n    hub_cache: /x   # cache\n"
	out, err := ApplyHubChanges([]byte(src), "MON-PC", []ConfigChange{
		{Op: "set", At: []string{"desktop_web"}, Value: map[string]any{"listen": "127.0.0.1:8770"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "# cache") || !strings.Contains(string(out), "# postes") {
		t.Errorf("commentaires perdus :\n%s", out)
	}
	m, err := ReadHubModel(out, "mon-pc", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := dig(hubJSON(t, m), "settings", "desktop_web", "listen"); got != "127.0.0.1:8770" {
		t.Errorf("relu : %v\n%s", got, out)
	}
	if got := dig(hubJSON(t, m), "settings", "hub_cache"); got != "/x" {
		t.Errorf("hub_cache perdu : %v", got)
	}
}

func problemsOf(ps []ConfigProblem, sev string) []string {
	var out []string
	for _, p := range ps {
		if p.Severity == sev {
			out = append(out, p.Message)
		}
	}
	return out
}

// Validation de backends.yaml : un listen identique à celui du serveur des fichiers est refusé (deux serveurs
// distincts), un listen illisible aussi ; hors boucle locale : avertissement ; boucle locale, `off` et absence :
// rien.
func TestValidateBackendsDesktopWeb(t *testing.T) {
	val := func(y string) []ConfigProblem { return ValidateConfig(ConfBackends, []byte(y)) }

	// identique à browser_server (déclaré) : erreur
	ps := val("defaults:\n  browser_server: { listen: '127.0.0.1:8770' }\n  desktop_web: { listen: '127.0.0.1:8770' }\n")
	if e := problemsOf(ps, SevError); len(e) == 0 || !strings.Contains(strings.Join(e, " "), "desktop_web") {
		t.Errorf("listen identique à browser_server accepté : %v", ps)
	}
	// « 8770 » = 127.0.0.1:8770 : même adresse, formes différentes
	ps = val("defaults:\n  browser_server: { listen: '127.0.0.1:8770' }\n  desktop_web: { listen: '8770' }\n")
	if len(problemsOf(ps, SevError)) == 0 {
		t.Errorf("« 8770 » et 127.0.0.1:8770 sont la même adresse : refus attendu : %v", ps)
	}
	// identique au serveur des fichiers PAR DÉFAUT (browser_server non déclaré)
	ps = val("defaults:\n  desktop_web: { listen: '" + defaultBrowserListen + "' }\n")
	if len(problemsOf(ps, SevError)) == 0 {
		t.Errorf("listen égal au port par défaut du serveur des fichiers (%s) accepté : %v", defaultBrowserListen, ps)
	}
	// ports différents : rien
	ps = val("defaults:\n  browser_server: { listen: '127.0.0.1:8770' }\n  desktop_web: { listen: '127.0.0.1:8771' }\n")
	if len(problemsOf(ps, SevError)) != 0 {
		t.Errorf("ports distincts refusés : %v", ps)
	}
	// off : valide ; absent : valide
	for _, y := range []string{"defaults:\n  desktop_web: { listen: off }\n", "defaults:\n  browser_server: { listen: off }\n"} {
		if ps := val(y); len(problemsOf(ps, SevError)) != 0 || len(problemsOf(ps, SevWarning)) != 0 {
			t.Errorf("%q : problèmes inattendus %v", y, ps)
		}
	}
	// illisible
	if ps := val("defaults:\n  desktop_web: { listen: 'nimporte' }\n"); len(problemsOf(ps, SevError)) == 0 {
		t.Errorf("listen illisible accepté : %v", ps)
	}
	// hors boucle locale : avertissement, pas d'erreur ; boucle locale : aucun avertissement
	ps = val("defaults:\n  browser_server: { listen: off }\n  desktop_web: { listen: '0.0.0.0:8770' }\n")
	if len(problemsOf(ps, SevError)) != 0 || len(problemsOf(ps, SevWarning)) == 0 {
		t.Errorf("0.0.0.0:8770 : attendu un avertissement sans erreur, obtenu %v", ps)
	}
	ps = val("defaults:\n  browser_server: { listen: off }\n  desktop_web: { listen: '127.0.0.1:8770' }\n")
	if w := problemsOf(ps, SevWarning); len(w) != 0 {
		t.Errorf("boucle locale : avertissement inattendu %v", w)
	}
	// conflit propre à une machine (priorité par machine) : listen du poste = browser_server du poste
	y := "hubs:\n  " + strings.ToLower(HubHost()) + ":\n    browser_server: { listen: '127.0.0.1:8770' }\n    desktop_web: { listen: '127.0.0.1:8770' }\n"
	if ps := val(y); len(problemsOf(ps, SevError)) == 0 {
		t.Errorf("conflit au niveau de la machine non détecté : %v", ps)
	}
}
