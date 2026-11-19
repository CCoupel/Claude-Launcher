package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #198 : après un redémarrage du serveur, les vues ouvertes sont gardées et resynchronisées quand leur onglet revient ;
// celles qui ne reviennent pas sont signalées ; un bandeau « Recharger » n'est jamais effacé (simulation node).
func TestRedemarrageServeurVuesConservees(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation du redémarrage ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "server_restart.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation du redémarrage : %v\n%s", err, out)
	}
}

// #171, #198 : init rejoué avant la réouverture du flux, bandeau « connexion perdue », backoff (simulation node).
func TestFluxSSEReconnexionInitRejoue(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation du flux ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "sse_flow.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation du flux : %v\n%s", err, out)
	}
}

// #191 : ordre des onglets (orderTabs, dropIndex, reorderPaths), simulation node ; cl.tabOrder dans le stockage local.
func TestOrdreOngletsSimulation(t *testing.T) {
	js := asset(t, "app.js")
	for _, want := range []string{"displayedTabs()", "startTabDrag("} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js : %q absent", want)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation de l'ordre des onglets ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "tab_order.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("ordre des onglets : %v\n%s", err, out)
	}
}
