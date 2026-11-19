package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// `reattach {path}` (#198) : rattache l'onglet à une session tmux vivante, sans jamais rien créer.
func TestReattachNeCreeRien(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-reat-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho jamais lancé >> "+filepath.Join(t.TempDir(), "x")+"\nsleep 60\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	var r recorder
	a := newApp(t, r.emit)

	res := dispatchJSON(t, a, "reattach", map[string]any{"path": ps[0]})
	if res["ok"] != false {
		t.Fatalf("sans session : %v", res)
	}
	if a.term(ps[0]) != nil {
		t.Fatal("reattach a créé un onglet sans session")
	}
	if out, _ := exec.Command("tmux", "-L", sock, "list-sessions", "-F", "#{session_name}").CombinedOutput(); strings.Contains(string(out), hostSession(ps[0])) {
		t.Fatalf("reattach a créé une session tmux : %s", out)
	}

	// une session vivante du projet : rattachée, sans send-keys (la commande n'est pas relancée)
	if out, err := exec.Command("tmux", "-L", sock, "new-session", "-d", "-s", hostSession(ps[0]), "-x", "100", "-y", "30", "sleep 300").CombinedOutput(); err != nil {
		t.Fatalf("tmux : %s", out)
	}
	res = dispatchJSON(t, a, "reattach", map[string]any{"path": ps[0]})
	if res["ok"] != true || res["tab"] != ps[0] || a.term(ps[0]) == nil {
		t.Fatalf("session vivante : %v", res)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.term(ps[0]).info().State != "ready" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := a.term(ps[0]).info().State; got != "ready" {
		t.Errorf("état = %q", got)
	}
	// idempotent
	if res = dispatchJSON(t, a, "reattach", map[string]any{"path": ps[0]}); res["ok"] != true {
		t.Errorf("second appel : %v", res)
	}
	// projet inconnu
	if res = dispatchJSON(t, a, "reattach", map[string]any{"path": "/inconnu"}); res["ok"] != false {
		t.Errorf("projet inconnu : %v", res)
	}
}
