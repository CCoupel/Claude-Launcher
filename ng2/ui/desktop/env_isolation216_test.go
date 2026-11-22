package desktop

// Garde d'isolation (#216, enquête GH_TOKEN) : la config claude de CE processus de test n'est jamais celle du poste, y
// compris APRÈS qu'un test a utilisé t.Setenv (sandbox) — la restauration de l'environnement rend la valeur du PROCESSUS,
// posée par TestMain. Avant le correctif de TestMain, elle redonnait le CLAUDE_CONFIG_DIR réel de la session : tout code
// encore actif (tâche de fond d'une App, ouverture de projet) écrivait alors dans le vrai settings.json.

import (
	"os"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

func TestConfigClaudeDuProcessusEstTemporaire216(t *testing.T) {
	t.Run("un test qui utilise sandbox", func(t *testing.T) { sandbox(t, "", "alpha") })
	t.Run("une redéfinition puis restauration", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	})
	// hors de tout t.Setenv : la valeur restaurée est celle du processus
	for name, dir := range map[string]string{"CLAUDE_CONFIG_DIR": os.Getenv("CLAUDE_CONFIG_DIR"), "core.ClaudeDir()": core.ClaudeDir()} {
		if dir == "" || !strings.HasPrefix(dir, os.TempDir()) {
			t.Errorf("%s = %q : hors du dossier temporaire — un test pourrait écrire dans la config claude du poste", name, dir)
		}
	}
	if home, _ := os.UserHomeDir(); !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("HOME = %q : hors du dossier temporaire", home)
	}
}
