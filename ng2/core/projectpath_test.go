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

func TestNomsPortablesRefuses(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	for _, n := range []string{"a:b", "a*", `a"b`, "a<b", "a|b", "a?", "CON", "con", "Nul.txt", "COM1", "COM0", "lpt9.log", "LPT0",
		"CONIN$", "conout$", "COM¹", "LPT²", "com³.txt", "fin.", "AUX "} {
		if _, err := ResolveProjectPathIn(roots, base, "", n); err == nil {
			t.Errorf("nom %q accepté", n)
		} else if pe, _ := AsPathError(err); pe == nil || pe.Field != "name" {
			t.Errorf("nom %q : champ %v", n, err)
		}
		if n == "AUX " { // espace final : retiré par le nettoyage des segments de catégorie
			continue
		}
		if _, err := ResolveProjectPathIn(roots, base, n, "p"); err == nil { // catégorie NOUVELLE
			t.Errorf("catégorie nouvelle %q acceptée", n)
		} else if pe, _ := AsPathError(err); pe == nil || pe.Field != "category" {
			t.Errorf("catégorie %q : champ %v", n, err)
		}
	}
	for _, n := range []string{"COM10", "console", "ok-1", "a.b", "CON2", "COM", "COM¹²"} {
		if _, err := ResolveProjectPathIn(roots, base, "", n); err != nil {
			t.Errorf("nom %q refusé : %v", n, err)
		}
	}
}

// Une catégorie EXISTANTE aux caractères non portables reste utilisable (listée ET acceptée) ; une nouvelle est refusée.
func TestCategorieExistanteNonPortableAcceptee(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	for _, n := range []string{"a:b", "CON", "fin."} {
		if err := os.MkdirAll(filepath.Join(base, n, "sous"), 0o755); err != nil {
			t.Skip("système de fichiers : ", err)
		}
		if _, err := ResolveProjectPathIn(roots, base, n, "p"); err != nil {
			t.Errorf("catégorie existante %q refusée : %v", n, err)
		}
		if _, err := ResolveProjectPathIn(roots, base, n+"/sous", "p"); err != nil {
			t.Errorf("sous-catégorie de %q refusée : %v", n, err)
		}
		if _, err := ResolveProjectPathIn(roots, base, n+"/neuve:x", "p"); err == nil {
			t.Errorf("segment nouveau non portable sous %q accepté", n)
		}
		if ex, err := CreateCategoryIn(roots, base, n); err != nil || !ex {
			t.Errorf("CreateCategory(%q) : existed=%v err=%v", n, ex, err)
		}
		cats, _ := ListCategoriesIn(roots, base)
		found := false
		for _, c := range cats {
			found = found || c.Path == n
		}
		if !found {
			t.Errorf("catégorie existante %q non listée : %v", n, cats)
		}
	}
	// les contrôles de sécurité restent : lien sortant et caché refusés même existants
	_ = os.MkdirAll(filepath.Join(base, ".cache"), 0o755)
	if _, err := ResolveProjectPathIn(roots, base, ".cache", "p"); err == nil {
		t.Error("catégorie cachée acceptée")
	}
}

func TestLienSortantSurLeNomChampName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	base, dehors := t.TempDir(), t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "cat"), 0o755)
	_ = os.Symlink(dehors, filepath.Join(base, "cat", "p"))
	_, err := ResolveProjectPathIn([]string{base}, base, "cat", "p")
	if pe, ok := AsPathError(err); !ok || pe.Field != "name" || pe.Msg != "chemin hors des bases" {
		t.Errorf("err = %v", err)
	}
}

// Dossier existant : refusé, jamais supprimé ni vidé (pas de nettoyage de ce qui n'est pas à nous).
func TestCreateProjectDirExistantIntact(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	_ = os.MkdirAll(filepath.Join(base, "cat", "p"), 0o755)
	marque := filepath.Join(base, "cat", "p", "travail.txt")
	_ = os.WriteFile(marque, []byte("a moi"), 0o644)
	_, undo, err := CreateProjectDir(roots, base, "cat", false, "p")
	if pe, ok := AsPathError(err); !ok || pe.Field != "name" || undo != nil {
		t.Fatalf("err = %v undo=%v", err, undo != nil)
	}
	if b, _ := os.ReadFile(marque); string(b) != "a moi" {
		t.Error("contenu du projet existant touché")
	}
	// catégorie inexistante sans newCategory : refus, rien créé
	if _, _, err := CreateProjectDir(roots, base, "nouvelle", false, "q"); err == nil {
		t.Error("catégorie inexistante acceptée sans newCategory")
	}
	if _, serr := os.Stat(filepath.Join(base, "nouvelle")); serr == nil {
		t.Error("catégorie créée malgré le refus")
	}
}

// Deux créations simultanées du même projet : une seule réussit, l'autre est refusée sans rien supprimer ; l'undo du
// gagnant ne retire que ce qu'il a créé.
func TestCreateProjectDirConcurrent(t *testing.T) {
	base := t.TempDir()
	roots := []string{base}
	type res struct {
		dir  string
		undo func() string
		err  error
	}
	ch := make(chan res, 8)
	for i := 0; i < 8; i++ {
		go func() {
			d, u, e := CreateProjectDir(roots, base, "NEUVE/SOUS", true, "proj")
			ch <- res{d, u, e}
		}()
	}
	var win res
	n := 0
	for i := 0; i < 8; i++ {
		r := <-ch
		if r.err == nil {
			n++
			win = r
		}
	}
	if n != 1 {
		t.Fatalf("%d créations réussies, attendu 1", n)
	}
	_ = os.WriteFile(filepath.Join(win.dir, "f"), nil, 0o644)
	win.undo()
	if _, err := os.Stat(win.dir); err == nil {
		t.Error("dossier du gagnant non supprimé par undo")
	}
	// la catégorie peut rester (créée par un perdant dont le dossier du gagnant l'empêchait de la retirer) : le retrait
	// des catégories créées par l'appel est vérifié en séquentiel (TestCreateProjectDirUndoGardeLesCategoriesPreexistantes)
}

func TestCreateProjectDirUndoGardeLesCategoriesPreexistantes(t *testing.T) {
	base := t.TempDir()
	_ = os.MkdirAll(filepath.Join(base, "A"), 0o755)
	d, undo, err := CreateProjectDir([]string{base}, base, "A/B", true, "p")
	if err != nil {
		t.Fatal(err)
	}
	undo()
	if _, err := os.Stat(d); err == nil {
		t.Error("projet non supprimé")
	}
	if _, err := os.Stat(filepath.Join(base, "A")); err != nil {
		t.Error("catégorie préexistante supprimée")
	}
	if _, err := os.Stat(filepath.Join(base, "A", "B")); err == nil {
		t.Error("catégorie créée par l'appel non retirée")
	}
}
