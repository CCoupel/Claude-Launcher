package desktop

import (
	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/testguard"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain : tout tmux lancé par un test de la suite place son socket sous un
// dossier temporaire dédié, supprimé à la fin — jamais dans /tmp/tmux-<uid> (#154).
func TestMain(m *testing.M) {
	core.NoExecSelf = true                // jamais « <binaire de test> version » (#219)
	core.TestIsolation = true             // écritures de config/secrets hors dossier temporaire refusées (isolation.go)
	os.Setenv(core.TestIsolationVar, "1") // … aussi dans les sous-processus lancés par les tests
	os.Setenv("CL_IN_CLUSTER", "0")       // le conteneur de développement est lui-même un pod : jamais de refus du backend host (#169)
	// La config claude de la session n'est JAMAIS celle des tests, même hors t.Setenv : sans cette valeur de processus,
	// l'environnement restauré à la fin de chaque test (sandbox) redonnait le CLAUDE_CONFIG_DIR RÉEL à tout code encore
	// actif (tâche de fond d'une App, ouverture de projet) — c'est ainsi qu'un faux jeton a pu atteindre le vrai
	// settings.json (#216, enquête GH_TOKEN). core et ui/tmux le faisaient déjà.
	claudeDir, cerr := os.MkdirTemp("", "ng2-test-claude-")
	if cerr != nil {
		panic(cerr)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	defer os.RemoveAll(claudeDir)
	code := 1
	func() {
		d, err := os.MkdirTemp("", "cltmux") // court : limite des chemins de sockets unix
		if err == nil {
			os.Setenv("TMUX_TMPDIR", d)
			defer os.RemoveAll(d)
		}
		if d := testguard.FakeEngines(); d != "" { // faux moteurs en échec immédiat : local = CI (engines.go)
			defer os.RemoveAll(d)
		}
		guard := testguard.Start()
		code = m.Run()
		if !guard() && code == 0 {
			code = 1
		}
		if v := core.IsolationViolations(); len(v) > 0 && code == 0 { // écriture refusée par la garde, même si l'appelant a ignoré l'erreur
			fmt.Fprintln(os.Stderr, "ISOLATION DES TESTS VIOLÉE :\n  "+strings.Join(v, "\n  "))
			code = 1
		}
	}()
	os.Exit(code)
}
