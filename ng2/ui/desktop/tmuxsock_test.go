package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #154 : la suite ui/desktop ne laisse aucun socket `cl-*` dans
// /tmp/tmux-<uid>. Un TestMain pose TMUX_TMPDIR sur un dossier temporaire
// supprimé à la fin ; tout tmux lancé par un test (-L) y place donc son socket.

func tmuxSockGuard(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
}

// TMUX_TMPDIR est posé pour toute la suite, hors /tmp/tmux-<uid> par défaut.
func TestSuiteTmuxTmpdirIsolated(t *testing.T) {
	tmuxSockGuard(t)
	d := os.Getenv("TMUX_TMPDIR")
	if d == "" {
		t.Fatal("TMUX_TMPDIR non posé : les sockets des tests iront dans /tmp/tmux-<uid> (#154)")
	}
	if filepath.Clean(d) == "/tmp" || filepath.Clean(d) == filepath.Clean(os.TempDir()) {
		t.Errorf("TMUX_TMPDIR = %s : doit être un sous-dossier dédié, supprimé à la fin de la suite", d)
	}
}

// Un serveur tmux démarré comme le font les tests (-L sans TMUX_TMPDIR propre)
// place son socket sous TMUX_TMPDIR, jamais dans /tmp/tmux-<uid>.
func TestTmuxSocketOfSuiteNotInDefaultDir(t *testing.T) {
	tmuxSockGuard(t)
	sock := fmt.Sprintf("cl-test-sockguard-%d", os.Getpid())
	t.Setenv("TMUX", "")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	if out, err := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", "g", "sleep 60").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("tmux", "-L", sock, "display-message", "-p", "#{socket_path}").Output()
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSpace(string(out))
	def := fmt.Sprintf("/tmp/tmux-%d/", os.Getuid())
	if strings.HasPrefix(path, def) {
		t.Errorf("socket %s dans le dossier par défaut %s (#154) : TMUX_TMPDIR doit être posé par un TestMain", path, def)
	}
	if d := os.Getenv("TMUX_TMPDIR"); d != "" && !strings.HasPrefix(path, d) {
		t.Errorf("socket %s hors de TMUX_TMPDIR=%s", path, d)
	}
}
