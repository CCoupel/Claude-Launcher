package desktop

import (
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Relance après une mise à jour (mode web, --addr fixe) : le port que
// l'ancien processus vient de libérer est repris dès qu'il est libre.
func TestListenRetry(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := busy.Addr().String()
	go func() { time.Sleep(400 * time.Millisecond); busy.Close() }()
	start := time.Now()
	ln, err := listenRetry(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("port non repris : %v", err)
	}
	ln.Close()
	if time.Since(start) < 300*time.Millisecond {
		t.Error("le port était pris : la reprise aurait dû attendre")
	}
	// port libre demandé (« :0 ») : pas d'attente
	if ln, err := listenRetry("127.0.0.1:0", 5*time.Second); err != nil {
		t.Error(err)
	} else {
		ln.Close()
	}
}

// Cartes : jamais d'appel tmux sur la goroutine d'émission. Un canal de
// contrôle qui ne répond pas ne doit pas les bloquer.
func TestCardsDoNotWaitOnTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep absent")
	}
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := NewApp(r.emit)
	pt := newProjTerm(a, core.Node{Path: ps[0], Key: "alpha", Name: "alpha"}, core.LaunchNormal)
	c, err := startClient([]string{"sleep", "30"}, nil) // ne répond jamais
	if err != nil {
		t.Fatal(err)
	}
	defer c.cmd.Process.Kill()
	pt.client, pt.win = c, "=x:"
	pt.infos = []core.PaneInfo{{ID: "%0", Cmd: "claude"}}
	a.terms[ps[0]] = pt
	a.sel = ps[0]
	done := make(chan Cards, 1)
	go func() { done <- a.Cards() }()
	select {
	case cs := <-done:
		if !cs.Open {
			t.Errorf("cartes : %+v", cs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Cards bloquée par le canal tmux")
	}
}

// Fin d'onglet en concurrence (quitter pendant qu'une session se termine) :
// pas de double fermeture.
func TestStopClosedOnce(t *testing.T) {
	a := &App{emit: func(string, any) {}, cfg: core.LoadConfig(), terms: map[string]*projTerm{}}
	pt := newProjTerm(a, core.Node{Path: "/p", Name: "p"}, core.LaunchNormal)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); pt.detach() }()
		go func() { defer wg.Done(); pt.closeStop() }()
	}
	wg.Wait()
	select {
	case <-pt.stop:
	default:
		t.Error("stop non fermé")
	}
}
