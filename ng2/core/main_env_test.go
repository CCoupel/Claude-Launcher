package core

import (
	"claude-launcher-ng2/internal/testguard"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain : la config claude de la session (CLAUDE_CONFIG_DIR, souvent sur le
// stockage partagé) n'est jamais celle des tests — un test qui ne la redéfinit
// pas écrirait dans le vrai cache de consommation commun (usage.go).
func TestMain(m *testing.M) {
	NoExecSelf = true                // jamais « <binaire de test> version » (#219)
	TestIsolation = true             // écritures de config/secrets hors dossier temporaire refusées (isolation.go)
	os.Setenv(TestIsolationVar, "1") // … aussi dans les sous-processus lancés par les tests
	d, err := os.MkdirTemp("", "ng2-test-claude-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", d)
	os.Setenv("CL_IN_CLUSTER", "0") // le conteneur de développement est lui-même un pod : jamais de refus du backend host (#169)
	// pas d'interrogation des vrais moteurs locaux depuis leaderAliveIn
	LocalCtrHosts.Set(func() map[string]bool { return nil })
	// jamais le VRAI podman / docker (#270) : « podman version » crée son stockage overlay (répertoires en mode 000, que le RemoveAll de
	// t.TempDir ne peut pas supprimer) sous le HOME temporaire d'un test — instable selon le runner. Faux moteurs en échec immédiat
	// en tête du PATH (comme ui/desktop et ui/tmux) ET stockage des conteneurs hors de tout dossier temporaire de test.
	if d := testguard.FakeEngines(); d != "" {
		defer os.RemoveAll(d)
	}
	if os.Getenv("XDG_DATA_HOME") == "" || strings.HasPrefix(os.Getenv("XDG_DATA_HOME"), os.TempDir()+"/Test") {
		os.Setenv("XDG_DATA_HOME", filepath.Join(os.TempDir(), "cl-core-test-data"))
	}
	guard := testguard.Start()
	code := m.Run()
	if !guard() && code == 0 {
		code = 1
	}
	if v := IsolationViolations(); len(v) > 0 && code == 0 { // écriture refusée par la garde, même si l'appelant a ignoré l'erreur
		fmt.Fprintln(os.Stderr, "ISOLATION DES TESTS VIOLÉE :\n  "+strings.Join(v, "\n  "))
		code = 1
	}
	os.RemoveAll(d)
	os.Exit(code)
}
