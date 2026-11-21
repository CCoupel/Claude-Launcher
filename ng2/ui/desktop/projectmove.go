package desktop

import (
	"fmt"
	"time"

	"claude-launcher-ng2/core"
)

// ── Déplacer / renommer un projet (#216, contracts/desktop-dispatch.md « projectMoveInfo » / « projectMove ») ──

// moveEnv : environnement du déplacement pour cette fenêtre — chemins : bases de config.conf ; « ouvert ici » : onglet ou
// session vue ; autre launcher : propriétaire vivant (vérification bornée).
func (a *App) moveEnv() core.MoveEnv {
	return core.MoveEnv{
		Roots:    a.conf().Roots,
		OpenHere: a.projectOpen,
		Foreign: func(path string) string {
			res := make(chan string, 1)
			go func() { res <- foreignOwner.Get()(path) }()
			select {
			case who := <-res:
				return who
			case <-time.After(foreignOwnerTimeout.Get()):
				return "backend injoignable : propriétaire non vérifiable"
			}
		},
	}
}

// projectMoveInfo : base, catégorie, nom, clé, ouvert, blockers ; pending : déplacement inachevé à reprendre ou annuler.
func (a *App) projectMoveInfo(path string) (any, error) {
	if !a.knownProject(path) {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	info, err := core.MoveInfoFor(a.moveEnv(), path)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	return info, nil
}

// moveArgs : arguments de projectMove.
type moveArgs struct {
	Path, Base, Category, Name string
	NewCategory                bool
	Resume, Cancel             bool // reprise / annulation d'un déplacement inachevé (état « partial »)
}

// projectMove : déplace ou renomme un projet fermé ; succès : arbre relu et sélection suivie. Refus de chemin :
// {ok:false, field, msg} (règle commune de #214).
func (a *App) projectMove(in moveArgs) (any, error) {
	if !a.knownProject(in.Path) && !in.Resume && !in.Cancel {
		return nil, fmt.Errorf("projet inconnu : %s", in.Path)
	}
	env := a.moveEnv()
	var r core.MoveResult
	switch {
	case in.Resume:
		r = core.ResumeMove(env, in.Path)
	case in.Cancel:
		r = core.CancelMove(env, in.Path)
	default:
		if _, err := core.ResolveProjectPathIn(env.Roots, in.Base, in.Category, in.Name); err != nil {
			return pathFail(err)
		}
		r = core.MoveProject(env, in.Path, core.MoveTarget{Base: in.Base, Category: in.Category, NewCategory: in.NewCategory, Name: in.Name})
	}
	if r.OK {
		a.rescan()
		a.mu.Lock()
		a.sel = r.Path
		a.mu.Unlock()
		a.changed()
		a.setMsg("✓ projet déplacé")
	} else if r.Msg != "" {
		a.changed()
	}
	return r, nil
}
