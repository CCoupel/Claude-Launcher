package core

import (
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/platform"
)

// Témoin de présence après un retour arrière rapide (helm rollback) : le pod recréé d'un StatefulSet a le MÊME nom de
// machine et, dans un conteneur, le MÊME pid (1) que son prédécesseur disparu ; il doit reconnaître le témoin de
// l'ancien processus (identifiant de processus différent) et le reprendre tout de suite, sans « launcher déjà actif ».
// Rapport : _work/reports/dev-backend-presence-20261117-131646.md (A, 5 cas). HOME factice ; attentes sur condition.

func twPresenceSetup(t *testing.T) string {
	t.Helper()
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_HUB_NAME", "hub-rollback")
	return presencePath()
}

// twAcquire : prend la présence (succès attendu) ; rend le contenu du témoin et la libération.
func twAcquire(t *testing.T) (string, func()) {
	t.Helper()
	rel, err := AcquirePresence()
	if err != nil {
		t.Fatalf("AcquirePresence : %v", err)
	}
	b, err := os.ReadFile(presencePath())
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b)), rel
}

// twForeignNonce : contenu d'un témoin frais écrit par « un autre processus » : champs de own (hôte pid date id [nonce]) avec
// l'identifiant de processus remplacé ; hostOverride/pidOverride (si non vides) remplacent l'hôte et le pid.
func twWitness(t *testing.T, host string, pid int, fields ...string) {
	twWitnessAged(t, 0, host, pid, fields...)
}

// twWitnessAged : témoin vieilli de age (date du fichier) : un prédécesseur de même pid n'est repris qu'après
// presenceSuccessorStale sans battement ; on vieillit le témoin plutôt que d'attendre.
func twWitnessAged(t *testing.T, age time.Duration, host string, pid int, fields ...string) {
	t.Helper()
	line := host + " " + itoaCore(pid) + " " + time.Now().Format(time.RFC3339) + " " + strings.Join(fields, " ")
	if err := os.WriteFile(presencePath(), []byte(strings.TrimSpace(line)+"\n"), 0o644); err != nil { // mtime = maintenant : témoin FRAIS
		t.Fatal(err)
	}
	if age > 0 {
		old := time.Now().Add(-age)
		_ = os.Chtimes(presencePath(), old, old)
	}
}

func itoaCore(n int) string { return intStr(n) }

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

// (1) témoin frais, même hôte, MÊME PID, autre identifiant de processus → le successeur le reprend.
func TestPresenceSuccessorWithSamePIDTakesOverFreshWitness(t *testing.T) {
	twPresenceSetup(t)
	own, rel := twAcquire(t) // un premier « processus » (ici le nôtre) pose son témoin
	rel()                    // libère : on réécrit le témoin à la main, comme l'aurait laissé un pod tué net
	f := strings.Fields(own)
	if len(f) < 4 {
		t.Fatalf("témoin inattendu : %q", own)
	}
	// même hôte et même pid, identifiants différents (id du launcher et identifiant de processus d'un AUTRE processus)
	fields := append([]string{}, f[3:]...)
	fields[len(fields)-1] = "predecesseur-disparu-0123456789abcdef"
	twWitnessAged(t, presenceSuccessorStale+2*time.Second, f[0], os.Getpid(), fields...) // sans battement depuis plus que presenceSuccessorStale
	t0 := time.Now()
	rel2, err := AcquirePresence()
	if time.Since(t0) > 3*time.Second {
		t.Errorf("reprise lente (%v) : un témoin déjà silencieux doit être repris tout de suite", time.Since(t0))
	}
	if err != nil {
		t.Fatalf("le successeur (même nom de machine, même pid 1 dans son conteneur) est refusé : %v", err)
	}
	rel2()
}

// (2) témoin frais écrit par CE processus : un second AcquirePresence du même processus reste refusé.
func TestPresenceSameProcessSecondAcquireStillRefused(t *testing.T) {
	twPresenceSetup(t)
	_, rel := twAcquire(t)
	defer rel()
	if r2, err := AcquirePresence(); err == nil {
		r2()
		t.Fatal("un second launcher du même nom, dans le même processus, a été admis")
	} else if !strings.Contains(err.Error(), "déjà actif") {
		t.Errorf("message : %v", err)
	}
}

// (3) témoin frais, même hôte, pid d'un AUTRE processus VIVANT (le père du test) : refusé.
func TestPresenceFreshWitnessOfAnotherLiveProcessRefused(t *testing.T) {
	twPresenceSetup(t)
	own, rel := twAcquire(t)
	rel()
	f := strings.Fields(own)
	fields := append([]string{}, f[3:]...)
	fields[len(fields)-1] = "un-autre-processus-vivant-0123456789"
	twWitness(t, f[0], os.Getppid(), fields...)
	if r, err := AcquirePresence(); err == nil {
		r()
		t.Fatal("un témoin frais d'un autre processus vivant de la même machine a été repris")
	}
}

// (4) témoin frais d'une AUTRE machine : refusé (aucune conclusion possible sur son processus).
func TestPresenceFreshWitnessOfAnotherMachineRefused(t *testing.T) {
	twPresenceSetup(t)
	own, rel := twAcquire(t)
	rel()
	f := strings.Fields(own)
	fields := append([]string{}, f[3:]...)
	twWitness(t, "autre-machine-"+platform.RealHostName(), os.Getpid(), fields...)
	if r, err := AcquirePresence(); err == nil {
		r()
		t.Fatal("un témoin frais d'une autre machine a été repris")
	}
}

// (5) témoin d'ANCIEN format (sans identifiant de processus), même hôte, même pid : repris (rien n'atteste que c'est ce processus).
func TestPresenceOldFormatWitnessWithSamePIDIsTakenOver(t *testing.T) {
	twPresenceSetup(t)
	own, rel := twAcquire(t)
	rel()
	f := strings.Fields(own)
	// ancien format : « hôte pid date identifiant-du-launcher » — sans le dernier champ (identifiant de processus)
	old := f[3:]
	if len(old) > 1 {
		old = old[:len(old)-1]
	}
	twWitnessAged(t, presenceSuccessorStale+2*time.Second, f[0], os.Getpid(), old...)
	rel2, err := AcquirePresence()
	if err != nil {
		t.Fatalf("témoin de l'ancien format, même hôte et même pid, refusé : %v", err)
	}
	rel2()
}
