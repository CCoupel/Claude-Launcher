package core

// #214 : chemins de projet contrôlés (contracts/desktop-dispatch.md « Règle commune — ResolveProjectPath »,
// « projectCategories », « categoryCreate »). Dossiers factices sous t.TempDir() uniquement.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// base214 : une base factice contenant un dossier-projet « solo » (.git) pour qu'elle ressemble à une racine.
func base214(t *testing.T) (roots []string, base string) {
	t.Helper()
	base = filepath.Join(t.TempDir(), "PROJETS")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{base}, base
}

func lsAll214(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error { out = append(out, p); return nil })
	return out
}

func wantPathErr214(t *testing.T, err error, field string) {
	t.Helper()
	pe, ok := AsPathError(err)
	if !ok {
		t.Fatalf("erreur = %v, attendu un refus de chemin (champ %q)", err, field)
	}
	if pe.Field != field {
		t.Errorf("champ = %q, attendu %q (%s)", pe.Field, field, pe.Msg)
	}
	if strings.TrimSpace(pe.Msg) == "" {
		t.Error("message vide : rien à afficher sous le champ")
	}
}

func TestResolveProjectPathNominalSansEcriture214(t *testing.T) {
	roots, base := base214(t)
	avant := lsAll214(t, base)
	for _, c := range []struct{ cat, name, want string }{
		{"INFRA", "supervision", filepath.Join(base, "INFRA", "supervision")},
		{"", "direct", filepath.Join(base, "direct")},
		{"PRO/CLIENT_A", "app", filepath.Join(base, "PRO", "CLIENT_A", "app")},
		{" INFRA ", " x ", filepath.Join(base, "INFRA", "x")}, // blancs de bord retirés
	} {
		got, err := ResolveProjectPathIn(roots, base, c.cat, c.name)
		if err != nil || got != c.want {
			t.Errorf("(%q,%q) = %q, %v ; attendu %q", c.cat, c.name, got, err, c.want)
		}
	}
	if apres := lsAll214(t, base); len(apres) != len(avant) {
		t.Errorf("la résolution a écrit sur le disque : %v", apres)
	}
	// base avec « / » final ou « . » : égale après Clean
	if _, err := ResolveProjectPathIn(roots, base+"/", "", "x"); err != nil {
		t.Errorf("base avec / final refusée : %v", err)
	}
	if _, err := ResolveProjectPathIn(roots, base+"/./", "", "x"); err != nil {
		t.Errorf("base avec ./ refusée : %v", err)
	}
}

func TestResolveProjectPathViaConfigRoots214(t *testing.T) {
	_, base := base214(t)
	t.Setenv("CL_ROOTS", base)
	got, err := ResolveProjectPath(base, "INFRA", "x")
	if err != nil || got != filepath.Join(base, "INFRA", "x") {
		t.Errorf("= %q, %v", got, err)
	}
	if _, err := ResolveProjectPath(filepath.Join(base, "INFRA"), "", "x"); err == nil {
		t.Error("sous-dossier d'une base accepté comme base")
	}
}

func TestResolveProjectPathBaseRefusee214(t *testing.T) {
	roots, base := base214(t)
	_ = os.MkdirAll(filepath.Join(base, "INFRA"), 0o755)
	for _, b := range []string{"", "   ", "/", "/etc", "/tmp", filepath.Join(base, "INFRA"), filepath.Join(base, ".."), base + "x", "relatif/PROJETS"} {
		_, err := ResolveProjectPathIn(roots, b, "", "x")
		wantPathErr214(t, err, "base")
		if pe, ok := AsPathError(err); ok && pe.Msg != "base inconnue" {
			t.Errorf("base %q : message %q, attendu « base inconnue »", b, pe.Msg)
		}
	}
	// racine configurée mais absente du disque (montage perdu)
	perdue := filepath.Join(t.TempDir(), "montage-perdu")
	_, err := ResolveProjectPathIn([]string{perdue}, perdue, "", "x")
	wantPathErr214(t, err, "base")
	if pe, _ := AsPathError(err); pe != nil && pe.Msg != "base inaccessible" {
		t.Errorf("message %q, attendu « base inaccessible »", pe.Msg)
	}
	// aucune racine configurée
	_, err = ResolveProjectPathIn(nil, base, "", "x")
	wantPathErr214(t, err, "base")
}

func TestResolveProjectPathCategorieTraversee214(t *testing.T) {
	roots, base := base214(t)
	avant := lsAll214(t, base)
	bad := []string{
		"..", "../x", "a/../b", "a/..", "../../etc", ".", "./a", "a/.",
		"/abs", "/etc/passwd", filepath.Join(base, "INFRA"), // absolus
		`a\b`, `..\x`, `\a`, `C:\x`, // séparateur Windows
		".cache", "a/.hidden", // cachés
		"a//b", "a/", "/", " ", "a/ /b", // segments vides
		"a\x01b", "a/b\x7f", "x\ny", // caractères de contrôle
		"a/b/c/d/e/f/g/h/i", // plus profond que maxScanDepth (8)
	}
	for _, c := range bad {
		p, err := ResolveProjectPathIn(roots, base, c, "x")
		if err == nil {
			t.Errorf("catégorie %q acceptée → %q", c, p)
			continue
		}
		wantPathErr214(t, err, "category")
	}
	if apres := lsAll214(t, base); len(apres) != len(avant) {
		t.Errorf("écriture disque malgré le refus : %v", apres)
	}
	// profondeur maximale acceptée
	if _, err := ResolveProjectPathIn(roots, base, "a/b/c/d/e/f/g/h", "x"); err != nil {
		t.Errorf("profondeur %d refusée : %v", maxScanDepth, err)
	}
}

func TestResolveProjectPathNomRefuse214(t *testing.T) {
	roots, base := base214(t)
	for _, n := range []string{"", "  ", "..", "a..b", "x/y", `x\y`, ".cache", "../x", "a/../b"} {
		p, err := ResolveProjectPathIn(roots, base, "INFRA", n)
		if err == nil {
			t.Errorf("nom %q accepté → %q", n, p)
			continue
		}
		wantPathErr214(t, err, "name")
	}
	// un nom qui ressemble à un nom de fichier est accepté
	for _, n := range []string{"projet-1", "mon projet", "a.b", "ÉTÉ"} {
		if _, err := ResolveProjectPathIn(roots, base, "INFRA", n); err != nil {
			t.Errorf("nom %q refusé : %v", n, err)
		}
	}
}

func TestResolveProjectPathLienSymboliqueSortant214(t *testing.T) {
	roots, base := base214(t)
	dehors := t.TempDir()
	if err := os.Symlink(dehors, filepath.Join(base, "SORTIE")); err != nil {
		t.Skipf("liens symboliques indisponibles : %v", err)
	}
	// lien RELATIF qui remonte hors de la base, vers un dossier de ce test (jamais vers /tmp lui-même : le résultat de
	// « REMONTE/x » dépendrait alors des fichiers qui s'y trouvent, ex. un /tmp/x existant)
	ailleurs := t.TempDir()
	rel, err := filepath.Rel(base, ailleurs)
	if err != nil || !strings.HasPrefix(rel, "..") {
		t.Fatalf("chemin relatif vers %s : %q %v", ailleurs, rel, err)
	}
	if err := os.Symlink(rel, filepath.Join(base, "REMONTE")); err != nil {
		t.Fatal(err)
	}
	// catégorie sous un lien sortant, existante ou non
	for _, c := range []string{"SORTIE", "SORTIE/sous", "REMONTE", "REMONTE/etc"} {
		p, err := ResolveProjectPathIn(roots, base, c, "x")
		if err == nil {
			t.Errorf("catégorie %q (lien sortant) acceptée → %q", c, p)
			continue
		}
		wantPathErr214(t, err, "category")
		if pe, _ := AsPathError(err); pe != nil && pe.Msg != "chemin hors des bases" {
			t.Errorf("%q : message %q, attendu « chemin hors des bases »", c, pe.Msg)
		}
	}
	// nom = lien sortant déjà posé (directement sous la base, puis dans une catégorie)
	_ = os.MkdirAll(filepath.Join(base, "INFRA"), 0o755)
	_ = os.Symlink(dehors, filepath.Join(base, "piege"))
	_ = os.Symlink(dehors, filepath.Join(base, "INFRA", "piege"))
	if p, err := ResolveProjectPathIn(roots, base, "", "piege"); err == nil {
		t.Errorf("nom portant un lien sortant accepté → %q", p)
	} else {
		wantPathErr214(t, err, "name")
	}
	if p, err := ResolveProjectPathIn(roots, base, "INFRA", "piege"); err == nil { // champ « name » ou « category » : refus suffit
		t.Errorf("nom portant un lien sortant (en catégorie) accepté → %q", p)
	} else if _, ok := AsPathError(err); !ok {
		t.Errorf("erreur non typée : %v", err)
	}
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors de la base : %v", es)
	}
	// un lien qui reste sous la base est accepté
	_ = os.MkdirAll(filepath.Join(base, "REEL"), 0o755)
	if err := os.Symlink(filepath.Join(base, "REEL"), filepath.Join(base, "ALIAS")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectPathIn(roots, base, "ALIAS", "x"); err != nil {
		t.Errorf("lien interne à la base refusé : %v", err)
	}
	// la base elle-même peut être un lien (racine sur un montage) : comparée résolue des deux côtés
	reel := t.TempDir()
	lien := filepath.Join(t.TempDir(), "BASE")
	if err := os.Symlink(reel, lien); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectPathIn([]string{lien}, lien, "INFRA", "x"); err != nil {
		t.Errorf("base portée par un lien refusée : %v", err)
	}
}

// ── catégories ────────────────────────────────────────────────────────────

func mkProj214(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(p, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestListCategories214(t *testing.T) {
	roots, base := base214(t)
	mkProj214(t, filepath.Join(base, "INFRA", "p1"))
	mkProj214(t, filepath.Join(base, "INFRA", "p2"))
	_ = os.MkdirAll(filepath.Join(base, "VIDE"), 0o755)                    // catégorie vide : listée, 0 projet
	_ = os.MkdirAll(filepath.Join(base, "PRO", "CLIENT_A"), 0o755)         // imbriquée vide
	mkProj214(t, filepath.Join(base, "PRO", "CLIENT_B", "app"))            // imbriquée avec un projet
	mkProj214(t, filepath.Join(base, "solo"))                              // un projet n'est pas une catégorie
	_ = os.MkdirAll(filepath.Join(base, ".cache", "x"), 0o755)             // caché : exclu, avec son contenu
	_ = os.MkdirAll(filepath.Join(base, "INFRA", ".trash"), 0o755)         // caché imbriqué : exclu
	_ = os.WriteFile(filepath.Join(base, "notes.txt"), []byte("x"), 0o644) // fichier : ignoré
	_ = os.MkdirAll(filepath.Join(base, "docs"), 0o755)                    // dossier avec un fichier = projet (isProjectDir)
	_ = os.WriteFile(filepath.Join(base, "docs", "a.md"), []byte("x"), 0o644)
	if err := os.Symlink(t.TempDir(), filepath.Join(base, "LIEN")); err != nil { // lien : non suivi
		t.Logf("liens indisponibles : %v", err)
	}

	cats, err := ListCategoriesIn(roots, base)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	var order []string
	for _, c := range cats {
		got[c.Path] = c.Projects
		order = append(order, c.Path)
	}
	want := map[string]int{"INFRA": 2, "VIDE": 0, "PRO": 0, "PRO/CLIENT_A": 0, "PRO/CLIENT_B": 1}
	if len(got) != len(want) {
		t.Errorf("catégories = %v, attendu %v", got, want)
	}
	for p, n := range want {
		if v, ok := got[p]; !ok || v != n {
			t.Errorf("catégorie %q : %d (présente=%v), attendu %d", p, v, ok, n)
		}
	}
	for _, bad := range []string{"solo", "docs", ".cache", "INFRA/p1", "INFRA/.trash", "notes.txt", "LIEN"} {
		if _, ok := got[bad]; ok {
			t.Errorf("%q listé à tort", bad)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] > order[i] {
			t.Errorf("non trié : %v", order)
		}
	}
	// cats jamais nil (sérialisé en [] et non null)
	roots2, base2 := base214(t)
	if cats, err := ListCategoriesIn(roots2, base2); err != nil || cats == nil || len(cats) != 0 {
		t.Errorf("base sans catégorie : %v, %v (liste vide non nil attendue)", cats, err)
	}
}

func TestListCategoriesBaseInvalide214(t *testing.T) {
	roots, base := base214(t)
	for _, b := range []string{"", "/etc", filepath.Join(base, "x")} {
		_, err := ListCategoriesIn(roots, b)
		wantPathErr214(t, err, "base")
	}
}

func TestListCategoriesProfondeurBornee214(t *testing.T) {
	roots, base := base214(t)
	p := base
	for i := 0; i < maxScanDepth+3; i++ {
		p = filepath.Join(p, "n")
	}
	_ = os.MkdirAll(p, 0o755)
	cats, err := ListCategoriesIn(roots, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if n := strings.Count(c.Path, "/") + 1; n > maxScanDepth {
			t.Errorf("catégorie %q de profondeur %d > %d", c.Path, n, maxScanDepth)
		}
	}
}

func TestCreateCategoryFusionEtRefus214(t *testing.T) {
	roots, base := base214(t)
	existed, err := CreateCategoryIn(roots, base, "PRO/CLIENT_A")
	if err != nil || existed {
		t.Fatalf("1re création = %v, %v", existed, err)
	}
	st, err := os.Stat(filepath.Join(base, "PRO", "CLIENT_A"))
	if err != nil || !st.IsDir() {
		t.Fatalf("dossier non créé : %v", err)
	}
	if runtimeNotWindows214() && st.Mode().Perm() != 0o755 {
		t.Errorf("droits %v, attendu 0755", st.Mode().Perm())
	}
	// doublon (même nom, même casse) = fusion, pas une erreur ; le contenu reste
	_ = os.WriteFile(filepath.Join(base, "PRO", "CLIENT_A", "garde"), []byte("x"), 0o644)
	existed, err = CreateCategoryIn(roots, base, "PRO/CLIENT_A")
	if err != nil || !existed {
		t.Errorf("2e création = %v, %v, attendu (true, nil)", existed, err)
	}
	if _, err := os.Stat(filepath.Join(base, "PRO", "CLIENT_A", "garde")); err != nil {
		t.Error("contenu de la catégorie touché par la fusion")
	}
	// un fichier de ce nom
	_ = os.WriteFile(filepath.Join(base, "FICHIER"), []byte("x"), 0o644)
	_, err = CreateCategoryIn(roots, base, "FICHIER")
	wantPathErr214(t, err, "category")
	// refus : rien n'est créé
	avant := lsAll214(t, base)
	for _, c := range []string{"", "..", "../x", "/abs", `a\b`, ".h", "a//b"} {
		if _, err := CreateCategoryIn(roots, base, c); err == nil {
			t.Errorf("catégorie %q acceptée", c)
		} else {
			wantPathErr214(t, err, "category")
		}
	}
	if _, err := CreateCategoryIn(roots, filepath.Join(base, "INFRA"), "x"); err == nil {
		t.Error("base non configurée acceptée")
	} else {
		wantPathErr214(t, err, "base")
	}
	if apres := lsAll214(t, base); len(apres) != len(avant) {
		t.Errorf("écriture malgré le refus : %v", apres)
	}
	// lien sortant
	dehors := t.TempDir()
	if err := os.Symlink(dehors, filepath.Join(base, "SORTIE")); err == nil {
		if _, err := CreateCategoryIn(roots, base, "SORTIE/x"); err == nil {
			t.Error("création sous un lien sortant acceptée")
		}
		if es, _ := os.ReadDir(dehors); len(es) != 0 {
			t.Errorf("écriture hors de la base : %v", es)
		}
	}
}

func runtimeNotWindows214() bool { return os.PathSeparator == '/' }
