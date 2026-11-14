package desktop

import (
	"encoding/json"
	"regexp"
	"testing"
)

// #140 : la taille des caractères des terminaux (Ctrl+molette) est la
// préférence `termFontSize`, gardée dans desktop-ui.json et relue au lancement
// suivant ; absente = défaut de l'interface (13) ; mise à null = retour au défaut.
func TestTermFontSizeRoundTrip(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)

	res, _ := a.Dispatch("init", nil)
	if v, ok := res.(map[string]any)["prefs"].(map[string]any)["termFontSize"]; ok {
		t.Fatalf("termFontSize présent sans réglage : %v (défaut = absent)", v)
	}

	if _, err := a.Dispatch("setPrefs", json.RawMessage(`{"termFontSize":17}`)); err != nil {
		t.Fatal(err)
	}
	b := newApp(t, r.emit) // lancement suivant
	res, _ = b.Dispatch("init", nil)
	if v := res.(map[string]any)["prefs"].(map[string]any)["termFontSize"]; v != 17.0 {
		t.Errorf("termFontSize relu : %v, attendu 17", v)
	}

	// les autres préférences ne sont pas touchées, et inversement
	if _, err := b.Dispatch("setPrefs", json.RawMessage(`{"navW":420}`)); err != nil {
		t.Fatal(err)
	}
	p := loadPrefs()
	if p["termFontSize"] != 17.0 || p["navW"] != 420.0 {
		t.Errorf("fusion clé par clé : %v", p)
	}

	// null : retour au défaut
	if _, err := b.Dispatch("setPrefs", json.RawMessage(`{"termFontSize":null}`)); err != nil {
		t.Fatal(err)
	}
	p = loadPrefs()
	if _, ok := p["termFontSize"]; ok || p["navW"] != 420.0 {
		t.Errorf("après null : %v", p)
	}
}

// #140 : défaut 13 et bornes 8 à 28 portés par l'interface (le Go garde toute
// valeur) ; le test vérifie les constantes et le branchement Ctrl+molette.
func TestTermFontSizeBoundsInFrontend(t *testing.T) {
	js := asset(t, "app.js")
	if !regexp.MustCompile(`termFontSize`).MatchString(js) {
		t.Fatal("préférence termFontSize absente de app.js")
	}
	if !regexp.MustCompile(`\b13\b`).MatchString(js) {
		t.Error("défaut 13 absent de app.js")
	}
	if !regexp.MustCompile(`(?s)Math\.max\(\s*8\b.*?\b28\b|Math\.min\(\s*28\b.*?\b8\b|\b8\b\s*,\s*28\b|\bMIN_FONT\b.*?=\s*8|\bMAX_FONT\b.*?=\s*28`).MatchString(js) {
		t.Error("bornes 8 et 28 absentes de app.js")
	}
	if !regexp.MustCompile(`addEventListener\(\s*['"]wheel['"]`).MatchString(js) ||
		!regexp.MustCompile(`ctrlKey`).MatchString(js) || !regexp.MustCompile(`preventDefault`).MatchString(js) {
		t.Error("écouteur wheel avec Ctrl et preventDefault absent de app.js")
	}
}
