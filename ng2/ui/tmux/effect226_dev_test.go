package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Coque : config.conf relu au tour suivant → la ligne d'état dit les options modifiées et ce qu'il reste à faire (texte seul,
// procédure de la coque), sans relancer ; aucune valeur secrète dans le message.
func TestNavDitLesOptionsRelues226(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	_ = os.MkdirAll(core.Ng2ConfigDir(), 0o755)
	write := func(s string, plus time.Duration) {
		_ = os.WriteFile(core.ConfigPath(), []byte(s), 0o600)
		at := time.Now().Add(plus)
		_ = os.Chtimes(core.ConfigPath(), at, at)
	}
	write("CLAUDE_DISABLE_MOUSE=1\nTEAM_HIDE_IDLE=30\n", 0)
	m := model{cfg: core.LoadConfig(), confSrc: core.ReadConfSrc(), windows: map[string]bool{}}
	write("CLAUDE_DISABLE_MOUSE=0\nTEAM_HIDE_IDLE=45\nGITHUB_TOKEN=ghp_FAKE_TEST_coque226\n", 5*time.Second)
	m = m.refreshCfg()
	for _, want := range []string{"config.conf relu", "CLAUDE_DISABLE_MOUSE", "TEAM_HIDE_IDLE", "GITHUB_TOKEN", "F5 sur le projet"} {
		if !strings.Contains(m.msg, want) {
			t.Errorf("ligne d'état sans %q : %q", want, m.msg)
		}
	}
	if strings.Contains(m.msg, "FAKE_TEST") {
		t.Errorf("valeur secrète dans la ligne d'état : %q", m.msg)
	}
	// rien de changé au tour suivant : le message n'est pas refait
	m.msg = ""
	m = m.refreshCfg()
	if m.msg != "" {
		t.Errorf("message refait sans changement : %q", m.msg)
	}
}
