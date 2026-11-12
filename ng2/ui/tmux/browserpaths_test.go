package tmux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// Projets sur l'hôte : la variable est posée dans l'environnement global du
// tmux du hub (serveur isolé), donc héritée par toute fenêtre créée ensuite.
func TestExportBrowserPathsHub(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CL_INNER_SOCKET", fmt.Sprintf("cl-test-bp-%d", os.Getpid()))
	t.Setenv("TMUX", "")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Cleanup(func() { _ = dtmux("kill-server").Run() })
	if err := os.MkdirAll(core.Ng2ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.BackendsPath(), []byte("defaults:\n  browser_paths: [{ container: /share/MFS/ROOT, host: '\\\\nas\\MFS\\ROOT' }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := dtmux("-f", "/dev/null", "new-session", "-d", "-s", "hub", "sleep 600").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	exportBrowserPathsHub()
	out, err := dtmux("new-window", "-P", "-F", "#{pane_id}", "-t", "hub:", "sh -c 'printf %s \"$CL_BROWSER_PATH_MAP\" > "+cfg+"/seen; sleep 5'").Output()
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	var seen []byte
	for i := 0; i < 50 && len(seen) == 0; i++ {
		seen, _ = os.ReadFile(cfg + "/seen")
		if len(seen) == 0 {
			exec.Command("sleep", "0.1").Run()
		}
	}
	var back []core.BrowserPath
	if err := json.Unmarshal(seen, &back); err != nil || len(back) != 1 || back[0].Host != `\\nas\MFS\ROOT` {
		t.Fatalf("vu dans le pane : %q", seen)
	}
}
