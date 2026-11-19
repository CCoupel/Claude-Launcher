package desktop

// #209 : simulation node de l'œil à 3 états (contrat en tête de testdata/hide_eye.js), comme tabs_bar.js. Écrit avant l'implémentation.

import (
	"os"
	"os/exec"
	"path/filepath"
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
