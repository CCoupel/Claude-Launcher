//go:build windows

package platform

// Enveloppes Windows (#110), exécutées sur le runner windows-latest de la CI.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnNetworkFSWindows(t *testing.T) {
	if !OnNetworkFS(`\\wsl.localhost\Ubuntu\share\MFS`) || !OnNetworkFS(`\\nas\partage`) {
		t.Error("UNC : relecture périodique attendue")
	}
	if OnNetworkFS(os.TempDir()) {
		t.Errorf("%s (disque local) vu comme réseau", os.TempDir())
	}
	if getDriveType(`C:\`) != DriveFixed {
		t.Errorf("C:\\ : type %d", getDriveType(`C:\`))
	}
}

func TestLeaderWindows(t *testing.T) {
	if LeaderAliveInHosts(0, "", nil) != LeaderDead || LeaderAliveInHosts(1234, "pro-demo#pid:[1]", nil) != LeaderUnknown {
		t.Error("verdicts")
	}
	if !strings.HasSuffix(HostPidSpace(), "#") {
		t.Error(HostPidSpace())
	}
}

// exe ouvert (comme un exe en cours d'exécution) : remplacé via .old
func TestReplaceExecutableWindows(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "claude-launcher-ng2.exe")
	_ = os.WriteFile(exe, []byte("v1"), 0o755)
	f, err := os.Open(exe) // garde un descripteur, comme le processus en cours
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = ReplaceExecutable(exe, func(dst string) error { return os.WriteFile(dst, []byte("v2"), 0o755) })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "v2" {
		t.Errorf("nouveau : %q", b)
	}
	if b, _ := os.ReadFile(OldExe(exe)); string(b) != "v1" {
		t.Errorf("ancien : %q", b)
	}
}

func TestConPTY(t *testing.T) {
	term, err := StartTerminal(TermSpec{Argv: []string{"cmd.exe", "/c", "echo bonjour-conpty"}, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if err := term.Resize(120, 40); err != nil {
		t.Errorf("Resize : %v", err)
	}
	out := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for !strings.Contains(b.String(), "bonjour-conpty") {
			n, err := term.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		out <- b.String()
	}()
	select {
	case s := <-out:
		if !strings.Contains(s, "bonjour-conpty") {
			t.Errorf("sortie : %q", s)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("pas de sortie de la pseudo-console")
	}
	if err := term.Wait(); err != nil {
		t.Errorf("Wait : %v", err)
	}
}

func TestTermSizeNoConsole(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "x")
	defer f.Close()
	if _, _, err := TermSize(int(f.Fd())); err == nil {
		t.Error("fichier ordinaire : pas une console")
	}
}
