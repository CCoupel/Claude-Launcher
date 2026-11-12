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
	c := WatchTree([]string{base})
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

// Montage perdu puis rétabli (MooseFS tombé) : la racine disparaît, l'arbre la
// signale « inaccessible », et elle revient seule au remontage — sans Ctrl+R ni
// relance (avant : plus surveillée du tout, navigateur vide jusqu'à la relance).
func TestWatchTreeRootLostAndBack(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "PROJETS")
	away := filepath.Join(parent, "PROJETS.demonte")
	_ = os.MkdirAll(filepath.Join(root, "cat/proj"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "cat/proj/README"), []byte("x"), 0o644)
	c := WatchTree([]string{root})
	time.Sleep(100 * time.Millisecond)

	if err := os.Rename(root, away); err != nil { // le dossier disparaît
		t.Fatal(err)
	}
	waitSignal(t, c, "racine disparue")
	nodes := ScanTree([]string{root})
	if len(nodes) != 1 || !nodes[0].Root || !nodes[0].Missing {
		t.Fatalf("racine perdue : %+v, attendu un en-tête « inaccessible »", nodes)
	}

	if err := os.Rename(away, root); err != nil { // remontage
		t.Fatal(err)
	}
	waitSignal(t, c, "racine revenue")
	nodes = ScanTree([]string{root})
	if len(nodes) != 3 || nodes[0].Missing || nodes[2].Name != "proj" {
		t.Fatalf("racine revenue : %+v", nodes)
	}

	// et elle reste surveillée ensuite
	_ = os.Mkdir(filepath.Join(root, "cat/neuf"), 0o755)
	waitSignal(t, c, "création après remontage")
}

func TestDirReachable(t *testing.T) {
	d := t.TempDir()
	if !dirReachable(d) {
		t.Error("dossier vide vu injoignable")
	}
	if dirReachable(filepath.Join(d, "absent")) {
		t.Error("dossier absent vu joignable")
	}
	f := filepath.Join(d, "fichier")
	_ = os.WriteFile(f, nil, 0o644)
	if dirReachable(f) {
		t.Error("fichier vu comme dossier joignable")
	}
}
