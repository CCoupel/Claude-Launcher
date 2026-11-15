package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #153 : le surveillant de mise en page relit config.conf quand il change et remet la fenêtre en page, sans relance.
func TestLayoutWatchReappliesOnConfigChange(t *testing.T) {
	marker := isoInner(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, win, _, proj, leader, mates := visProject(t, marker)
	done := make(chan struct{})
	go func() { defer close(done); RunLayoutWatch(win, leader, proj) }()
	t.Cleanup(func() {
		_ = dtmux("kill-server").Run()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("le surveillant ne s'est pas arrêté")
		}
	})
	width := func(pane string) int {
		out, _ := dtmux("display-message", "-p", "-t", pane, "#{pane_width}|#{window_width}").Output()
		var pw, ww int
		fmt.Sscanf(strings.ReplaceAll(strings.TrimSpace(string(out)), "|", " "), "%d %d", &pw, &ww)
		if ww == 0 {
			return -1
		}
		return pw * 100 / ww
	}
	// organisation d'avant : le leader occupe toute la largeur (ligne du haut)
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		end := time.Now().Add(15 * time.Second)
		for time.Now().Before(end) {
			if ok() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s : leader à %d %%, premier teammate à %d %% de la largeur", what, width(leader), width(mates[0]))
	}
	// la fenêtre est remise en page par le surveillant : les teammates passent sur une ligne sous le leader
	waitFor("top par défaut", func() bool { return width(leader) > 90 && width(mates[0]) < 90 })
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=left\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor("left après changement de config.conf", func() bool { w := width(leader); return w > 30 && w < 60 })
	// retour à top
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=top\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor("top après retour", func() bool { return width(leader) > 90 })
}
