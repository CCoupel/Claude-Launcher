package desktop

import (
	"testing"
	"time"
)

// Après Stop, plus aucun relevé de consommation ne démarre (usage.json / .err écrits dans un environnement restauré) ;
// avant Stop, un relevé en cours est attendu — borné.
func TestUsageNeDemarrePlusApresStop(t *testing.T) {
	var r recorder
	a := newApp(t, r.emit)
	if !a.usageBegin() {
		t.Fatal("App en marche : relevé refusé")
	}
	go func() { time.Sleep(300 * time.Millisecond); a.usageGate.end() }()
	start := time.Now()
	a.Stop()
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("Stop n'a pas attendu le relevé en cours (%v)", d)
	}
	if a.usageBegin() {
		t.Error("relevé accepté après Stop")
	}
	a.refreshUsage() // sans effet ni écriture : retourne tout de suite
	a.readUsage()
	if n := gateOf(&a.usageGate); n != 0 {
		t.Errorf("barrière conso = %d", n)
	}
}

func TestStopNAttendPasUnReleveBloque(t *testing.T) {
	defer usageStopWait.Set(usageStopWait.Set(150 * time.Millisecond))
	var r recorder
	a := newApp(t, r.emit)
	a.usageBegin()
	defer a.usageGate.end()
	start := time.Now()
	a.Stop()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Stop retenu par un relevé bloqué : %v", d)
	}
}

func gateOf(g *openGate) int { g.mu.Lock(); defer g.mu.Unlock(); return g.n }
