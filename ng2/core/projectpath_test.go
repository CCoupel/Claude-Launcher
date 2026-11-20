package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveProjectPathRefus(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	for _, c := range []struct{ base, cat, name, field string }{
		{base + "/autre", "", "p", "base"},
		{"", "", "p", "base"},
		{base, "../x", "p", "category"},
		{base, "a/../b", "p", "category"},
		{base, "/etc", "p", "category"},
		{base, `a\b`, "p", "category"},
		{base, ".caché", "p", "category"},
		{base, "a//b", "p", "category"},
		{base, "a\x01b", "p", "category"},
		{base, "", "../p", "name"},
		{base, "", "a/b", "name"},
		{base, "", "..", "name"},
		{base, "", "", "name"},
	} {
		_, err := ResolveProjectPathIn(roots, c.base, c.cat, c.name)
		pe, ok := AsPathError(err)
		if !ok || pe.Field != c.field {
			t.Errorf("%+v : erreur %v, champ attendu %s", c, err, c.field)
		}
	}
	if p, err := ResolveProjectPathIn(roots, base, "INFRA/K8S", "sup"); err != nil || p != filepath.Join(base, "INFRA", "K8S", "sup") {
		t.Errorf("chemin valide refusé : %q %v", p, err)
	}
}

func TestResolveProjectPathLienSortant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	base, dehors := t.TempDir(), t.TempDir()
	if err := os.Symlink(dehors, filepath.Join(base, "lien")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveProjectPathIn([]string{base}, base, "lien", "p")
	if pe, ok := AsPathError(err); !ok || pe.Msg != "chemin hors des bases" {
		t.Errorf("lien sortant accepté : %v", err)
	}
	if _, err := CreateCategoryIn([]string{base}, base, "lien/neuve"); err == nil {
		t.Error("catégorie créée à travers un lien sortant")
	}
	if _, serr := os.Stat(filepath.Join(dehors, "neuve")); serr == nil {
		t.Error("dossier créé hors de la base")
	}
}

func TestListEtCreateCategory(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	_ = os.MkdirAll(filepath.Join(base, "INFRA", "p1", ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(base, "PRO", "CLIENT_A"), 0o755) // catégorie vide
	_ = os.MkdirAll(filepath.Join(base, ".cache", "x"), 0o755)     // caché
	_ = os.WriteFile(filepath.Join(base, "INFRA", "p1", "a.txt"), nil, 0o644)
	cats, err := ListCategoriesIn(roots, base)
	if err != nil {
		t.Fatal(err)
	}
	want := []Category{{"INFRA", 1}, {"PRO", 0}, {"PRO/CLIENT_A", 0}}
	if len(cats) != len(want) {
		t.Fatalf("catégories = %v", cats)
	}
	for i := range want {
		if cats[i] != want[i] {
			t.Errorf("cats[%d] = %v, attendu %v", i, cats[i], want[i])
		}
	}
	if ex, err := CreateCategoryIn(roots, base, "INFRA"); err != nil || !ex {
		t.Errorf("catégorie existante : existed=%v err=%v", ex, err)
	}
	if ex, err := CreateCategoryIn(roots, base, "NEUVE/SOUS"); err != nil || ex {
		t.Errorf("création : existed=%v err=%v", ex, err)
	}
	_ = os.WriteFile(filepath.Join(base, "fichier"), nil, 0o644)
	if _, err := CreateCategoryIn(roots, base, "fichier"); err == nil {
		t.Error("catégorie sur un fichier acceptée")
	}
}
