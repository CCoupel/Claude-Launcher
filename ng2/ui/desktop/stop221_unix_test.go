//go:build !windows

package desktop

import (
	"syscall"
	"testing"
	"time"
)

// Terminal bloqué : le client tmux ne réagit plus (processus arrêté, ni détachement ni fermeture de l'entrée ne le font
// sortir) — Stop tient dans les 25 s et l'arrêt forcé de l'arbre de processus ne laisse rien derrière.
func TestStopForcesStuckClient221(t *testing.T) {
	f := openFakeProject(t)
	pids := childClients(t)
	if len(pids) == 0 {
		t.Fatal("précondition : aucun client tmux -C enfant")
	}
	for _, p := range pids {
		if err := syscall.Kill(p, syscall.SIGSTOP); err != nil {
			t.Fatalf("SIGSTOP %d : %v", p, err)
		}
	}
	t.Cleanup(func() { // ne jamais laisser un processus arrêté
		for _, p := range pids {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	})
	t0 := time.Now()
	f.a.Stop()
	if d := time.Since(t0); d > stopBudget {
		t.Errorf("Stop avec terminal bloqué : %v, attendu ≤ %v", d, stopBudget)
	}
	assertNoTerminalLeft(t)
}
