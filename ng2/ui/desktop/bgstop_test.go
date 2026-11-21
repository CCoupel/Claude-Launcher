package desktop

import (
	"sync/atomic"
	"testing"
	"time"
)

// Les tâches de fond lancées par a.bg sont suivies : Stop les attend (borné) ; après Stop plus aucune ne démarre (écrivain
// tardif dans l'environnement restauré du test suivant / dossier en cours de suppression).
func TestBgSuiviEtRefuseApresStop(t *testing.T) {
	var r recorder
	a := newApp(t, r.emit)
	var done atomic.Bool
	a.bg(func() { time.Sleep(300 * time.Millisecond); done.Store(true) })
	start := time.Now()
	a.Stop()
	if !done.Load() || time.Since(start) < 250*time.Millisecond {
		t.Errorf("Stop n'a pas attendu la tâche de fond (finie=%v, %v)", done.Load(), time.Since(start))
	}
	var ran atomic.Bool
	a.bg(func() { ran.Store(true) })
	time.Sleep(50 * time.Millisecond)
	if ran.Load() {
		t.Error("tâche de fond démarrée après Stop")
	}
	if !a.waitBackground(time.Second) {
		t.Error("waitBackground : tâches non finies")
	}
}

func TestStopNAttendPasUneTacheDeFondBloquee(t *testing.T) {
	defer bgStopWait.Set(bgStopWait.Set(150 * time.Millisecond))
	var r recorder
	a := newApp(t, r.emit)
	release := make(chan struct{})
	a.bg(func() { <-release })
	defer close(release)
	start := time.Now()
	a.Stop()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Stop retenu par une tâche bloquée : %v", d)
	}
}
