package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Organisation (hors env) et couleur restent à chaud sur un projet ouvert ; blockers de l'env = ceux du déplacement, sauf
// « déplacement inachevé ».
func TestEnvEditBlockers216(t *testing.T) {
	env := MoveEnv{OpenHere: func(string) bool { return true }, Foreign: func(string) string { return "pod-x" }, Running: func(string) string { return "c sur b" }}
	b := EnvEditBlockers(env, "/p")
	if len(b) != 3 {
		t.Fatalf("blockers = %v, attendu ouvert ici + autre launcher + conteneur actif", b)
	}
	m := EnvEditRefusal(env, "/p")
	if m != "projet ouvert dans ce launcher — fermez le projet pour modifier son environnement" { // première raison + consigne
		t.Errorf("message = %q", m)
	}
	if strings.Join(b, "|") != "projet ouvert dans ce launcher|projet ouvert par un autre launcher (pod-x)|conteneur ou pod en cours d'exécution (c sur b)" {
		t.Errorf("blockers = %v", b)
	}
	free := MoveEnv{OpenHere: func(string) bool { return false }, Foreign: func(string) string { return "" }, Running: func(string) string { return "" }}
	if m := EnvEditRefusal(free, "/p"); m != "" {
		t.Errorf("projet fermé : refus %q", m)
	}
}

// SaveConfigGuarded : garde appelée avant toute écriture, puis revérifiée juste avant (sous le verrou d'env.yaml) ; refus :
// EnvLockedError (errors.Is ErrEnvLocked), fichier intact ; autres sortes de fichiers : jamais gardés.
func TestSaveConfigGuardedRevalideSousVerrou216(t *testing.T) {
	unixOnly(t)
	proj := psEnv(t)
	src := "image: docker.io/library/debian:12\npackages: [jq]\n"
	psWrite(t, EnvPath(proj), src)
	st, _ := os.Stat(EnvPath(proj))
	calls := 0
	free := func() []string { calls++; return nil }                                        // contrôle complet (lent) : hors verrou, une fois
	late := func() []string { return []string{"projet ouvert par un autre launcher (x)"} } // revérification rapide, sous verrou
	_, err := SaveConfigGuarded(ConfProjectEnv, EnvPath(proj), []byte("image: docker.io/library/debian:12\npackages: [git]\n"), st.ModTime(), free, late)
	var le *EnvLockedError
	if !errors.As(err, &le) || !errors.Is(err, ErrEnvLocked) || len(le.Blockers) != 1 || calls != 1 {
		t.Fatalf("err = %v (appels %d)", err, calls)
	}
	if b, _ := os.ReadFile(EnvPath(proj)); string(b) != src {
		t.Error("env.yaml écrit malgré la revérification")
	}
	if _, err := SaveConfigGuarded(ConfProjectEnv, EnvPath(proj), []byte("image: docker.io/library/debian:12\npackages: [git]\n"), st.ModTime(), func() []string { return nil }, func() []string { return nil }); err != nil {
		t.Fatalf("libre : %v", err)
	}
}

func TestEnvEditCheckInjoignableEstUnAvertissement216(t *testing.T) {
	env := MoveEnv{OpenHere: func(string) bool { return false },
		Foreign: func(string) string { return UnverifiablePrefix + " : propriétaire non vérifiable" },
		Running: func(string) string { return UnverifiablePrefix + " : conteneur non vérifiable" }}
	b, w := EnvEditCheck(env, "/p")
	if len(b) != 0 || len(w) != 1 || w[0] != EnvWarnUnknown {
		t.Errorf("blockers=%v warns=%v", b, w)
	}
	env.OpenHere = func(string) bool { return true }
	if b, _ := EnvEditCheck(env, "/p"); len(b) != 1 {
		t.Errorf("ouvert ici = blocker certain : %v", b)
	}
	// le déplacement, lui, reste fermé quand la vérification est impossible (fail-closed)
	env.OpenHere = func(string) bool { return false }
	if len(MoveBlockers(env, "/p")) == 0 {
		t.Error("MoveBlockers doit refuser quand la vérification est impossible")
	}
}

// Échec RAPIDE d'un backend (le moteur répond « erreur » tout de suite, pas un délai) : « non vérifiable » comme un délai —
// avertissement pour l'env.yaml, refus d'emblée pour le déplacement (fail-closed dès le départ) ; ForeignOwner (ouverture) inchangé.
func TestBackendEnEchecRapideNonVerifiable216(t *testing.T) {
	unixOnly(t)
	proj := psEnv(t)
	psWrite(t, EnvPath(proj), "image: docker.io/library/debian:12\n")
	psWrite(t, BackendsPath(), "backends:\n  lab: { type: docker, host: 'tcp://10.0.0.1:2375', shared: [/share] }\n")
	bin := t.TempDir()
	psWrite(t, filepath.Join(bin, "docker"), "#!/bin/sh\necho 'cannot connect' >&2\nexit 1\n")
	_ = os.Chmod(filepath.Join(bin, "docker"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// projet LOCAL (non affecté à un backend conteneur) : jamais « non vérifiable » faute de moteur — déplacement non bloqué à tort
	if who := ForeignOwnerChecked(proj); who != "" {
		t.Fatalf("projet local : ForeignOwnerChecked = %q, attendu vide", who)
	}
	if r := runningCtr(proj); r != "" {
		t.Fatalf("projet local : runningCtr = %q, attendu vide", r)
	}
	if b := MoveBlockers(MoveEnv{OpenHere: func(string) bool { return false }}, proj); len(b) != 0 {
		t.Errorf("projet local : déplacement bloqué à tort : %v", b)
	}
	if err := WriteAssignment(proj, "lab"); err != nil { // projet sur le backend conteneur en échec
		t.Fatal(err)
	}
	if who := ForeignOwnerChecked(proj); !strings.HasPrefix(who, UnverifiablePrefix) {
		t.Fatalf("ForeignOwnerChecked = %q", who)
	}
	if who := ForeignOwner(proj); who != "" {
		t.Errorf("ForeignOwner (ouverture) doit rester muet : %q", who)
	}
	if r := runningCtr(proj); !strings.HasPrefix(r, UnverifiablePrefix) {
		t.Fatalf("runningCtr = %q", r)
	}
	env := MoveEnv{OpenHere: func(string) bool { return false }}
	if b, w := EnvEditCheck(env, proj); len(b) != 0 || len(w) != 1 || w[0] != EnvWarnUnknown {
		t.Errorf("env.yaml : blockers=%v warns=%v (avertissement attendu)", b, w)
	}
	if b := MoveBlockers(env, proj); len(b) == 0 {
		t.Error("déplacement : un backend en échec rapide doit refuser dès le départ")
	}
}

func TestEnvExecBlockersNonVerifableEstUnRefus216(t *testing.T) {
	env := MoveEnv{OpenHere: func(string) bool { return false }, Foreign: func(string) string { return "" },
		Running: func(string) string { return UnverifiablePrefix + " : x" }}
	if b := EnvEditBlockers(env, "/p"); len(b) != 0 {
		t.Errorf("env.yaml : %v", b)
	}
	b := EnvExecBlockers(env, "/p")
	if len(b) != 1 || b[0] != EnvBlockUnknown || !strings.HasPrefix(EnvExecRefusal(env, "/p"), UnverifiablePrefix) {
		t.Errorf("backend/image : %v / %q", b, EnvExecRefusal(env, "/p"))
	}
	// listing et propriétaire en parallèle : durée du plus lent, pas la somme
	slow := func(string) string { time.Sleep(300 * time.Millisecond); return "" }
	env.Foreign, env.Running = slow, slow
	t0 := time.Now()
	EnvEditCheck(env, "/p")
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Errorf("garde séquentielle : %v", d)
	}
}
