package core

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
)

// Garde : sous `go test`, le binaire de test n'est jamais relancé avec « version » (il rejouerait toute la suite).
func TestVersionInstalleeNeLancePasLeBinaireDeTest(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	var n int32
	old, oldExe := installedVersionCmd, launcherExe
	installedVersionCmd = func(context.Context, string) ([]byte, error) { atomic.AddInt32(&n, 1); return []byte("x v1"), nil }
	launcherExe = func() (string, error) { return self, nil }
	defer func() { installedVersionCmd, launcherExe = old, oldExe }()
	if v := installedVersion(); v != "" {
		t.Errorf("installedVersion = %q, attendu vide pour le binaire de test", v)
	}
	_ = LauncherStateFor("desktop")
	if atomic.LoadInt32(&n) != 0 {
		t.Errorf("%d exécution(s) du binaire de test", n)
	}
}
