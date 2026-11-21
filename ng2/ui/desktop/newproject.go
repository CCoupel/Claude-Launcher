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
	Raw            bool   // obsolète : ignoré (l'interface ne l'émet plus)
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
	ctx    context.Context
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

// setJob : modifie le job identifié par id (jamais un autre job qui l'aurait remplacé).
func (a *App) setJob(id string, fn func(j *createJob)) {
	a.jobMu.Lock()
	if a.job != nil && a.job.ID == id {
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
	if source != "git" {
		if why := core.ValidateRemote(in.Remote); why != "" { // remote facultatif : refusé avant toute écriture
			return map[string]any{"ok": false, "field": "remote", "msg": why}, nil
		}
	}
	roots := a.conf().Roots
	var job *createJob // source git : place réservée AVANT toute écriture, test et pose sous un seul verrou (jamais deux clones)
	if source == "git" {
		if _, err := core.ValidateCloneURL(in.URL); err != nil {
			return map[string]any{"ok": false, "field": "url", "msg": err.Error()}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		job = &createJob{NewJob: NewJob{ID: newToken(), Name: strings.TrimSpace(in.Name), Step: "préparation", State: "running"}, cancel: cancel, ctx: ctx}
		a.jobMu.Lock()
		if a.job != nil && a.job.State == "running" {
			a.jobMu.Unlock()
			cancel()
			return map[string]any{"ok": false, "msg": "création en cours"}, nil
		}
		a.job = job
		a.jobMu.Unlock()
		a.changed()
	}
	releaseJob := func() { // refus avant le clonage : la réservation est rendue
		if job != nil {
			job.cancel()
			a.jobMu.Lock()
			if a.job == job {
				a.job = nil
			}
			a.jobMu.Unlock()
			a.changed()
		}
	}
	dir, undo, err := core.CreateProjectDir(roots, in.Base, in.Category, in.NewCategory, in.Name)
	if err != nil {
		releaseJob()
		return pathFail(err)
	}
	if source == "git" {
		return a.startCloneJob(in, dir, undo, job)
	}
	if err := core.ApplyExec(dir, in.Image, in.Backend); err != nil {
		return nil, fmt.Errorf("env.yaml : %v — %s", err, undo())
	}
	// Projet brut (empty) et Template projet : dossier + git init, remote lié seulement s'il est fourni. Jamais de
	// /init-project d'office ni de CLAUDE.md minimal : `raw` (obsolète) est ignoré (#215).
	mode := core.LaunchNormal
	if err := core.InitProject(dir, false, in.Remote); err != nil {
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

// startCloneJob : clonage en tâche de fond de la place réservée par create ; progression dans l'instantané (newJob),
// annulable (createCancel).
func (a *App) startCloneJob(in createArgs, dir string, undo func() string, job *createJob) (any, error) {
	id, ctx, cancel := job.ID, job.ctx, job.cancel
	a.setJob(id, func(j *createJob) { j.Name = filepath.Base(dir) })
	a.bgWG.Add(1)
	go func() {
		defer a.bgWG.Done()
		defer cancel()
		fail := func(msg string) {
			note := undo()
			a.setJob(id, func(j *createJob) { j.State, j.Msg = "failed", msg+" — "+note })
			a.holdJob(id)
		}
		// entrée d'approbation posée AVANT le clonage : un clone interrompu ne devient jamais de confiance (retirée par undo)
		if err := core.RegisterClone(dir, in.URL, in.Image); err != nil {
			fail("registre d'approbation : " + err.Error())
			return
		}
		auth := core.CloneAuth{Token: a.conf().GithubToken, Backend: in.Backend}
		err := core.CloneProject(ctx, in.URL, dir, auth, func(step string, pct int) {
			a.setJob(id, func(j *createJob) { j.Step, j.Pct = step, pct })
		})
		switch {
		case errors.Is(err, context.Canceled):
			fail("annulée")
		case err != nil:
			fail(err.Error())
		default:
			// l'état (none / pending) est décidé sur le contenu du DÉPÔT seul, AVANT ApplyExec : l'env.yaml qu'ApplyExec écrit
			// ensuite est celui du launcher (empreinte re-scellée), jamais un « pending » sur son propre fichier (#215)
			if _, err := core.MarkEnvPending(dir, in.URL, in.Image); err != nil {
				fail("registre d'approbation : " + err.Error())
				return
			}
			if err := core.ApplyExec(dir, in.Image, in.Backend); err != nil {
				fail("env.yaml : " + err.Error())
				return
			}
			// « terminé » seulement si le projet s'ouvre ; sinon échec visible (le dépôt cloné reste en place)
			if _, err := a.finishCreate(dir, core.LaunchNormal); err != nil {
				a.setJob(id, func(j *createJob) { j.State, j.Msg = "failed", "cloné, mais ouverture impossible : "+err.Error() })
			} else {
				a.setJob(id, func(j *createJob) { j.State, j.Step, j.Pct, j.Msg = "done", "terminé", 100, "" })
			}
			a.holdJob(id)
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
	a.mu.Lock()
	n, known := a.node(path)
	a.mu.Unlock()
	if !known || !n.Project || !core.InitProposalPending(path) { // un projet connu, portant le marqueur : rien d'autre
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
	leader := ""
	if l := t.lastLayout(); l != nil {
		leader = l.Leader // le leader est celui de la dernière mise en page publiée
	}
	if leader == "" {
		return map[string]any{"ok": false, "msg": "le leader n'est pas encore prêt : réessayer dans un instant"}, nil // marqueur gardé
	}
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

// envTrustInfo : la question d'approbation de l'environnement d'un projet cloné.
func (a *App) envTrustInfo(path string) (any, error) {
	if !a.knownProject(path) {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	info, err := core.EnvTrustInfoFor(path)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	return info, nil
}

// envTrustAnswer : accept (définitif) puis ouverture ; ignore (socle choisi seul) puis ouverture ; cancel (rien).
func (a *App) envTrustAnswer(path, choice, sha string) (any, error) {
	if !a.knownProject(path) {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	retry, err := core.AnswerEnvTrust(path, choice, sha)
	switch {
	case retry:
		return map[string]any{"ok": false, "msg": err.Error(), "retry": true}, nil
	case err != nil:
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	case choice == "cancel":
		return map[string]any{"ok": true}, nil
	}
	return a.open(path, core.LaunchNormal)
}

// envTrustReview : « Revoir » des réglages — l'entrée repasse à pending (projet fermé seulement).
func (a *App) envTrustReview(path string) map[string]any {
	if a.projectOpen(path) {
		return map[string]any{"ok": false, "msg": "projet ouvert : le fermer avant de revoir son environnement"}
	}
	if err := core.ReviewEnvTrust(path); err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}
	}
	return map[string]any{"ok": true}
}
