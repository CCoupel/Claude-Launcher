package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// bacs : CL_CONFIG_DIR et CLAUDE_CONFIG_DIR temporaires — jamais le vrai config.conf ni le vrai settings.json.
func confBench225(t *testing.T) (conf, settings string) {
	t.Helper()
	unixOnly(t)
	root := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	_ = os.MkdirAll(Ng2ConfigDir(), 0o755)
	return ConfigPath(), filepath.Join(ClaudeDir(), "settings.json")
}

// editConf : réécrit config.conf avec une date distincte (la résolution du système de fichiers ne doit pas masquer l'édition).
func editConf(t *testing.T, path, content string, plus time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(plus)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// FreshConfig : inchangé → même copie ; modifié → relu (racines gardées) ; invalide → dernière copie valide + message, une fois.
func TestFreshConfig225(t *testing.T) {
	conf, _ := confBench225(t)
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\nUSAGE_REFRESH_SEC=100\n", 0)
	cur := LoadConfig()
	cur.Roots = []string{"/racine/figée"}
	if c, w := FreshConfig(cur); w != "" || c.UsageRefreshSec != 100 || !c.Stamp.Same(cur.Stamp) {
		t.Fatalf("inchangé : %+v %q", c.UsageRefreshSec, w)
	}
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=1\nUSAGE_REFRESH_SEC=200\n", 5*time.Second)
	c, w := FreshConfig(cur)
	if w != "" || c.UsageRefreshSec != 200 || !c.DisableMouse || len(c.Roots) != 1 || c.Roots[0] != "/racine/figée" {
		t.Fatalf("modifié : usage=%d mouse=%v roots=%v warn=%q", c.UsageRefreshSec, c.DisableMouse, c.Roots, w)
	}
	// invalide : ancienne copie gardée, dite une seule fois
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n", 10*time.Second)
	if ps := ValidateConfig(ConfConf, readFileBytes(t, conf)); !HasErrors(ps) {
		t.Skip("contenu jugé valide par la validation actuelle")
	}
	bad, w := FreshConfig(c)
	if bad.UsageRefreshSec != 200 || !bad.DisableMouse {
		t.Errorf("config invalide : copie valide perdue (%d, souris %v)", bad.UsageRefreshSec, bad.DisableMouse)
	}
	if w == "" {
		t.Fatal("aucun message pour un config.conf invalide")
	}
	if _, w2 := FreshConfig(bad); w2 != "" {
		t.Errorf("message répété : %q", w2)
	}
}

func readFileBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
