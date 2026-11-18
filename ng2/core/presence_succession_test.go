package core

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/platform"
)

// Pod de StatefulSet recréé (même nom de machine, même pid) : le témoin du prédécesseur est repris aussitôt ; le même
// processus, un autre processus vivant et une autre machine restent refusés ; ancien format repris.
func TestPresenceSuccessionSamePid(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_HUB_NAME", "hub-succession")
	oldW, oldS := presenceWait, presenceSuccessorStale
	presenceWait, presenceSuccessorStale = 700*time.Millisecond, 400*time.Millisecond
	defer func() { presenceWait, presenceSuccessorStale = oldW, oldS }()
	host, pid := platform.RealHostName(), os.Getpid()
	now := time.Now().Format(time.RFC3339)
	write := func(content string, age time.Duration) {
		t.Helper()
		if err := os.WriteFile(presencePath(), []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-age)
		_ = os.Chtimes(presencePath(), old, old)
	}
	acquire := func() (time.Duration, error) {
		t0 := time.Now()
		rel, err := AcquirePresence()
		if err == nil {
			rel()
		}
		return time.Since(t0), err
	}
	// prédécesseur de même pid, SANS battement depuis assez longtemps : repris tout de suite
	write(fmt.Sprintf("%s %d %s tok autre-processus", host, pid, now), time.Second)
	if d, err := acquire(); err != nil || d > 300*time.Millisecond {
		t.Errorf("prédécesseur silencieux : %v en %v (repris tout de suite attendu)", err, d)
	}
	// prédécesseur de même pid au battement FRAIS (nœud isolé, pod forcé) : jamais écrasé — le démarrage attend dans le
	// processus puis refuse en nommant le prédécesseur ; ici le battement continue
	// marges larges (battement 50 ms contre 1 s de silence exigé) : un runner lent (Windows en CI) peut retarder un
	// battement de plusieurs centaines de ms sans que le prédécesseur ne paraisse silencieux
	presenceWait, presenceSuccessorStale = 1500*time.Millisecond, time.Second
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				now := time.Now()
				_ = os.Chtimes(presencePath(), now, now)
			}
		}
	}()
	write(fmt.Sprintf("%s %d %s tok vivant", host, pid, now), 0)
	d, err := acquire()
	close(stop)
	presenceWait, presenceSuccessorStale = 700*time.Millisecond, 400*time.Millisecond // marges d'origine pour la suite
	if err == nil || !strings.Contains(err.Error(), "vivant") || d < time.Second {
		t.Errorf("prédécesseur au battement frais : erreur %v après %v (attente puis refus nommant le prédécesseur attendus)", err, d)
	}
	// prédécesseur fraîchement arrêté : le démarrage ATTEND le silence de son battement puis le reprend (pas de sortie)
	write(fmt.Sprintf("%s %d %s tok arrete", host, pid, now), 0)
	if d, err := acquire(); err != nil || d < 300*time.Millisecond || d > 650*time.Millisecond {
		t.Errorf("prédécesseur arrêté à l'instant : %v après %v (attente d'environ 400 ms puis reprise attendues)", err, d)
	}
	write(fmt.Sprintf("%s %d %s tok %s", host, pid, now, processNonce), time.Hour) // le même processus : jamais repris comme prédécesseur
	if _, err := acquire(); err != nil {                                           // périmé (une heure) : repris pour cause de péremption, pas comme prédécesseur
		t.Errorf("témoin périmé du même processus : %v", err)
	}
	write(fmt.Sprintf("%s %d %s tok %s", host, pid, now, processNonce), 0) // le même processus, frais
	if d, err := acquire(); err == nil || d > 300*time.Millisecond {
		t.Errorf("le même processus doit être refusé tout de suite : %v en %v", err, d)
	}
	write(fmt.Sprintf("%s %d %s tok x", host, os.Getppid(), now), 0) // un autre processus vivant
	if _, err := acquire(); err == nil {
		t.Error("un autre processus vivant doit rester refusé")
	}
	write(fmt.Sprintf("autre-machine %d %s tok x", pid, now), 0) // une autre machine
	if _, err := acquire(); err == nil {
		t.Error("une autre machine doit rester refusée")
	}
	write(fmt.Sprintf("%s %d %s tok", host, pid, now), time.Second) // ancien format, silencieux
	if _, err := acquire(); err != nil {
		t.Errorf("témoin d'ancien format silencieux non repris : %v", err)
	}
}

// La libération retire le témoin de CE launcher (régression : elle cherchait l'identifiant en préfixe du fichier et ne
// retirait jamais rien) et ne touche jamais le témoin d'un autre.
func TestPresenceReleaseRemovesOwnWitnessOnly(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_HUB_NAME", "hub-release")
	rel, err := AcquirePresence()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(presencePath()); err != nil {
		t.Fatalf("témoin absent après la prise : %v", err)
	}
	rel()
	if _, err := os.Stat(presencePath()); err == nil {
		t.Error("le témoin n'est pas retiré à la libération")
	}
	rel2, err := AcquirePresence()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(presencePath(), []byte("autre-machine 1 2026-01-01T00:00:00Z autre-id autre-proc\n"), 0o644) // repris par un autre
	rel2()
	if _, err := os.Stat(presencePath()); err != nil {
		t.Error("la libération a retiré le témoin d'un autre launcher")
	}
}

// reclaimStaleIfMin : un battement arrivé entre le constat de silence et la reprise (date rafraîchie, contenu inchangé)
// n'est jamais écrasé ; un fichier resté silencieux est repris.
func TestReclaimStaleIfMinRevoitLeBattement(t *testing.T) {
	p := t.TempDir() + "/temoin"
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := reclaimStaleIfMin(p, time.Hour, "x\n", 400*time.Millisecond); ok || err != nil {
		t.Errorf("témoin battu à l'instant repris : %v %v", ok, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("témoin frais supprimé")
	}
	old := time.Now().Add(-2 * time.Second)
	_ = os.Chtimes(p, old, old)
	if ok, err := reclaimStaleIfMin(p, time.Hour, "x\n", 400*time.Millisecond); !ok || err != nil {
		t.Errorf("témoin silencieux non repris : %v %v", ok, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("témoin silencieux encore là")
	}
}
