package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Coque dont le navigateur a disparu : détectée (et donc recréée à la relance au
// lieu d'échouer sur « coque sans navigateur ») ; coque saine ou d'avant les
// rôles : laissée telle quelle. Serveur tmux isolé, jamais celui du launcher.
func TestShellWithoutNav(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	sock := fmt.Sprintf("cl-test-outer-%d", os.Getpid())
	t.Setenv("CL_OUTER_SOCKET", sock)
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil { // court : limite des sockets unix
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	// Enregistré en dernier, donc exécuté en premier (#144) : avant que
	// TMUX_TMPDIR soit restauré et son dossier effacé, sinon kill-server ne
	// trouve plus la socket et le serveur survit.
	t.Cleanup(func() {
		stopOuterShell()
		if left := leftovers(sock, 5*time.Second); left != "" {
			t.Errorf("processus restants après le test (%s) :\n%s", sock, left)
		}
	})

	newShell := func(tag bool) (nav string) {
		_ = otmux("kill-server").Run()
		if out, err := otmux("-f", "/dev/null", "new-session", "-d", "-s", outerSession, "-x", "120", "-y", "40", "sleep 600").CombinedOutput(); err != nil {
			// runner sans tmux utilisable (CI) : rien à vérifier ici
			t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
		}
		nav = strings.TrimSpace(run(t, otmux("display-message", "-p", "-t", outerSession+":0.0", "#{pane_id}")))
		hub := strings.TrimSpace(run(t, otmux("split-window", "-h", "-d", "-P", "-F", "#{pane_id}", "-t", nav, "sleep 600")))
		info := strings.TrimSpace(run(t, otmux("split-window", "-v", "-d", "-P", "-F", "#{pane_id}", "-t", nav, "sleep 600")))
		if tag {
			tagPane(nav, roleNav)
			tagPane(hub, roleHub)
			tagPane(info, roleInfo)
		}
		return nav
	}

	nav := newShell(true)
	if shellWithoutNav() {
		t.Fatal("coque saine vue sans navigateur")
	}
	run(t, otmux("kill-pane", "-t", nav))
	if !shellWithoutNav() {
		t.Fatal("navigateur tué : non détecté")
	}

	newShell(false) // coque d'avant les rôles : ensureLayout étiquette le pane 0
	if shellWithoutNav() {
		t.Fatal("coque d'avant les rôles vue sans navigateur")
	}

	_ = otmux("kill-server").Run() // plus de coque du tout
	if !shellWithoutNav() {
		t.Fatal("coque introuvable : doit être recréée")
	}
}

func TestNavErrorRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if lastNavError() != "" {
		t.Fatal("erreur inventée")
	}
	RecordNavError(errors.New("program was killed: boom\n"))
	if s := lastNavError(); !strings.Contains(s, "program was killed: boom") || strings.Contains(s, "\n") {
		t.Fatalf("lastNavError = %q", s)
	}
}

func run(t *testing.T, c *exec.Cmd) string {
	t.Helper()
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v : %v (%s)", c.Args, err, out)
	}
	return string(out)
}
