package testguard

import (
	"os"
	"path/filepath"
	"runtime"
)

// FakeEngines : dossier de faux « podman » et « docker » qui échouent immédiatement (« cannot connect »), placé en TÊTE du
// PATH des suites ui/desktop et ui/tmux par défaut. Sans lui, un poste sans moteur et le runner CI (qui détecte un vrai
// docker / podman et en fait un backend, backends.yaml absent) n'ont pas le même comportement : local = CI.
// Un test qui veut un moteur pose le sien en tête du PATH (t.Setenv) ; un test qui veut « aucun backend conteneur » écrit un
// backends.yaml vide (voir noCtrBackends dans chaque suite). Sous Windows : sans objet (moteurs locaux refusés), nil.
// Rend le dossier à supprimer en fin de suite ("" si rien posé).
func FakeEngines() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	d, err := os.MkdirTemp("", "ng2-test-engines-")
	if err != nil {
		return ""
	}
	for _, n := range []string{"podman", "docker"} {
		_ = os.WriteFile(filepath.Join(d, n), []byte("#!/bin/sh\necho 'Cannot connect to the container engine (faux moteur de test)' >&2\nexit 1\n"), 0o755)
	}
	os.Setenv("PATH", d+string(os.PathListSeparator)+os.Getenv("PATH"))
	return d
}
