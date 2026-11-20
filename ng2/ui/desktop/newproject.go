package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/tun"
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
	Source         string // empty (défaut) | git | template
	Raw            bool
	Remote, URL    string
	Image, Backend string
}

// NewJob : création asynchrone en cours ou terminée (source « git ») ; champ `newJob` de l'instantané.
type NewJob struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Step  string `json:"step"`
	Pct   int    `json:"pct"`
	State string `json:"state"` // running, done, failed
	Msg   string `json:"msg,omitempty"`
}

// newJobHold : durée pendant laquelle une création terminée reste dans l'instantané.
var newJobHold = tun.Of(30 * time.Second)

type createJob struct {
	NewJob
	cancel context.CancelFunc
}

// newJobSnapLocked : copie de l'état de la création (verrou propre, indépendant de a.mu).
func (a *App) newJobSnapLocked() *NewJob {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	if a.job == nil {
		return nil
	}
	j := a.job.NewJob
	return &j
}

func (a *App) setJob(fn func(j *createJob)) {
	a.jobMu.Lock()
	if a.job != nil {
		fn(a.job)
	}
	a.jobMu.Unlock()
	a.changed()
}

// create : nouveau projet <base>/<catégorie>/<nom> (chemin contrôlé par core.CreateProjectDir : base inconnue, `..`,
// absolu et lien sortant refusés, même sur appel forgé), puis ouvert. Échec : le dossier créé par cet appel et les
// catégories qu'il a créées (vides) sont supprimés. Source « git » : asynchrone ({job}), voir cloneJob.
func (a *App) create(in createArgs) (any, error) {
	source := in.Source
	if source == "" {
		source = "empty"
	}
	switch source {
	case "empty", "template", "git":
	default:
		return nil, fmt.Errorf("source inconnue : %q", in.Source)
	}
	roots := a.conf().Roots
	if source == "git" {
		if _, err := core.ValidateCloneURL(in.URL); err != nil {
			return map[string]any{"ok": false, "field": "url", "msg": err.Error()}, nil
		}
		a.jobMu.Lock()
		busy := a.job != nil && a.job.State == "running"
		a.jobMu.Unlock()
		if busy {
			return map[string]any{"ok": false, "msg": "création en cours"}, nil
		}
	}
	dir, undo, err := core.CreateProjectDir(roots, in.Base, in.Category, in.NewCategory, in.Name)
	if err != nil {
		return pathFail(err)
	}
	if source == "git" {
		return a.startCloneJob(in, dir, undo)
	}
	if err := core.ApplyExec(dir, in.Image, in.Backend); err != nil {
		return nil, fmt.Errorf("env.yaml : %v — %s", err, undo())
	}
	mode := core.LaunchNormal // ni /init-project d'office, ni question : brut seulement pour le projet sans git
	if in.Raw && source == "empty" {
		mode = core.LaunchNewRaw
	}
	if err := core.InitProject(dir, in.Raw && source == "empty", in.Remote); err != nil {
		return nil, fmt.Errorf("%v — %s", err, undo())
	}
	if source == "template" {
		if err := core.MarkInitProposed(dir); err != nil {
			return nil, fmt.Errorf("marqueur /init-project : %v — %s", err, undo())
		}
	}
	return a.finishCreate(dir, mode)
}

// finishCreate : arbre relu, projet sélectionné et ouvert.
func (a *App) finishCreate(dir string, mode core.LaunchMode) (any, error) {
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

// startCloneJob : clonage en tâche de fond ; progression dans l'instantané (newJob), annulable (createCancel).
func (a *App) startCloneJob(in createArgs, dir string, undo func() string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	id := newToken()
	a.jobMu.Lock()
	a.job = &createJob{NewJob: NewJob{ID: id, Name: filepath.Base(dir), Step: "préparation", State: "running"}, cancel: cancel}
	a.jobMu.Unlock()
	a.changed()
	a.bgWG.Add(1)
	go func() {
		defer a.bgWG.Done()
		defer cancel()
		fail := func(msg string) {
			note := undo()
			a.setJob(func(j *createJob) { j.State, j.Msg = "failed", msg+" — "+note })
			a.holdJob(id)
		}
		auth := core.CloneAuth{Token: a.conf().GithubToken, Backend: in.Backend}
		err := core.CloneProject(ctx, in.URL, dir, auth, func(step string, pct int) {
			a.setJob(func(j *createJob) { j.Step, j.Pct = step, pct })
		})
		switch {
		case errors.Is(err, context.Canceled):
			fail("annulée")
		case err != nil:
			fail(err.Error())
		default:
			if err := core.ApplyExec(dir, in.Image, in.Backend); err != nil {
				fail("env.yaml : " + err.Error())
				return
			}
			a.setJob(func(j *createJob) { j.State, j.Step, j.Pct, j.Msg = "done", "terminé", 100, "" })
			a.holdJob(id)
			_, _ = a.finishCreate(dir, core.LaunchNormal)
		}
	}()
	return map[string]any{"job": id}, nil
}

// holdJob : l'état final reste lisible newJobHold, puis disparaît de l'instantané.
func (a *App) holdJob(id string) {
	time.AfterFunc(newJobHold.Get(), func() {
		a.jobMu.Lock()
		if a.job != nil && a.job.ID == id && a.job.State != "running" {
			a.job = nil
		}
		a.jobMu.Unlock()
		a.changed()
	})
}

// createCancel : arrête le clonage (le nettoyage est celui d'un échec) ; sur une création terminée : retire son état.
func (a *App) createCancel(id string) (any, error) {
	a.jobMu.Lock()
	j := a.job
	a.jobMu.Unlock()
	if j == nil || j.ID != id {
		return map[string]any{"ok": false, "msg": "aucune création"}, nil
	}
	if j.State == "running" {
		j.cancel()
		return map[string]any{"ok": true}, nil
	}
	a.jobMu.Lock()
	a.job = nil
	a.jobMu.Unlock()
	a.changed()
	return map[string]any{"ok": true}, nil
}

// initProposalAnswer : réponse à la proposition /init-project d'un projet créé depuis le template. run : la commande
// est tapée dans le leader et le marqueur retiré ; sinon la proposition n'est plus faite avant le prochain lancement.
func (a *App) initProposalAnswer(path string, run bool) (any, error) {
	if !core.InitProposalPending(path) {
		return map[string]any{"ok": false, "msg": "aucune proposition en attente"}, nil
	}
	a.proposedMu.Lock()
	a.proposed[path] = true
	a.proposedMu.Unlock()
	if !run {
		return map[string]any{"ok": true}, nil
	}
	t := a.term(path)
	if t == nil {
		return map[string]any{"ok": false, "msg": "projet non ouvert"}, nil
	}
	t.mu.Lock()
	leader := t.leader
	t.mu.Unlock()
	if why := t.input(leader, "/init-project\r"); why != "" {
		return map[string]any{"ok": false, "msg": "saisie refusée : " + why}, nil
	}
	core.ClearInitProposal(path)
	return map[string]any{"ok": true}, nil
}

// proposeInit : à l'ouverture (état « ready ») d'un projet qui attend sa proposition, évènement initProposal — une
// fois par processus et par projet.
func (a *App) proposeInit(path string) {
	if !core.InitProposalPending(path) {
		return
	}
	a.proposedMu.Lock()
	done := a.proposed[path]
	a.proposed[path] = true
	a.proposedMu.Unlock()
	if !done {
		a.emit("initProposal", map[string]any{"path": path})
	}
}
