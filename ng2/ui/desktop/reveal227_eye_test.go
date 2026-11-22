package desktop

// #227 : simulation node de l'œil commun aux éléments cachés (contrat en tête de testdata/secret_eye.js) : masqué par défaut,
// re-masquage au clic / à 30 s / par eyesHide (rubrique, fermeture), valeur effacée, refus sans valeur, réponse tardive jetée.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSecretEyeSimulation227(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent : simulation ignorée")
	}
	wd, _ := os.Getwd()
	out, err := exec.Command(node, filepath.Join(wd, "testdata", "secret_eye.js"), filepath.Join(wd, "frontend", "app.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("simulation de l'œil : %v\n%s", err, out)
	}
}
