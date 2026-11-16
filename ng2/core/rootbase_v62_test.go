package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// LOT-4.1 (revue c3, M5) — non-régression : un config.conf de la v6.2 (sans ${CL_ROOT_BASE}) au chemin standard
// <racine>/.config/claude-launcher-ng2/config.conf, lu SANS la variable, donne les chemins du fichier, sans
// avertissement, sans racine de base déduite et sans variable exportée.
func TestV62ConfigAtStandardPathIsReadUnchanged(t *testing.T) {
	unixOnly(t)
	withHome(t)
	t.Setenv(RootBaseVar, "")
	root := t.TempDir()
	dir := filepath.Join(root, ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(dir, 0o755)
	t.Setenv("CL_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	conf := "# config v6.2\nGITHUB_DIR=\"/data/p\"\nGITHUB_DIRS=()\nCLAUDE_CONFIG_DIR=\"../../.claude\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	if !reflect.DeepEqual(cfg.Roots, []string{"/data/p"}) {
		t.Errorf("racines de projets : %v, attendu [/data/p] (chemin du fichier inchangé)", cfg.Roots)
	}
	if want := filepath.Join(root, ".claude"); cfg.ClaudeConfigDir != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, attendu %q (relatif au dossier du fichier, comme en v6.2)", cfg.ClaudeConfigDir, want)
	}
	if cfg.RootBaseWarn != "" {
		t.Errorf("avertissement inattendu sur un fichier v6.2 : %q", cfg.RootBaseWarn)
	}
	if d, src := RootBase(); d != "" || src != "" {
		t.Errorf("RootBase() = %q (%s), attendu vide : aucune racine déduite d'un fichier v6.2", d, src)
	}
	if v, ok := os.LookupEnv(RootBaseVar); ok && v != "" {
		t.Errorf("%s exportée par la lecture : %q", RootBaseVar, v)
	}
}

// Même résultat quand le fichier v6.2 ne cite la variable que dans un COMMENTAIRE et reste au chemin standard :
// les chemins du fichier sont ceux lus, jamais ceux d'une racine déduite.
func TestV62ConfigMentioningVarInCommentKeepsItsPaths(t *testing.T) {
	unixOnly(t)
	withHome(t)
	t.Setenv(RootBaseVar, "")
	root := t.TempDir()
	dir := filepath.Join(root, ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(dir, 0o755)
	t.Setenv("CL_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	conf := "# autrefois : GITHUB_DIR=\"${CL_ROOT_BASE}/PROJETS\" (retiré)\nGITHUB_DIR=\"/data/p2\"\nGITHUB_DIRS=()\n"
	_ = os.WriteFile(filepath.Join(dir, "config.conf"), []byte(conf), 0o600)
	cfg := LoadConfig()
	if !reflect.DeepEqual(cfg.Roots, []string{"/data/p2"}) {
		t.Errorf("racines : %v, attendu [/data/p2]", cfg.Roots)
	}
	for _, r := range cfg.Roots {
		if strings.Contains(r, root) || strings.Contains(r, "PROJETS") {
			t.Errorf("chemin déduit de la racine au lieu de celui du fichier : %q", r)
		}
	}
	if d, _ := RootBase(); d != "" {
		t.Errorf("RootBase() = %q, attendu vide", d)
	}
	if v := os.Getenv(RootBaseVar); v != "" {
		t.Errorf("%s exportée : %q", RootBaseVar, v)
	}
}
