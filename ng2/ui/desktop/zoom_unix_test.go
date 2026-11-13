//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"claude-launcher-ng2/core"
)

// #138 : plein écran rapide même quand la team se lit lentement
// (CLAUDE_CONFIG_DIR par 9P sous Windows) : son config.json est un tube nommé
// dont la lecture ne revient pas ; le plein écran et son retour doivent être
// publiés en moins de 300 ms et le pane recapturé en moins d'une seconde.
func TestZoomNotSlowedByTeamRead(t *testing.T) {
	h := openPanel(t)
	d := filepath.Join(core.ClaudeDir(), "teams", "lente")
	_ = os.MkdirAll(d, 0o755)
	fifo := filepath.Join(d, "config.json")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("mkfifo : ", err)
	}
	t.Cleanup(func() { // débloque le tour lent resté sur la lecture
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
	zoomTiming(t, h.r, h.pt)
}
