package desktop

// Tests de spécification #189 / #190 / #191 (v6.4.0) — onglets de projets : couleur hors sélection, titre stable,
// ordre par glisser-déposer conservé dans cl.tabOrder. Maquette retenue B (tab_bar__couleur_titre_ordre.html).
// Simulation node de fonctions pures d'app.js (contrat en tête de testdata/tabs_bar.js), comme sse_flow.js.
// Écrits avant l'implémentation (TDD).

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTabsBarSimulation189(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "tabs_bar.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation des onglets : %v\n%s", err, out)
	}
}

// Largeur stable (#190) et liseré à la couleur du projet (#189) : emplacement d'état de largeur fixe, liseré bas de 3 px.
func TestTabsBarStyle189(t *testing.T) {
	css := asset(t, "style.css")
	if !regexp.MustCompile(`\.tab \.st\s*\{[^}]*\bwidth:`).MatchString(css) {
		t.Error("l'emplacement d'état (.tab .st) doit avoir une largeur fixe (réservé même au repos)")
	}
	if !regexp.MustCompile(`\.tab[^{]*\{[^}]*3px`).MatchString(css) {
		t.Error("liseré de 3 px à la couleur du projet absent des règles .tab")
	}
	app := asset(t, "app.js")
	if strings.Contains(app, "' · agent au travail'") && !strings.Contains(app, "tabView(") {
		t.Error("« agent au travail » ne doit plus s'ajouter au titre : infobulle seulement")
	}
	if !strings.Contains(app, "cl.tabOrder") {
		t.Error("clé de stockage local cl.tabOrder absente")
	}
}
