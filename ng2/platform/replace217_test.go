package platform

// #217 : remplacement de fichier avec retry Windows, en logique pure (erreurs injectées, valable sur tous les
// systèmes). Tag `atomic217` : API posée à l'écriture ; noms ATTENDUS :
//
//	func IsReplaceRetryable(err error) bool                                    // errno 5 (ACCESS_DENIED) ou 32 (SHARING_VIOLATION), même enveloppé
//	func ReplaceWithRetry(src, dst string, rename func(a, b string) error, sleep func(time.Duration)) error
//	func ReplaceFile(src, dst string) error                                    // ReplaceWithRetry(os.Rename, time.Sleep) sous Windows

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func linkErr217(code syscall.Errno) error {
	return &os.LinkError{Op: "rename", Old: "a", New: "b", Err: code}
}

func TestIsReplaceRetryable217(t *testing.T) {
	for _, c := range []syscall.Errno{5, 32} {
		if !IsReplaceRetryable(linkErr217(c)) {
			t.Errorf("errno %d enveloppé : doit être retenté", c)
		}
		if !IsReplaceRetryable(c) {
			t.Errorf("errno %d nu : doit être retenté", c)
		}
	}
	for _, e := range []error{nil, errors.New("autre"), linkErr217(2), linkErr217(13), os.ErrNotExist} {
		if IsReplaceRetryable(e) {
			t.Errorf("%v : ne doit pas être retenté", e)
		}
	}
}

func TestReplaceWithRetrySuccesDuPremierCoup217(t *testing.T) {
	n := 0
	var sleeps []time.Duration
	err := ReplaceWithRetry("a", "b", func(a, b string) error {
		n++
		if a != "a" || b != "b" {
			t.Errorf("args = %q %q", a, b)
		}
		return nil
	}, func(d time.Duration) { sleeps = append(sleeps, d) })
	if err != nil || n != 1 || len(sleeps) != 0 {
		t.Errorf("err=%v essais=%d attentes=%v", err, n, sleeps)
	}
}

func TestReplaceWithRetryReussitApresVerrous217(t *testing.T) {
	n := 0
	var sleeps []time.Duration
	err := ReplaceWithRetry("a", "b", func(a, b string) error {
		n++
		if n < 3 {
			return linkErr217(32)
		}
		return nil
	}, func(d time.Duration) { sleeps = append(sleeps, d) })
	if err != nil || n != 3 || len(sleeps) != 2 {
		t.Fatalf("err=%v essais=%d attentes=%v", err, n, sleeps)
	}
	if sleeps[0] != 50*time.Millisecond {
		t.Errorf("première attente = %v, attendu 50 ms", sleeps[0])
	}
	if sleeps[1] <= sleeps[0] {
		t.Errorf("attentes non croissantes : %v", sleeps)
	}
}

func TestReplaceWithRetryAbandonneEtMessageExplicite217(t *testing.T) {
	n := 0
	var sleeps []time.Duration
	cause := linkErr217(5)
	err := ReplaceWithRetry("a", "b", func(a, b string) error { n++; return cause },
		func(d time.Duration) { sleeps = append(sleeps, d) })
	if err == nil {
		t.Fatal("erreur attendue après épuisement des essais")
	}
	if n < 5 || n > 6 {
		t.Errorf("%d essais, attendu 5 (jusqu'à 6 si 5 reprises)", n)
	}
	if len(sleeps) != n-1 {
		t.Errorf("%d attentes pour %d essais", len(sleeps), n)
	}
	for i, d := range sleeps {
		if d < 50*time.Millisecond || d > 800*time.Millisecond {
			t.Errorf("attente %d = %v hors de 50→800 ms", i, d)
		}
		if i > 0 && d < sleeps[i-1] {
			t.Errorf("attentes décroissantes : %v", sleeps)
		}
	}
	if n != 5 || len(sleeps) != 4 || sleeps[3] != 400*time.Millisecond {
		t.Errorf("essais=%d attentes=%v, attendu 5 essais, 50/100/200/400 ms", n, sleeps)
	}
	if !strings.Contains(err.Error(), "verrouillé par un autre programme") {
		t.Errorf("message non explicite : %v", err)
	}
	if !errors.Is(err, cause) && !errors.Is(err, syscall.Errno(5)) {
		t.Errorf("cause perdue : %v", err)
	}
}

func TestReplaceWithRetryErreurNonRetentable217(t *testing.T) {
	n := 0
	sleeps := 0
	boom := errors.New("disque plein")
	err := ReplaceWithRetry("a", "b", func(a, b string) error { n++; return boom },
		func(time.Duration) { sleeps++ })
	if !errors.Is(err, boom) || n != 1 || sleeps != 0 {
		t.Errorf("err=%v essais=%d attentes=%d : aucune reprise attendue", err, n, sleeps)
	}
	if strings.Contains(err.Error(), "verrouillé") {
		t.Errorf("message de verrou pour une autre erreur : %v", err)
	}
}

func TestReplaceFileRemplaceReellementUnFichier217(t *testing.T) {
	d := t.TempDir()
	src, dst := d+"/src", d+"/dst"
	_ = os.WriteFile(dst, []byte("ancien"), 0o600)
	_ = os.WriteFile(src, []byte("nouveau"), 0o600)
	if err := ReplaceFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "nouveau" {
		t.Errorf("dst = %q", b)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("src encore présent")
	}
}
