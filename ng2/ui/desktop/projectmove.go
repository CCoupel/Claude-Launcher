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
				return core.UnverifiablePrefix + " : propriétaire non vérifiable"
			}
		},
		Running: func(path string) string { // listing des moteurs : borné comme le propriétaire (un moteur qui ne répond pas ne bloque pas)
			res := make(chan string, 1)
			go func() { res <- runningOwner.Get()(path) }()
			select {
			case what := <-res:
				return what
			case <-time.After(foreignOwnerTimeout.Get()):
				return core.UnverifiablePrefix + " : conteneur non vérifiable"
			}
		},
	}
}

// projectMoveInfo : base, catégorie, nom, clé, ouvert, blockers ; pending : déplacement inachevé à reprendre ou annuler.
func (a *App) projectMoveInfo(path string, dest *core.MoveTarget) (any, error) {
	if !a.knownProject(path) && len(core.PendingMoves(path)) == 0 { // un projet déplacé à moitié peut être hors de l'arbre relu
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	info, err := core.MoveInfoDest(a.moveEnv(), path, dest)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	return info, nil
}

// moveArgs : arguments de projectMove.
type moveArgs struct {
	Path, Base, Category, Name string
	NewCategory                bool
}

// projectMove : déplace ou renomme un projet fermé ; succès : arbre relu et sélection suivie. Refus de chemin :
// {ok:false, field, msg} (règle commune de #214).
func (a *App) projectMove(in moveArgs) (any, error) {
	if !a.knownProject(in.Path) {
		return nil, fmt.Errorf("projet inconnu : %s", in.Path)
	}
	env := a.moveEnv()
	if _, err := core.ResolveProjectPathIn(env.Roots, in.Base, in.Category, in.Name); err != nil {
		return pathFail(err)
	}
	r := core.MoveProject(env, in.Path, core.MoveTarget{Base: in.Base, Category: in.Category, NewCategory: in.NewCategory, Name: in.Name})
	a.afterMove(r, "✓ projet déplacé")
	return r, nil
}

// afterMove : succès — arbre relu et sélection suivie ; sinon l'instantané est rafraîchi (blocker « déplacement inachevé »).
func (a *App) afterMove(r core.MoveResult, msg string) {
	if r.State == "partial" {
		a.rescan() // le projet peut déjà être sous sa destination
	}
	if r.OK {
		a.rescan()
		a.mu.Lock()
		a.sel = r.Path
		a.mu.Unlock()
		a.changed()
		a.setMsg(msg)
	} else if r.Msg != "" {
		a.changed()
	}
}

// projectMoveResume : « Reprendre » — rejoue les étapes restantes de l'entrée du journal id (pending.id).
func (a *App) projectMoveResume(id string) (any, error) {
	r := core.ResumeMoveID(a.moveEnv(), id)
	a.afterMove(r, "✓ déplacement repris et terminé")
	return r, nil
}

// projectMoveAbort : « Annuler » — retour arrière des étapes faites ; succès : projet à son chemin d'origine.
func (a *App) projectMoveAbort(id string) (any, error) {
	r := core.AbortMoveID(a.moveEnv(), id)
	a.afterMove(r, "↩ déplacement annulé : état initial rétabli")
	return r, nil
}
