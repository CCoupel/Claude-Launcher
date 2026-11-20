package desktop

import (
	"testing"
	"time"
)

// Les dates d'usage sont strictement croissantes même quand l'horloge ne bouge pas (Windows : pas de ~1 ms ou plus).
func TestStampStrictementCroissant224(t *testing.T) {
	w := &webBridge{}
	same := time.Now()
	prev := time.Time{}
	for i := 0; i < 1000; i++ {
		s := w.stamp(same)
		if !s.After(prev) {
			t.Fatalf("date %d non croissante : %v après %v", i, s, prev)
		}
		prev = s
	}
}

// L'éviction retire TOUJOURS la plus ancienne session (ordre d'amorçage), jamais une plus récente tirée au hasard
// parmi des dates identiques.
func TestEvictionRetireLaPlusAncienne224(t *testing.T) {
	w := &webBridge{subs: map[chan []byte]bool{}}
	var ids []string
	for i := 0; i < 3*maxSessions; i++ {
		ids = append(ids, w.newSession())
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.sessions) != maxSessions {
		t.Fatalf("%d sessions, attendu %d", len(w.sessions), maxSessions)
	}
	for i, id := range ids {
		_, alive := w.sessions[id]
		if want := i >= len(ids)-maxSessions; alive != want {
			t.Fatalf("session %d : vivante=%v, attendu %v (seules les %d dernières survivent)", i, alive, want, maxSessions)
		}
	}
}
