//go:build !windows

package desktop

import (
	"sync"
	"testing"
	"time"
)

// DATA RACE (QA) : prepareHost écrivait t.host / t.sock / t.sess pendant que Snapshot (canForce) et le rafraîchissement les
// lisaient sans le verrou du terminal. Ouverture d'un projet sur l'hôte avec des Snapshot et des canForce en boucle (go test
// -race : échoue sur l'ancien code).
func TestOuvertureHoteEtSnapshotSansCourse(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	noContainerBackend(t)
	fakeClaudeSession214(t)
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = a.Snapshot()
				if t := a.term(ps[0]); t != nil {
					_ = t.info()
					_, _ = t.hostTarget()
				}
			}
		}()
	}
	if _, err := a.open(ps[0], 0); err != nil {
		t.Fatal(err)
	}
	if !a.waitOpenings(20 * time.Second) {
		t.Fatal("l'ouverture ne finit pas")
	}
	time.Sleep(200 * time.Millisecond) // quelques Snapshot de plus sur le terminal ouvert
	close(stop)
	wg.Wait()
}
