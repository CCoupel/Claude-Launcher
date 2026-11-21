//go:build windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Faux positif CI Windows : le dossier temporaire du runner s'écrit C:\Users\RUNNER~1\… (8.3) d'un côté, C:\Users\runneradmin\…
// de l'autre ; la garde comparait des chaînes. Les deux formes, et une casse différente, sont le MÊME dossier.
func TestGardeIsolationNomCourt8p3(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "claude-launcher-cache-un-nom-long")
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	u, _ := syscall.UTF16PtrFromString(long)
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Skipf("pas de nom court 8.3 sur ce volume : %v", err)
	}
	short := syscall.UTF16ToString(buf[:n])
	if strings.EqualFold(short, long) {
		t.Skip("noms courts désactivés sur ce volume")
	}
	TestIsolation = true
	defer func() { TestIsolation = false }()
	for name, p := range map[string]string{"court": filepath.Join(short, "usage.json.err"), "casse": strings.ToUpper(filepath.Join(long, "usage.json.err"))} {
		if !underTemp(p) {
			t.Errorf("%s : %s refusé alors qu'il est sous le dossier temporaire", name, p)
		}
		if err := guardWrite(p); err != nil {
			t.Errorf("%s : %v", name, err)
		}
	}
	ResetIsolationViolations()
}
