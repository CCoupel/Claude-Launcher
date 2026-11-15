package desktop

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// #150 : trois tailles de caractères indépendantes — terminaux (`termFontSize`,
// #140), arbre (`treeFontSize`), cartes (`cardsFontSize`) — gardées par poste
// dans desktop-ui.json, bornes 8 à 28 portées par l'interface, absentes = défaut.
// Contrat : contracts/desktop-dispatch.md (v6.2.0 LOT-1).

func prefsOf(t *testing.T, a *App) map[string]any {
	t.Helper()
	res, err := a.Dispatch("init", nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.(map[string]any)["prefs"].(map[string]any)
}

func TestAreaFontSizesRoundTrip(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)

	p := prefsOf(t, a)
	for _, k := range []string{"treeFontSize", "cardsFontSize"} {
		if v, ok := p[k]; ok {
			t.Fatalf("%s présent sans réglage : %v (défaut = absent)", k, v)
		}
	}

	if _, err := a.Dispatch("setPrefs", json.RawMessage(`{"treeFontSize":15,"cardsFontSize":10}`)); err != nil {
		t.Fatal(err)
	}
	b := newApp(t, r.emit) // lancement suivant : reprise au démarrage
	p = prefsOf(t, b)
	if p["treeFontSize"] != 15.0 || p["cardsFontSize"] != 10.0 {
		t.Errorf("tailles relues : tree=%v cards=%v, attendu 15 et 10", p["treeFontSize"], p["cardsFontSize"])
	}
}

// Indépendance : chaque taille se règle sans toucher aux deux autres.
func TestAreaFontSizesIndependent(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	set := func(js string) {
		t.Helper()
		if _, err := a.Dispatch("setPrefs", json.RawMessage(js)); err != nil {
			t.Fatal(err)
		}
	}
	set(`{"termFontSize":17}`)
	set(`{"treeFontSize":14}`)
	set(`{"cardsFontSize":9}`)
	p := loadPrefs()
	if p["termFontSize"] != 17.0 || p["treeFontSize"] != 14.0 || p["cardsFontSize"] != 9.0 {
		t.Fatalf("trois tailles attendues 17/14/9 : %v", p)
	}
	set(`{"treeFontSize":null}`) // retour au défaut de la seule zone arbre
	p = loadPrefs()
	if _, ok := p["treeFontSize"]; ok || p["termFontSize"] != 17.0 || p["cardsFontSize"] != 9.0 {
		t.Errorf("après treeFontSize=null : %v", p)
	}
	set(`{"termFontSize":null,"cardsFontSize":null}`)
	if p = loadPrefs(); len(p) != 0 {
		t.Errorf("toutes les tailles remises au défaut : %v", p)
	}
}

// « Ce poste » (configModel kind hub) rend les trois tailles ; défauts arbre 13,
// cartes 12, terminaux 13 quand absentes.
func TestConfigModelHubAreaFontSizes(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	model := func() map[string]any {
		t.Helper()
		res, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res.(map[string]any)["model"]) // comme le fait la liaison
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := model()
	if m["termFontSize"] != 13.0 || m["treeFontSize"] != 13.0 || m["cardsFontSize"] != 12.0 {
		t.Errorf("défauts hub : term=%v tree=%v cards=%v, attendu 13/13/12", m["termFontSize"], m["treeFontSize"], m["cardsFontSize"])
	}
	_, _ = a.Dispatch("setPrefs", json.RawMessage(`{"treeFontSize":16,"cardsFontSize":11}`))
	m = model()
	if m["treeFontSize"] != 16.0 || m["cardsFontSize"] != 11.0 || m["termFontSize"] != 13.0 {
		t.Errorf("hub après réglage : %v", m)
	}
}

// Interface : bornes 8-28 partagées, une préférence par zone, Ctrl+molette sur
// le navigateur et sur les cartes en plus des terminaux, sans zoom de la page,
// réglage dans Réglages › Ce poste.
func TestAreaFontSizesFrontend(t *testing.T) {
	js, st := asset(t, "app.js"), asset(t, "settings.js")
	for _, k := range []string{"treeFontSize", "cardsFontSize", "termFontSize"} {
		if !strings.Contains(js, k) {
			t.Errorf("préférence %s absente de app.js", k)
		}
	}
	// settings.js : deux lignes de réglage (arbre, cartes) à côté de celle des terminaux ;
	// elles lisent le modèle « Ce poste » ou la préférence
	if !regexp.MustCompile(`(?i)tree`).MatchString(st) || !regexp.MustCompile(`(?i)cards`).MatchString(st) {
		t.Error("lignes de réglage arbre et cartes absentes de settings.js (Réglages › Ce poste)")
	}
	if !regexp.MustCompile(`MIN_FONT = 8, MAX_FONT = 28`).MatchString(js) {
		t.Error("bornes MIN_FONT = 8, MAX_FONT = 28 absentes de app.js")
	}
	wheels := regexp.MustCompile(`(?s)addEventListener\(\s*['"]wheel['"].{0,500}`).FindAllString(js, -1)
	withCtrl := 0
	for _, w := range wheels {
		if strings.Contains(w, "ctrlKey") && strings.Contains(w, "preventDefault") {
			withCtrl++
		}
	}
	// terminaux d'une part, navigateur et cartes d'autre part (un écouteur par zone ou un délégué)
	if withCtrl < 2 {
		t.Errorf("%d écouteur(s) wheel Ctrl+molette avec preventDefault, attendu au moins 2 (terminaux + navigateur/cartes)", withCtrl)
	}
	for _, msg := range []string{"Taille de l'arbre", "Taille des cartes"} {
		if !strings.Contains(js, msg) {
			t.Errorf("message bref « %s : N » absent de app.js", msg)
		}
	}
}
