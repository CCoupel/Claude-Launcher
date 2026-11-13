package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Un pane de la coque dont le processus meurt (ici SIGBUS simulé : code 135)
// est relancé, et la cause est consignée puis lue une fois au redémarrage.
func TestSupervisedRestartsAndRecords(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	count := filepath.Join(t.TempDir(), "runs")
	cmd := supervised(roleNav, "sh -c "+core.Shq("echo run >>"+core.Shq(count)+"; echo 'unexpected fault address' >&2; exit 135"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "sh", "-c", cmd).Run()
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "run"); n < 2 {
		t.Fatalf("relancé %d fois, attendu ≥ 2", n)
	}
	e, _ := os.ReadFile(navErrFile())
	if !strings.Contains(string(e), "arrêt inattendu (135) (SIGBUS") || !strings.Contains(string(e), "unexpected fault address") {
		t.Fatalf("nav.err = %q", e)
	}
	why := takeNavCrash()
	if !strings.Contains(why, "SIGBUS") {
		t.Fatalf("takeNavCrash = %q", why)
	}
	if takeNavCrash() != "" {
		t.Fatal("cause relue deux fois")
	}
	if _, err := os.Stat(navErrFile() + ".last"); err != nil {
		t.Fatal("détail non conservé en .last")
	}
}

// Navigateur fermé : ensureLayout le recrée en haut à gauche (au-dessus des
// infos, ou à gauche de la zone tmux s'il ne reste qu'elle). Serveur isolé.
func TestEnsureLayoutRecreatesNav(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CL_OUTER_SOCKET", fmt.Sprintf("cl-test-heal-%d", os.Getpid()))
	t.Cleanup(func() { _ = otmux("kill-server").Run() })
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	for _, keepInfo := range []bool{true, false} {
		_ = otmux("kill-server").Run()
		if out, err := otmux("-f", "/dev/null", "new-session", "-d", "-s", outerSession, "-x", "120", "-y", "40", "sleep 600").CombinedOutput(); err != nil {
			t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
		}
		nav := strings.TrimSpace(run(t, otmux("display-message", "-p", "-t", outerSession+":0.0", "#{pane_id}")))
		hub := strings.TrimSpace(run(t, otmux("split-window", "-h", "-d", "-P", "-F", "#{pane_id}", "-t", nav, "sleep 600")))
		info := strings.TrimSpace(run(t, otmux("split-window", "-v", "-d", "-P", "-F", "#{pane_id}", "-t", nav, "sleep 600")))
		tagPane(nav, roleNav)
		tagPane(hub, roleHub)
		tagPane(info, roleInfo)
		run(t, otmux("kill-pane", "-t", nav))
		if !keepInfo {
			run(t, otmux("kill-pane", "-t", info))
		}
		if err := EnsureLayout("/bin/true"); err != nil {
			t.Fatalf("keepInfo=%v : %v", keepInfo, err)
		}
		roles, _ := outerPanes()
		if roles[roleNav] == "" || roles[roleInfo] == "" || roles[roleHub] != hub {
			t.Fatalf("keepInfo=%v : rôles %v", keepInfo, roles)
		}
		pos := strings.TrimSpace(run(t, otmux("display-message", "-p", "-t", roles[roleNav], "#{pane_left},#{pane_top}")))
		if pos != "0,0" {
			t.Fatalf("keepInfo=%v : navigateur recréé en %s, attendu en haut à gauche", keepInfo, pos)
		}
	}
}
