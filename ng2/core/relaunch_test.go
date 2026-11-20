//go:build linux

package core

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// procfs factice : /proc/<pid>/{cmdline,environ,exe}.
func fakeProc(t *testing.T, root string, pid int, exe string, argv []string, env []string) {
	t.Helper()
	d := filepath.Join(root, strconv.Itoa(pid))
	_ = os.MkdirAll(d, 0o755)
	var cl, ev []byte
	for _, a := range argv {
		cl = append(append(cl, a...), 0)
	}
	for _, e := range env {
		ev = append(append(ev, e...), 0)
	}
	_ = os.WriteFile(filepath.Join(d, "cmdline"), cl, 0o644)
	_ = os.WriteFile(filepath.Join(d, "environ"), ev, 0o644)
	if err := os.Symlink(exe, filepath.Join(d, "exe")); err != nil {
		t.Fatal(err)
	}
}

func TestStaleProcsMemeServeurSeulement(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "claude-launcher-ng2")
	old := filepath.Join(dir, "old", "claude-launcher-ng2")
	_ = os.MkdirAll(filepath.Dir(old), 0o755)
	_ = os.WriteFile(cur, []byte("v2"), 0o755)
	_ = os.WriteFile(old, []byte("v1"), 0o755)
	oldExe, oldRoot := launcherExe, procRoot
	launcherExe = func() (string, error) { return cur, nil }
	procRoot = filepath.Join(dir, "proc")
	defer func() { launcherExe, procRoot = oldExe, oldRoot }()
	t.Setenv("CL_INNER_SOCKET", "sock-a")
	bin := "claude-launcher-ng2"
	fakeProc(t, procRoot, 100, old, []string{bin, "mirror", "@1", "p"}, []string{"CL_INNER_SOCKET=sock-a"})
	fakeProc(t, procRoot, 101, old, []string{bin, "layout-watch", "@1", "%1", "/p"}, []string{"CL_INNER_SOCKET=sock-a"})
	fakeProc(t, procRoot, 102, cur, []string{bin, "mirror", "@2", "q"}, []string{"CL_INNER_SOCKET=sock-a"}) // déjà à jour
	fakeProc(t, procRoot, 103, old, []string{bin, "mirror", "@3", "r"}, []string{"CL_INNER_SOCKET=sock-b"}) // autre serveur
	fakeProc(t, procRoot, 104, old, []string{bin, "nav", "--launch"}, []string{"CL_INNER_SOCKET=sock-a"})   // pas auxiliaire
	fakeProc(t, procRoot, 105, old, []string{"autre", "mirror", "x"}, []string{"CL_INNER_SOCKET=sock-a"})   // autre programme
	got := StaleProcs()
	if len(got) != 2 || got[0] != (StaleProc{"mirror", 100}) || got[1] != (StaleProc{"layout-watch", 101}) {
		t.Fatalf("StaleProcs = %v", got)
	}
}

func TestInstalledVersionEtBinChanged(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ng")
	_ = os.WriteFile(exe, []byte("a"), 0o755)
	oldExe, oldCmd := launcherExe, installedVersionCmd
	launcherExe = func() (string, error) { return exe, nil }
	installedVersionCmd = func(_ context.Context, _ string) ([]byte, error) { return []byte("claude-launcher-ng2 v9.9.9\n"), nil }
	startStampOnce, startStamp = sync.Once{}, nil
	defer func() {
		launcherExe, installedVersionCmd = oldExe, oldCmd
		startStampOnce, startStamp = sync.Once{}, nil
	}()
	MarkLauncherStart()
	if binChanged() {
		t.Error("binChanged avant remplacement")
	}
	if v := installedVersion(); v != "v9.9.9" {
		t.Errorf("version = %q", v)
	}
	_ = os.Remove(exe)
	_ = os.WriteFile(exe, []byte("bb"), 0o755)
	if !binChanged() {
		t.Error("binChanged après remplacement attendu")
	}
}
