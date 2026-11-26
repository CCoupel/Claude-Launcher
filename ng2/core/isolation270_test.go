//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #270 — CI ubuntu : « TempDir RemoveAll cleanup: … /.local/share/containers/storage/overlay/…/work/work: permission denied ». Un
// test de core a lancé le VRAI podman (« podman version ») sous son HOME temporaire : stockage overlay en mode 000 que RemoveAll ne
// supprime pas. Deux garanties : (1) aucun test de core n'atteint un vrai moteur de conteneurs ; (2) le nettoyage d'un HOME de test
// est robuste à des répertoires en mode 000.

// Les moteurs du PATH sont les faux (en échec immédiat) posés par TestMain ; le stockage des conteneurs est hors des dossiers de test.
func TestCoreTestsNeverReachRealContainerEngines270(t *testing.T) {
	for _, tool := range []string{"podman", "docker"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Errorf("%s introuvable : les faux moteurs ne sont pas en tête du PATH", tool)
			continue
		}
		if !strings.Contains(filepath.Base(filepath.Dir(p)), "ng2-test-engines-") {
			t.Errorf("%s = %s : un VRAI moteur est atteignable depuis les tests de core (stockage overlay sous HOME)", tool, p)
		}
		out, err := exec.Command(tool, "version", "--format", "{{.Client.Version}}").CombinedOutput()
		if err == nil || !strings.Contains(string(out), "faux moteur de test") {
			t.Errorf("%s version : %v %q, attendu l'échec immédiat du faux moteur", tool, err, out)
		}
	}
	// stockage des conteneurs : jamais sous un dossier temporaire de test (que t.TempDir doit pouvoir supprimer)
	xdg := os.Getenv("XDG_DATA_HOME")
	if xdg == "" || strings.Contains(xdg, "/Test") || strings.Contains(xdg, "/001") {
		t.Errorf("XDG_DATA_HOME = %q : doit désigner un dossier hors des dossiers temporaires de test", xdg)
	}
	// même sous un HOME de test
	withHome(t)
	if x := os.Getenv("XDG_DATA_HOME"); strings.HasPrefix(x, os.Getenv("HOME")) || x == "" {
		t.Errorf("XDG_DATA_HOME = %q sous le HOME du test %q", x, os.Getenv("HOME"))
	}
}

// Nettoyage robuste : un arbre en mode 000 (comme le stockage overlay de podman) sous le HOME d'un test ne fait pas échouer la
// suppression du dossier temporaire. Sous-test : si le nettoyage échouait, t.TempDir signalerait l'échec du sous-test.
func TestWithHomeCleanupSurvivesLockedTrees270(t *testing.T) {
	t.Run("home", func(t *testing.T) {
		h := withHome(t)
		deep := filepath.Join(h, ".local", "share", "containers", "storage", "overlay", "compat123", "work", "work")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(deep, "f"), []byte("x"), 0o000)
		for _, d := range []string{deep, filepath.Dir(deep), filepath.Dir(filepath.Dir(deep))} {
			if err := os.Chmod(d, 0o000); err != nil {
				t.Fatal(err)
			}
		}
	})
}
