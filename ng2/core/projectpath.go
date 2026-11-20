package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// ── Chemins de projet contrôlés (#214, contracts/desktop-dispatch.md « Règle commune ») ──
//
// Toute base, catégorie ou nom reçu (interface, appel forgé) repasse par ResolveProjectPath : aucun chemin n'est
// produit hors des bases configurées (`..`, chemin absolu, `\`, lien symbolique sortant).

// PathError : refus d'un chemin, avec le champ fautif (« base », « category », « name ») et la raison affichée sous
// ce champ.
type PathError struct {
	Field string
	Msg   string
}

func (e *PathError) Error() string { return e.Msg }

func pathErr(field, msg string) error { return &PathError{Field: field, Msg: msg} }

// AsPathError : l'erreur est un refus de chemin.
func AsPathError(err error) (*PathError, bool) {
	var pe *PathError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

// ResolveProjectPath : <base>/<category>/<name> d'après LoadConfig().Roots ; voir ResolveProjectPathIn.
func ResolveProjectPath(base, category, name string) (string, error) {
	return ResolveProjectPathIn(LoadConfig().Roots, base, category, name)
}

// ResolveProjectPathIn : comme ResolveProjectPath, avec les racines roots. Rien n'est écrit sur le disque.
func ResolveProjectPathIn(roots []string, base, category, name string) (string, error) {
	if why := ValidProjectName(name); why != "" {
		if err := checkBase(roots, base); err != nil {
			return "", err
		}
		return "", pathErr("name", why)
	}
	if strings.Contains(name, "..") {
		return "", pathErr("name", "nom invalide (..)")
	}
	return resolveIn(roots, base, category, strings.TrimSpace(name))
}

// ResolveCategoryPathIn : <base>/<category> (catégorie non vide), mêmes contrôles.
func ResolveCategoryPathIn(roots []string, base, category string) (string, error) {
	if strings.TrimSpace(category) == "" {
		if err := checkBase(roots, base); err != nil {
			return "", err
		}
		return "", pathErr("category", "catégorie requise")
	}
	return resolveIn(roots, base, category, "")
}

// checkBase : base égale (après Clean) à l'une des racines, et accessible.
func checkBase(roots []string, base string) error {
	if strings.TrimSpace(base) == "" {
		return pathErr("base", "base inconnue")
	}
	b := filepath.Clean(base)
	known := false
	for _, r := range roots {
		if r != "" && filepath.Clean(r) == b {
			known = true
			break
		}
	}
	if !known {
		return pathErr("base", "base inconnue")
	}
	if st, err := os.Stat(b); err != nil || !st.IsDir() {
		return pathErr("base", "base inaccessible")
	}
	return nil
}

// checkCategory : segments d'une catégorie relative (« A/B ») ; rend les segments nettoyés.
func checkCategory(category string) ([]string, error) {
	if category == "" {
		return nil, nil
	}
	if strings.ContainsAny(category, `\`) || strings.HasPrefix(category, "/") || filepath.IsAbs(category) ||
		filepath.VolumeName(category) != "" {
		return nil, pathErr("category", "catégorie invalide (chemin relatif, sans \\)")
	}
	segs := strings.Split(category, "/")
	if len(segs) > maxScanDepth {
		return nil, pathErr("category", "catégorie trop profonde")
	}
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		t := strings.TrimSpace(s)
		switch {
		case t == "":
			return nil, pathErr("category", "nom de catégorie vide")
		case t == "." || t == "..":
			return nil, pathErr("category", "catégorie invalide (. ou ..)")
		case strings.HasPrefix(t, "."):
			return nil, pathErr("category", "catégorie invalide (pas de . au début)")
		case strings.IndexFunc(t, unicode.IsControl) >= 0:
			return nil, pathErr("category", "catégorie invalide (caractère de contrôle)")
		}
		out = append(out, t)
	}
	return out, nil
}

func resolveIn(roots []string, base, category, name string) (string, error) {
	if err := checkBase(roots, base); err != nil {
		return "", err
	}
	segs, err := checkCategory(category)
	if err != nil {
		return "", err
	}
	b := filepath.Clean(base)
	p := filepath.Join(append([]string{b}, segs...)...)
	if name != "" {
		p = filepath.Join(p, name)
	}
	// contrôle final : le parent existant le plus profond, liens résolus, reste sous la base résolue
	realBase, err := filepath.EvalSymlinks(b)
	if err != nil {
		return "", pathErr("base", "base inaccessible")
	}
	deep := p
	for {
		if _, err := os.Lstat(deep); err == nil {
			break
		}
		up := filepath.Dir(deep)
		if up == deep {
			break
		}
		deep = up
	}
	realDeep, err := filepath.EvalSymlinks(deep)
	if err != nil {
		return "", pathErr(outField(category, name), "chemin hors des bases")
	}
	if rel, err := filepath.Rel(realBase, realDeep); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", pathErr(outField(category, name), "chemin hors des bases")
	}
	return p, nil
}

func outField(category, name string) string {
	if category != "" || name == "" {
		return "category"
	}
	return "name"
}

// Category : une catégorie (dossier qui n'est pas un projet) d'une base.
type Category struct {
	Path     string `json:"path"` // relatif à la base, « / »
	Projects int    `json:"projects"`
}

// ListCategories : sous-dossiers réels de base qui ne sont pas des projets, récursivement jusqu'à maxScanDepth,
// dossiers cachés exclus, triés ; une catégorie vide est listée. Base invalide : erreur de la règle commune.
func ListCategories(base string) ([]Category, error) {
	return ListCategoriesIn(LoadConfig().Roots, base)
}

// ListCategoriesIn : ListCategories avec les racines roots.
func ListCategoriesIn(roots []string, base string) ([]Category, error) {
	if err := checkBase(roots, base); err != nil {
		return nil, err
	}
	out := []Category{}
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") { // liens symboliques non suivis : e.IsDir() faux
				continue
			}
			p := filepath.Join(dir, e.Name())
			if isProjectDir(p) {
				continue
			}
			r := e.Name()
			if rel != "" {
				r = rel + "/" + e.Name()
			}
			out = append(out, Category{Path: r, Projects: countProjects(p)})
			if depth+1 < maxScanDepth {
				walk(p, r, depth+1)
			}
		}
	}
	walk(filepath.Clean(base), "", 0)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// countProjects : projets directement sous dir.
func countProjects(dir string) int {
	n := 0
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && isProjectDir(filepath.Join(dir, e.Name())) {
			n++
		}
	}
	return n
}

// CreateCategory : crée <base>/<category> (MkdirAll 0755) après la règle commune ; une catégorie existante n'est pas
// une erreur (existed : fusion) ; un fichier de ce nom : refus sur le champ « category ».
func CreateCategory(base, category string) (existed bool, err error) {
	return CreateCategoryIn(LoadConfig().Roots, base, category)
}

// CreateCategoryIn : CreateCategory avec les racines roots.
func CreateCategoryIn(roots []string, base, category string) (existed bool, err error) {
	p, err := ResolveCategoryPathIn(roots, base, category)
	if err != nil {
		return false, err
	}
	if st, serr := os.Stat(p); serr == nil {
		if st.IsDir() {
			return true, nil
		}
		return false, pathErr("category", "un fichier porte déjà ce nom")
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return false, pathErr("category", err.Error())
	}
	return false, nil
}
