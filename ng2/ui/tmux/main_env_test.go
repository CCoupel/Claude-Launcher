package tmux

import (
	"claude-launcher-ng2/internal/testguard"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Copie de l'environnement de test de core (core/main_env_test.go) et de ses
// assistants utilisés ici (core/backend_test.go, core/resume_test.go) : les
// fichiers _test d'un package ne sont pas visibles des autres.

// TestMain : la config claude de la session (CLAUDE_CONFIG_DIR, souvent sur le
// stockage partagé) n'est jamais celle des tests — un test qui ne la redéfinit
// pas écrirait dans le vrai cache de consommation commun (usage.go).
func TestMain(m *testing.M) {
	core.NoExecSelf = true                // jamais « <binaire de test> version » (#219)
	core.TestIsolation = true             // écritures de config/secrets hors dossier temporaire refusées (isolation.go)
	os.Setenv(core.TestIsolationVar, "1") // … aussi dans les sous-processus lancés par les tests
	d, err := os.MkdirTemp("", "ng2-test-claude-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", d)
	os.Setenv("CL_IN_CLUSTER", "0") // le conteneur de développement est lui-même un pod : jamais de refus du backend host (#169)
	// pas d'interrogation des vrais moteurs locaux depuis leaderAliveIn
	core.LocalCtrHosts.Set(func() map[string]bool { return nil })
	engines := testguard.FakeEngines() // faux moteurs en échec immédiat : local = CI (engines.go)
	guard := testguard.Start()
	code := m.Run()
	if !guard() && code == 0 {
		code = 1
	}
	if v := core.IsolationViolations(); len(v) > 0 && code == 0 { // écriture refusée par la garde, même si l'appelant a ignoré l'erreur
		fmt.Fprintln(os.Stderr, "ISOLATION DES TESTS VIOLÉE :\n  "+strings.Join(v, "\n  "))
		code = 1
	}
	os.RemoveAll(d)
	if engines != "" {
		os.RemoveAll(engines)
	}
	os.Exit(code)
}

func withHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CL_CONFIG_DIR", "") // indépendant de l'environnement de l'appelant (config sous HOME attendue)
	t.Setenv(core.RootBaseVar, "")
	return h
}

// fakeClaude : conversations (id → âge) d'un projet sous un faux ~/.claude.
func fakeClaude(t *testing.T, path string, convs map[string]time.Duration) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", core.ProjectDirName(path))
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	for id, age := range convs {
		f := filepath.Join(dir, id+".jsonl")
		content := "{}\n"
		if age < 0 { // conversation vide : ignorée
			content, age = "", -age
		}
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		_ = os.Chtimes(f, mt, mt)
	}
	return root
}
