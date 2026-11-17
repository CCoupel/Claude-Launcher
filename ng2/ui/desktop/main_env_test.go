package desktop

import (
	"os"
	"testing"
)

// TestMain : tout tmux lancé par un test de la suite place son socket sous un
// dossier temporaire dédié, supprimé à la fin — jamais dans /tmp/tmux-<uid> (#154).
func TestMain(m *testing.M) {
	os.Setenv("CL_IN_CLUSTER", "0") // le conteneur de développement est lui-même un pod : jamais de refus du backend host (#169)
	code := 1
	func() {
		d, err := os.MkdirTemp("", "cltmux") // court : limite des chemins de sockets unix
		if err == nil {
			os.Setenv("TMUX_TMPDIR", d)
			defer os.RemoveAll(d)
		}
		code = m.Run()
	}()
	os.Exit(code)
}
