package core

import (
	"path/filepath"
	"runtime"
	"testing"
)

// unixOnly : test d'un comportement propre à Linux/macOS (espaces de PID,
// droits Unix, liens symboliques, faux moteurs en scripts sh, backend host…),
// sauté sur le runner Windows de la CI (#112).
func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("propre à Linux/macOS")
	}
}

// winHome : sous Windows, os.UserHomeDir, os.UserConfigDir et os.UserCacheDir
// lisent USERPROFILE, APPDATA et LOCALAPPDATA (pas HOME) ; les dossiers sont
// placés comme sous Linux (~/.config, ~/.cache) pour que les tests s'appliquent.
func winHome(t *testing.T, h string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	t.Setenv("USERPROFILE", h)
	t.Setenv("APPDATA", filepath.Join(h, ".config"))
	t.Setenv("LOCALAPPDATA", filepath.Join(h, ".cache"))
}
