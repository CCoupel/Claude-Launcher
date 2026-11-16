package core

import (
	"path/filepath"
	"runtime"
	"testing"
)

// #165 revue M2 : une barre oblique finale ne change pas la racine de projets (bash et Go donnent le même chemin).
func TestRootBaseTrailingSlashSameProjects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script bash")
	}
	root := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv(RootBaseVar, root+"/")
	if got := LoadConfig().Roots; len(got) != 1 || got[0] != filepath.Join(root, "PROJETS") {
		t.Errorf("racines avec barre finale : %v, attendu [%s]", got, filepath.Join(root, "PROJETS"))
	}
	if d, _ := RootBase(); d != root {
		t.Errorf("RootBase() = %q, attendu %q", d, root)
	}
}
