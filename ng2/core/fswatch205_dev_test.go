package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// #206 : après l'échéance, le vidage continue en fond jusqu'au retour de Close ; #205 : balayage des .tmp d'état.

func TestDev206DrainContinuesAfterTimeout(t *testing.T) {
	ev := make(chan fsnotify.Event)
	er := make(chan error)
	release := make(chan struct{})
	// Close qui ne rend la main qu'une fois TOUS les évènements envoyés (lecteur du surveillant bloqué sur l'envoi)
	sent := make(chan struct{})
	closeFn := func() error {
		for i := 0; i < 5; i++ {
			ev <- fsnotify.Event{}
		}
		er <- errors.New("x")
		close(sent)
		<-release
		return nil
	}
	start := time.Now()
	if drainClose(closeFn, ev, er, 50*time.Millisecond, "test") {
		t.Fatal("Close bloqué : faux attendu")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("arrêt non borné : %v", time.Since(start))
	}
	select {
	case <-sent: // les envois ont été vidés par la goroutine de fond, après le retour de drainClose
	case <-time.After(5 * time.Second):
		t.Fatal("plus aucun lecteur après l'échéance : le surveillant reste bloqué sur l'envoi")
	}
	close(release)
}

func TestDev205SweepStateTmp(t *testing.T) {
	t.Setenv(CacheDirVar, t.TempDir())
	d := stateDir()
	old := filepath.Join(d, "abc.json.123.tmp")
	fresh := filepath.Join(d, "abc.json.456.tmp")
	keep := filepath.Join(d, "abc.json")
	for _, p := range []string{old, fresh, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-30 * time.Hour)
	_ = os.Chtimes(old, past, past)
	_ = os.Chtimes(keep, past, past)
	if n := sweepStateTmp(); n != 1 {
		t.Errorf("supprimés : %d", n)
	}
	for p, want := range map[string]bool{old: false, fresh: true, keep: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s présent=%v, attendu %v", filepath.Base(p), err == nil, want)
		}
	}
}
