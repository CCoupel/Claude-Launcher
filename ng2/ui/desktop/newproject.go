package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-launcher-ng2/core"
)

// ── Nouveau projet : base, catégorie, nom (#214, contracts/desktop-dispatch.md « v6.6.0 ») ──

// BaseInfo : une base (racine de config.conf) proposée à la création.
type BaseInfo struct {
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

// projectBases : racines configurées dans l'ordre de config.conf ; default = la seule base accessible, sinon la
// base du projet sélectionné dans l'arbre, sinon "".
func (a *App) projectBases() ([]BaseInfo, string) {
	roots := a.conf().Roots
	bases := make([]BaseInfo, 0, len(roots))
	var ok []string
	for _, r := range roots {
		p := filepath.Clean(r)
		st, err := os.Stat(p)
		missing := err != nil || !st.IsDir()
		bases = append(bases, BaseInfo{Path: p, Missing: missing})
		if !missing {
			ok = append(ok, p)
		}
	}
	if len(ok) == 1 {
		return bases, ok[0]
	}
	a.mu.Lock()
	sel := a.sel
	a.mu.Unlock()
	for _, b := range ok {
		if sel != "" && (sel == b || strings.HasPrefix(sel, b+string(filepath.Separator))) {
			return bases, b
		}
	}
	return bases, ""
}

// pathFail : refus de chemin sous la forme { ok:false, field, msg } ; les autres erreurs restent des erreurs.
func pathFail(err error) (any, error) {
	if pe, ok := core.AsPathError(err); ok {
		return map[string]any{"ok": false, "field": pe.Field, "msg": pe.Msg}, nil
	}
	return nil, err
}

func (a *App) projectCategories(base string) (any, error) {
	cats, err := core.ListCategoriesIn(a.conf().Roots, base)
	if err != nil {
		return pathFail(err)
	}
	return map[string]any{"categories": cats}, nil
}

func (a *App) categoryCreate(base, category string) (any, error) {
	existed, err := core.CreateCategoryIn(a.conf().Roots, base, category)
	if err != nil {
		return pathFail(err)
	}
	return map[string]any{"ok": true, "path": category, "existed": existed}, nil
}

// createArgs : arguments de `create` (contrat v6.6.0).
type createArgs struct {
	Base, Category string
	NewCategory    bool
	Name           string
	Source         string // empty (défaut) ; git et template : LOT-2.2
	Raw            bool
	Remote, URL    string
	Image, Backend string
}

// create : nouveau projet <base>/<catégorie>/<nom> (chemin contrôlé par core.ResolveProjectPathIn : base inconnue,
// `..`, absolu et lien sortant refusés, même sur appel forgé), puis ouvert. Échec : le dossier créé par cet appel
// et la catégorie qu'il a créée (vide) sont supprimés.
func (a *App) create(in createArgs) (any, error) {
	if in.Source != "" && in.Source != "empty" {
		return nil, fmt.Errorf("source %q non disponible", in.Source)
	}
	roots := a.conf().Roots
	dir, undo, err := core.CreateProjectDir(roots, in.Base, in.Category, in.NewCategory, in.Name)
	if err != nil {
		return pathFail(err)
	}
	if err := core.ApplyExec(dir, in.Image, in.Backend); err != nil {
		return nil, fmt.Errorf("env.yaml : %v — %s", err, undo())
	}
	mode := core.LaunchNewManaged
	if in.Raw {
		mode = core.LaunchNewRaw
	}
	if err := core.InitProject(dir, in.Raw, in.Remote); err != nil {
		return nil, fmt.Errorf("%v — %s", err, undo())
	}
	a.rescan()
	a.mu.Lock()
	a.sel = dir
	a.mu.Unlock()
	go a.gitOne(dir, latestTemplate())
	r, err := a.open(dir, mode) // comme le bash : le projet créé est lancé aussitôt
	if err == nil {
		a.setMsg("✓ projet créé")
	}
	return r, err
}
