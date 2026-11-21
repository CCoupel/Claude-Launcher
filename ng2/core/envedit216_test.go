package core

import (
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
	for _, w := range []string{"projet ouvert dans ce launcher", "autre launcher (pod-x)", "conteneur ou pod en cours d'exécution (c sur b)", "fermez le projet pour modifier son environnement"} {
		if !strings.Contains(m, w) {
			t.Errorf("message sans %q : %s", w, m)
		}
	}
	free := MoveEnv{OpenHere: func(string) bool { return false }, Foreign: func(string) string { return "" }, Running: func(string) string { return "" }}
	if m := EnvEditRefusal(free, "/p"); m != "" {
		t.Errorf("projet fermé : refus %q", m)
	}
}
