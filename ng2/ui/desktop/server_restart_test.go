package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
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
