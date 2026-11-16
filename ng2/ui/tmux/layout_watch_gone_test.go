package tmux

import (
	"testing"
	"time"
)

// Le surveillant de mise en page rend la main quand la fenêtre n'existe plus — y compris si elle
// disparaît avant sa première lecture réussie (sinon il tourne à 1 Hz, à jamais : processus qui
// survivent à la relance du hub).
func TestLayoutWatchReturnsWhenWindowGone(t *testing.T) {
	marker := isoInner(t)
	_, win, _, _, leader, _ := visProject(t, marker)
	proj := t.TempDir()            // aucune team : seule la lecture de la fenêtre peut arrêter le surveillant
	_ = dtmux("kill-server").Run() // la fenêtre disparaît avant le premier tour
	done := make(chan struct{})
	go func() { defer close(done); RunLayoutWatch(win, leader, proj) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("le surveillant de mise en page ne s'arrête pas quand la fenêtre est fermée")
	}
}
