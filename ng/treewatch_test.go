package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTreeDirs(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"grp/proj", "grp/vide", "seul"} {
		_ = os.MkdirAll(filepath.Join(base, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(base, "grp/proj/README"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "seul/README"), []byte("x"), 0o644)
	// racine et groupes (dont le dossier vide, futur projet), jamais l'intérieur d'un projet
	want := []string{base, filepath.Join(base, "grp"), filepath.Join(base, "grp/vide")}
	if got := treeDirs([]string{base}); !reflect.DeepEqual(got, want) {
		t.Errorf("treeDirs=%v, attendu %v", got, want)
	}
	sig := dirsSig(want)
	time.Sleep(20 * time.Millisecond)
	_ = os.Remove(filepath.Join(base, "grp/vide"))
	if dirsSig(want) == sig {
		t.Error("suppression non vue par dirsSig")
	}
}

// signal reçu dans le délai, ou échec
func waitSignal(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(treePollEvery + 2*time.Second):
		t.Fatalf("%s : aucun signal", what)
	}
}

func TestWatchTree(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "grp/proj"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "grp/proj/README"), []byte("x"), 0o644)
	c := watchTree([]string{base})
	time.Sleep(100 * time.Millisecond)

	_ = os.Mkdir(filepath.Join(base, "grp/neuf"), 0o755) // nouveau dossier dans un groupe
	waitSignal(t, c, "création")
	_ = os.WriteFile(filepath.Join(base, "grp/neuf/README"), []byte("x"), 0o644) // il devient projet
	waitSignal(t, c, "dossier devenu projet")
	_ = os.RemoveAll(filepath.Join(base, "grp/proj"))
	waitSignal(t, c, "suppression")

	// l'intérieur d'un projet n'est pas surveillé
	_ = os.WriteFile(filepath.Join(base, "grp/neuf/autre"), []byte("x"), 0o644)
	select {
	case <-c:
		t.Error("signal pour un fichier dans un projet")
	case <-time.After(time.Second):
	}
}
