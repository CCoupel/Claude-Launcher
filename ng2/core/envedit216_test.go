package core

import (
	"errors"
	"os"
	"strings"
	"testing"
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
	late := func() []string { // libre au 1er contrôle, bloqué quand on revérifie à l'écriture
		calls++
		if calls >= 2 {
			return []string{"projet ouvert par un autre launcher (x)"}
		}
		return nil
	}
	_, err := SaveConfigGuarded(ConfProjectEnv, EnvPath(proj), []byte("image: docker.io/library/debian:12\npackages: [git]\n"), st.ModTime(), late)
	var le *EnvLockedError
	if !errors.As(err, &le) || !errors.Is(err, ErrEnvLocked) || len(le.Blockers) != 1 || calls != 2 {
		t.Fatalf("err = %v (appels %d)", err, calls)
	}
	if b, _ := os.ReadFile(EnvPath(proj)); string(b) != src {
		t.Error("env.yaml écrit malgré la revérification")
	}
	if _, err := SaveConfigGuarded(ConfProjectEnv, EnvPath(proj), []byte("image: docker.io/library/debian:12\npackages: [git]\n"), st.ModTime(), func() []string { return nil }); err != nil {
		t.Fatalf("libre : %v", err)
	}
}
