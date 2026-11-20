package desktop

// #209 : simulation node de l'œil à 3 états (contrat en tête de testdata/hide_eye.js), comme tabs_bar.js. Écrit avant l'implémentation.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHideEyeSimulation209(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "hide_eye.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation de l'œil : %v\n%s", err, out)
	}
}

// v2 (proposition A) : CSS — œil aligné à droite du titre, choix du projet en fond plein accent (« bouton enfoncé »), plus de puce.
func TestHideEyeStyleV2209(t *testing.T) {
	css := asset(t, "style.css")
	if !regexp.MustCompile(`\.eye[^{]*\{[^}]*margin-left:\s*auto`).MatchString(css) {
		t.Error("l'œil doit être le dernier élément du titre, aligné à droite (margin-left: auto sur .eye)")
	}
	if !regexp.MustCompile(`\.eye\.own[^{]*\{[^}]*background`).MatchString(css) {
		t.Error("règle .eye.own (fond plein accent) absente")
	}
	if !regexp.MustCompile(`\.eye\.own:hover`).MatchString(css) {
		t.Error("règle .eye.own:hover absente")
	}
	if strings.Contains(asset(t, "app.js"), "'hidechip'") && !regexp.MustCompile(`\.hidechip`).MatchString(css) {
		t.Error("hidechip utilisée sans règle CSS")
	}
	if regexp.MustCompile(`\.hidechip`).MatchString(css) && !strings.Contains(asset(t, "app.js"), "'hidechip'") {
		t.Error(".hidechip n'est plus utilisée : règle CSS à supprimer")
	}
	// thèmes clair et sombre : la marque reste lisible (fond et couleur d'icône posés pour .eye.own dans chaque thème utilisé)
	if !regexp.MustCompile(`\.eye\.own[^{]*\{[^}]*color`).MatchString(css) {
		t.Error(".eye.own : couleur d'icône (sombre sur fond accent) absente")
	}
}
