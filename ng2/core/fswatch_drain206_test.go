package core

import (
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// #206 : après l'échéance, drainClose rend faux MAIS une goroutine de fond continue de vider Events/Errors jusqu'au
// retour de Close ; plus aucune goroutine d'envoi/lecture ne reste bloquée, les handles sont libérés.
func TestDrainCloseApresEcheanceContinueDeVider206(t *testing.T) {
	events := make(chan fsnotify.Event) // non tamponnés : un envoi sans lecteur bloque
	errs := make(chan error)
	release := make(chan struct{})
	sent := make(chan struct{}, 2)
	closeFn := func() error { // Close lent : ne rend la main qu'une fois « release » fermé ET ses envois reçus
		<-release
		events <- fsnotify.Event{Name: "tardif"}
		errs <- nil
		sent <- struct{}{}
		return nil
	}
	if drainClose(closeFn, events, errs, 100*time.Millisecond, "test206") {
		t.Fatal("Close sans réponse dans le délai : faux attendu")
	}
	close(release) // Close se débloque APRÈS l'abandon : ses envois doivent trouver un lecteur
	select {
	case <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("après l'échéance, plus personne ne lit Events/Errors : Close reste bloqué sur son envoi (fuite)")
	}
}

func TestDrainCloseAucuneGoroutineResiduelle206(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		events := make(chan fsnotify.Event)
		errs := make(chan error)
		release := make(chan struct{})
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			_ = drainClose(func() error { <-release; events <- fsnotify.Event{}; return nil }, events, errs, 20*time.Millisecond, "test206")
		}()
		<-finished // échéance dépassée
		close(release)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Errorf("goroutines résiduelles : %d avant, %d après (lecteur ou fermeture bloqué)", before, n)
	}
}

func TestDrainCloseRendFauxALEcheanceBorne206(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	if drainClose(func() error { <-block; return nil }, make(chan fsnotify.Event), make(chan error), 150*time.Millisecond, "test206") {
		t.Error("faux attendu à l'échéance")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("l'arrêt doit rester borné par la limite : %v", d)
	}
}

func TestDrainCloseVraiSurveillantEtFermetureDoubleSansFuite206(t *testing.T) {
	before := runtime.NumGoroutine()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	if !closeFsWatcher(w, "test206") {
		t.Error("fermeture d'un vrai surveillant : vrai attendu")
	}
	time.Sleep(200 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > before+1 {
		t.Errorf("goroutine résiduelle après fermeture : %d → %d", before, n)
	}
}
