package tmux

import (
	"os"
	"path/filepath"
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
	d, err := os.MkdirTemp("", "ng2-test-claude-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", d)
	// pas d'interrogation des vrais moteurs locaux depuis leaderAliveIn
	core.LocalCtrHosts = func() map[string]bool { return nil }
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}

func withHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
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
