package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Encadrés Identifiants ssh / Kubeconfig (#193, #194) : une fiche reconstruite par stPoll pendant la lecture du coffre
// ne doit pas rester sur « lecture du coffre… » ; ils se placent sous les champs de connexion ; défilement visible.
func TestSecretBoxesRefresh(t *testing.T) {
	st := asset(t, "settings.js")
	if i, j := strings.Index(st, "stSecretWait(b, t)"), strings.Index(st, "// test par étapes"); i < 0 || j < 0 || i > j {
		t.Error("les encadrés doivent précéder la zone « Tester » (sous les champs de connexion)")
	}
	if !strings.Contains(asset(t, "style.css"), "overflow-y: scroll") {
		t.Error("barre de défilement de la fiche non forcée (.stpg)")
	}
	for _, k := range []string{"finally { priv.value = ''", "finally { ta.value = ''; file.value = ''"} {
		if !strings.Contains(st, k) {
			t.Errorf("champ secret non vidé dans un finally : %q absent", k)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation du rafraîchissement ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "st_refresh.js"), filepath.Join(wd, "frontend", "settings.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation stPoll : %v\n%s", err, out)
	}
}
