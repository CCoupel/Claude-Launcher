package core

import (
	"claude-launcher-ng2/internal/testguard"
	"os"
	"testing"
)

// TestMain : la config claude de la session (CLAUDE_CONFIG_DIR, souvent sur le
// stockage partagé) n'est jamais celle des tests — un test qui ne la redéfinit
// pas écrirait dans le vrai cache de consommation commun (usage.go).
func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "ng2-test-claude-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", d)
	os.Setenv("CL_IN_CLUSTER", "0") // le conteneur de développement est lui-même un pod : jamais de refus du backend host (#169)
	// pas d'interrogation des vrais moteurs locaux depuis leaderAliveIn
	LocalCtrHosts.Set(func() map[string]bool { return nil })
	guard := testguard.Start()
	code := m.Run()
	if !guard() && code == 0 {
		code = 1
	}
	os.RemoveAll(d)
	os.Exit(code)
}
