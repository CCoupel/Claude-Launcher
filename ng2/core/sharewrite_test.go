package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClampShareWriteTimeout(t *testing.T) {
	for in, want := range map[int]int{0: 10, -3: 10, 1: 2, 2: 2, 50: 50, 120: 120, 500: 120} {
		if got := ClampShareWriteTimeout(in); got != want {
			t.Errorf("Clamp(%d) = %d, attendu %d", in, got, want)
		}
	}
}

// Un partage bloquant rend la main au délai ; l'écriture abandonnée est la seule en cours (refus immédiat des
// suivantes) ; dès qu'elle rend la main, les écritures reprennent.
func TestWithinShareBlockedThenResumes(t *testing.T) {
	release := make(chan struct{})
	ShareWriteHook = func(string) { <-release }
	defer func() { ShareWriteHook = nil }()
	t0 := time.Now()
	err := WithinShare(100*time.Millisecond, func() error { return nil })
	if !IsShareTimeout(err) || time.Since(t0) > time.Second {
		t.Fatalf("attendu un délai court, obtenu %v après %v", err, time.Since(t0))
	}
	t1 := time.Now()
	if err := WithinShare(time.Second, func() error { return errors.New("jamais") }); !IsShareTimeout(err) || time.Since(t1) > 200*time.Millisecond {
		t.Fatalf("la seconde écriture doit échouer tout de suite : %v", err)
	}
	close(release)
	deadline := time.Now().Add(30 * time.Second)
	for {
		shareMu.Lock()
		busy := shareBusy
		shareMu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("l'écriture abandonnée ne rend pas la main")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ShareWriteHook = nil
	if err := WithinShare(time.Second, func() error { return nil }); err != nil {
		t.Fatalf("reprise attendue : %v", err)
	}
}

// Refus immédiat (écriture précédente bloquée) : message distinct d'un vrai dépassement de délai.
func TestShareTimeoutBusyMessage(t *testing.T) {
	if m := (&ShareTimeout{After: 10 * time.Second}).Error(); m != "partage injoignable : config.conf non écrit après 10 s" {
		t.Errorf("message de délai = %q", m)
	}
	if m := (&ShareTimeout{After: 10 * time.Second, Busy: true}).Error(); !strings.Contains(m, "précédente") || strings.Contains(m, "après") {
		t.Errorf("message Busy = %q", m)
	}
}
