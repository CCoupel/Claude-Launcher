package main

import "testing"

// La fenêtre native ne fait pas attendre une mise à jour avant de s'ouvrir
// (bandeau orange → jaune → vert) ; la coque tmux garde la sienne au lancement.
func TestStartupUpdate(t *testing.T) {
	if startupUpdate("desktop") {
		t.Error("desktop : mise à jour bloquante avant la fenêtre")
	}
	if !startupUpdate("shell") {
		t.Error("shell : mise à jour au lancement perdue")
	}
}
