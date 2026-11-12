package tmux

import "testing"

// un teammate (%3) placé à l'index 0 ne doit pas être pris pour le leader (%0)
func TestWinStateLeader(t *testing.T) {
	ws := winState{panes: []winPane{{id: "%3", index: 0}, {id: "%10", index: 1}, {id: "%0", index: 2}, {id: "%2", index: 3}}}
	if got := ws.leader(); got != "%0" {
		t.Fatalf("leader = %q, attendu %%0", got)
	}
	if got := (winState{}).leader(); got != "" {
		t.Fatalf("fenêtre vide : %q", got)
	}
}
