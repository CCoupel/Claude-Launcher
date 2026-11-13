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

func TestTmuxAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"3.2": true, "3.3a": true, "3.6": true, "next-3.4": true, "3.1c": false, "2.9": false, "4.0": true, "": false} {
		if got := tmuxAtLeast(v, 3, 2); got != want {
			t.Errorf("tmuxAtLeast(%q, 3.2) = %v", v, got)
		}
	}
	cs := parseClients("/dev/pts/3|0|attached,focused,UTF-8|12\n|1|attached,control-mode|13\nclient-14|1|control-mode,ignore-size|14\n/dev/pts/5|0|attached,ignore-size|15\n")
	if len(cs) != 2 || cs[0].name != "/dev/pts/3" || cs[0].ignoreSize || !cs[1].ignoreSize {
		t.Errorf("clients : %+v", cs)
	}
}

// #126 : deux clients attachés — l'interface (client de contrôle) et un tmux
// normal d'une autre taille, actif (comme le hub Linux) : la fenêtre garde la
// taille de l'interface, qui pilote ; en lecture seule, l'interface cède la
// taille à l'autre client. Sans second client : la taille suit l'interface.
func TestWindowSizeWithTwoClients(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil || !tmuxAtLeast(strings.TrimPrefix(strings.TrimSpace(string(out)), "tmux "), 3, 2) {
		t.Skip("tmux ≥ 3.2 requis (drapeaux de client)")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-size-" + strconv.Itoa(os.Getpid())
	outer := sock + "-outer"
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", outer, "kill-server").Run()
		_ = exec.Command("tmux", "-L", sock, "kill-server").Run()
	})
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho prêt\nexec cat\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")

	var r recorder
	a := newApp(t, r.emit)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(150, 45)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	win := "=" + hostSession(ps[0]) + ":"
	size := func() string {
		b, _ := exec.Command("tmux", "-L", sock, "display", "-p", "-t", win, "#{window_width}x#{window_height}").Output()
		return strings.TrimSpace(string(b))
	}
	waitSize := func(want string) {
		t.Helper()
		end := time.Now().Add(8 * time.Second)
		for size() != want && time.Now().Before(end) {
			time.Sleep(100 * time.Millisecond)
		}
		if got := size(); got != want {
			t.Fatalf("taille %s, attendu %s", got, want)
		}
	}
	waitSize("150x45") // seul client : la taille de l'interface

	// second client : un tmux normal de 90x25 (dans un autre serveur tmux),
	// actif (frappe), comme le hub Linux
	inner := "tmux -L " + sock + " attach -t " + win
	if out, err := exec.Command("tmux", "-L", outer, "new-session", "-d", "-x", "90", "-y", "25", inner).CombinedOutput(); err != nil {
		t.Fatalf("second client : %v %s", err, out)
	}
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		b, _ := exec.Command("tmux", "-L", sock, "list-clients", "-F", "#{client_control_mode}").Output()
		if strings.Count(string(b), "0") >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	pt.govern() // (au battement suivant sinon, toutes les 5 s)
	for i := 0; i < 5; i++ {
		_ = exec.Command("tmux", "-L", outer, "send-keys", "x").Run()
		time.Sleep(200 * time.Millisecond)
	}
	if got := size(); got != "150x45" {
		t.Errorf("l'autre client a pris la taille : %s", got)
	}
	// l'interface redimensionnée : la fenêtre suit, malgré l'autre client
	pt.resize(120, 40)
	waitSize("120x40")

	// lecture seule (un autre launcher pilote) : l'interface cède la taille
	pt.mu.Lock()
	pt.readOnly = true
	pt.mu.Unlock()
	pt.govern()
	_ = exec.Command("tmux", "-L", outer, "send-keys", "y").Run()
	// taille de l'autre client (la barre d'état du tmux qui le porte en prend une ligne)
	b, _ := exec.Command("tmux", "-L", sock, "list-clients", "-F", "#{client_control_mode} #{client_width}x#{client_height}").Output()
	other := ""
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "0" {
			other = f[1]
		}
	}
	// la fenêtre prend la taille de l'autre client (moins sa barre d'état, s'il en a une)
	end = time.Now().Add(8 * time.Second)
	w := strings.Split(other, "x")[0] + "x"
	for !strings.HasPrefix(size(), w) && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := size(); !strings.HasPrefix(got, w) {
		t.Fatalf("lecture seule : taille %s, attendu celle de l'autre client (%s)", got, other)
	}
	a.detachTab(ps[0])
}
