package tmux

import (
	"os"
	"testing"

	"claude-launcher-ng2/core"
)

// noContainerBackend : « aucun backend conteneur » POSÉ (backends.yaml vide sous la config du test), pas supposé — un
// podman / docker du poste (runner de CI) deviendrait sinon un backend et changerait le flux (F2, ouverture). Précondition
// vérifiée : le banc est isolé ou le test s'arrête. Suppose withHome(t) ou CL_CONFIG_DIR déjà posés.
func noContainerBackend(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(core.BackendsPath(), []byte("backends: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(core.CtrBackends()) != 0 {
		t.Fatal("un backend conteneur est encore détecté : banc non isolé")
	}
}
