package desktop

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// ── Rafraîchir un projet et forcer sa fermeture (#158) ────────────────────
// Contrat : contracts/desktop-dispatch.md (v6.2.0, LOT-3).

// refreshTimeout : délai global d'un rafraîchissement ; au-delà, le dernier
// évènement est émis avec ce qui n'a pas fini en erreur.
var refreshTimeout = 30 * time.Second

// Étapes, dans l'ordre de l'évènement `refresh`.
var refreshSteps = []struct{ id, label string }{
	{"ctr", "conteneur"}, {"session", "session"}, {"git", "git"},
	{"github", "GitHub"}, {"tpl", "template"}, {"usage", "consommation"},
}

// RefreshStep : une étape de l'évènement `refresh`.
type RefreshStep struct {
	ID    string `json:"id"`
	State string `json:"state"` // wait, run, ok, err
}

// RefreshEvent : avancement du rafraîchissement d'un projet.
type RefreshEvent struct {
	Path  string        `json:"path"`
	Steps []RefreshStep `json:"steps"`
	Done  bool          `json:"done"`
	Msg   string        `json:"msg"`
}

// refreshRun : état d'un rafraîchissement en cours (un seul par projet).
type refreshRun struct {
	a    *App
	path string
	mu   sync.Mutex
	st   []RefreshStep
	errs map[string]string // étape -> message
	fin  bool              // dernier évènement émis : plus rien n'est émis ensuite
}

func (r *refreshRun) emit(done bool, msg string) {
	r.mu.Lock()
	if r.fin {
		r.mu.Unlock()
		return
	}
	r.fin = done
	ev := RefreshEvent{Path: r.path, Steps: append([]RefreshStep(nil), r.st...), Done: done, Msg: msg}
	r.mu.Unlock()
	r.a.emit("refresh", ev)
}

func (r *refreshRun) set(id, state, msg string) {
	r.mu.Lock()
	for i := range r.st {
		if r.st[i].ID == id {
			r.st[i].State = state
		}
	}
	if msg != "" {
		r.errs[id] = msg
	}
	r.mu.Unlock()
	r.emit(false, "")
}

// refreshProject : lance le rafraîchissement en arrière-plan.
func (a *App) refreshProject(path string) (any, error) {
	if err := a.checkProjectArg(true, path); err != nil {
		return nil, err
	}
	if _, busy := a.refreshing.LoadOrStore(path, true); busy {
		return map[string]any{"ok": false, "msg": "rafraîchissement déjà en cours"}, nil
	}
	a.changed()
	go a.runRefresh(path)
	return map[string]any{"ok": true}, nil
}

// runRefresh : toutes les relectures du projet, hors a.mu, délai global
// refreshTimeout ; le dernier évènement est toujours émis.
func (a *App) runRefresh(path string) {
	r := &refreshRun{a: a, path: path, errs: map[string]string{}}
	for _, s := range refreshSteps {
		r.st = append(r.st, RefreshStep{ID: s.id, State: "wait"})
	}
	timedOut := false
	defer func() {
		if p := recover(); p != nil {
			r.emit(true, fmt.Sprintf("rafraîchissement interrompu : %v", p))
		}
		a.refreshing.Delete(path)
		a.changed()
		a.cardsChanged()
	}()
	r.emit(false, "")

	var wg sync.WaitGroup
	// step : exécute f en tâche ; une étape qui n'a pas fini à l'échéance reste
	// en cours dans sa goroutine (elle réécrit seulement des caches) mais est
	// rapportée en erreur.
	finished := make(chan struct{})
	step := func(id string, f func() (string, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.set(id, "run", "")
			defer func() {
				if p := recover(); p != nil {
					r.set(id, "err", fmt.Sprintf("%v", p))
				}
			}()
			if msg, err := f(); err != nil {
				r.set(id, "err", err.Error())
			} else {
				r.set(id, "ok", msg)
				r.mu.Lock()
				delete(r.errs, id) // un message d'information n'est pas une erreur
				r.mu.Unlock()
			}
		}()
	}
	// conteneur puis session : la session se lit dans la vue des conteneurs
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.set("ctr", "run", "")
		_, err := a.refreshCtr(path)
		if err != nil {
			r.set("ctr", "err", err.Error())
		} else {
			r.set("ctr", "ok", "")
		}
		r.set("session", "run", "")
		if err := a.refreshSession(path); err != nil {
			r.set("session", "err", err.Error())
		} else {
			r.set("session", "ok", "")
		}
	}()
	step("git", func() (string, error) { a.refreshGit(path); return "", nil })
	step("github", func() (string, error) {
		if err := a.fetchMilestone(path, true); err != nil {
			return "", fmt.Errorf("GitHub non relu : %v", err)
		}
		return "", nil
	})
	step("tpl", func() (string, error) { a.refreshTpl(path); return "", nil })
	step("usage", func() (string, error) { a.refreshUsage(); return "", nil })
	go func() { wg.Wait(); close(finished) }()

	select {
	case <-finished:
	case <-time.After(refreshTimeout):
		timedOut = true
		r.mu.Lock()
		for i := range r.st {
			if s := r.st[i].State; s == "run" || s == "wait" {
				r.st[i].State = "err"
				r.errs[r.st[i].ID] = "délai dépassé"
			}
		}
		r.mu.Unlock()
	}
	r.emit(true, a.refreshSummary(r, timedOut))
}

// refreshSummary : phrase de fin — erreurs d'abord, sinon l'état relu.
func (a *App) refreshSummary(r *refreshRun, timedOut bool) string {
	r.mu.Lock()
	var bad []string
	for _, s := range refreshSteps {
		if m, ok := r.errs[s.id]; ok {
			if timedOut && m == "délai dépassé" {
				bad = append(bad, s.label+" non relu : délai dépassé")
			} else {
				bad = append(bad, m)
			}
		}
	}
	r.mu.Unlock()
	if len(bad) > 0 {
		return strings.Join(bad, " · ")
	}
	a.mu.Lock()
	n, ok := a.node(r.path)
	var p core.ProjInfo
	var t *projTerm
	if ok {
		p, t = a.projInfo(n), a.terms[r.path]
	}
	a.mu.Unlock()
	if !ok {
		return "projet inconnu"
	}
	if can, why, _ := core.ForceCloseable(a.forceInput(p, t)); can {
		return why + " — fermeture forcée proposée"
	}
	return "à jour"
}

// refreshCtr : conteneur ou pod du projet, diagnostic du backend forcé ; le
// résultat remplace, pour ce projet, la vue des conteneurs.
func (a *App) refreshCtr(path string) (string, error) {
	v := core.ReadCtrViewForced([]string{path})
	be := v.Assign[path]
	a.mu.Lock()
	if a.ctr.Env == nil { // avant la première lecture
		a.ctr = core.CtrView{Env: map[string]bool{}, Assign: map[string]string{}, Found: map[string]core.Found{},
			Errs: map[string]string{}, Diag: map[string][]core.Problem{}, Bad: map[string]string{},
			Live: map[string]core.LiveSess{}, Dups: map[string][]string{}, Down: map[string]string{}}
	}
	c := &a.ctr
	c.Env[path], c.Assign[path] = v.Env[path], v.Assign[path]
	if !v.Env[path] {
		delete(c.Env, path)
	}
	if v.Assign[path] == "" {
		delete(c.Assign, path)
	}
	setOrDel := func(ok bool, set func(), del func()) {
		if ok {
			set()
		} else {
			del()
		}
	}
	f, ok := v.Found[path]
	setOrDel(ok, func() { c.Found[path] = f }, func() { delete(c.Found, path) })
	b, ok := v.Bad[path]
	setOrDel(ok, func() { c.Bad[path] = b }, func() { delete(c.Bad, path) })
	l, ok := v.Live[path]
	setOrDel(ok, func() { c.Live[path] = l }, func() { delete(c.Live, path) })
	d, ok := v.Dups[path]
	setOrDel(ok, func() { c.Dups[path] = d }, func() { delete(c.Dups, path) })
	if be != "" { // état du backend du projet
		ps, ok := v.Diag[be]
		setOrDel(ok, func() { c.Diag[be] = ps }, func() { delete(c.Diag, be) })
		e, ok := v.Errs[be]
		setOrDel(ok, func() { c.Errs[be] = e }, func() { delete(c.Errs, be) })
		dn, ok := v.Down[be]
		setOrDel(ok, func() { c.Down[be] = dn }, func() { delete(c.Down, be) })
	}
	a.mu.Unlock()
	a.storeCtrSnapshots(a.ctrCopy())
	a.changed()
	switch {
	case v.Bad[path] != "":
		return "", fmt.Errorf("configuration d'exécution invalide : %s", v.Bad[path])
	case len(v.Diag[be]) > 0:
		return "", fmt.Errorf("backend %s inutilisable : %s", be, v.Diag[be][0].What)
	case v.Errs[be] != "":
		return "", fmt.Errorf("backend %s : %s", be, v.Errs[be])
	}
	return "", nil
}

func (a *App) ctrCopy() core.CtrView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctr
}

// refreshSession : session de reprise, et onglet retiré s'il n'a plus de
// session ni de conteneur derrière lui.
func (a *App) refreshSession(path string) error {
	r, has := a.conf().PendingSession(path)
	a.mu.Lock()
	if has {
		a.sessions[path] = r
	} else {
		delete(a.sessions, path)
	}
	t := a.terms[path]
	_, hasLive := a.ctr.Live[path]
	hasEnv := a.ctr.Env[path]
	a.mu.Unlock()
	if t != nil && t.info().State != "opening" {
		gone := false
		switch {
		case t.host:
			gone = !hostSessionAlive(t.sock, t.sess)
		case hasEnv:
			gone = !hasLive
		}
		if gone {
			a.detachTab(path)
		}
	}
	a.changed()
	return nil
}

// hostSessionAlive : la session tmux locale d'un projet sur l'hôte existe.
func hostSessionAlive(sock, sess string) bool {
	c := platform.NoWindow(exec.Command("tmux", "-L", sock, "has-session", "-t", "="+sess))
	c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
	return c.Run() == nil
}

// refreshGit : git, état du template et état complet des cartes, sans cache.
func (a *App) refreshGit(path string) {
	a.gitOne(path, latestTemplate())
	ps := core.ComputeState(path)
	direct.Lock()
	direct.m[path] = ps
	direct.Unlock()
	a.cardsChanged()
}

// refreshTpl : version du template, registre des agents et team sans cache.
func (a *App) refreshTpl(path string) {
	v := core.ReadTrim(core.TplVersionPath())
	a.mu.Lock()
	a.tplVer = v
	a.mu.Unlock()
	core.ForgetTplInfo(path)
	teamCache.Lock()
	delete(teamCache.m, path)
	teamCache.Unlock()
	a.readTeam(path)
	a.changed()
}

// refreshUsage : consommation ; dans le délai minimal après un échec de l'API,
// la valeur en cache est rendue sans appel.
func (a *App) refreshUsage() {
	core.LinkUsageCache()
	core.FetchUsageForced(a.conf().UsageRefreshSec)
	u := core.ReadUsage()
	a.mu.Lock()
	a.usage = u
	a.mu.Unlock()
	a.changed()
}

// ── Fermeture forcée ──────────────────────────────────────────────────────

// forceInput : ce que décide core.ForceCloseable pour la ligne p (a.mu non
// requis pour t : seul t.info() est lu).
func (a *App) forceInput(p core.ProjInfo, t *projTerm) core.ForceInput {
	in := core.ForceInput{P: p, Tab: t != nil, K8s: a.backendIsK8s(p.Backend)}
	if t != nil && t.host {
		in.HostGone = t.info().State != "opening" && !hostSessionAlive(t.sock, t.sess)
	}
	return in
}

// backendIsK8s : le backend est de type k8s (instantané des backends, sans fichier).
func (a *App) backendIsK8s(name string) bool {
	if p := a.bkSnap.Load(); p != nil {
		for _, b := range *p {
			if b.Name == name {
				return b.K8s()
			}
		}
	}
	return false
}

// canForce : champ `canForce` de la ligne. Appelé avec a.mu tenu par Snapshot :
// pas d'appel tmux ici, seulement l'état de l'onglet.
func (a *App) canForce(path string, p core.ProjInfo) bool {
	t := a.terms[path]
	in := core.ForceInput{P: p, Tab: t != nil, K8s: a.backendIsK8s(p.Backend)}
	if t != nil && t.host {
		st := t.info().State
		in.HostGone = st == "closed" || st == "error"
	}
	ok, _, _ := core.ForceCloseable(in)
	return ok
}

// forceClose : la question de la fermeture forcée, sa raison et ce qui sera
// nettoyé ; ou un message si l'action est refusée ou sans objet.
func (a *App) forceClose(path string) (any, error) {
	if err := a.checkProjectArg(true, path); err != nil {
		return nil, err
	}
	n, p, t, busy := a.forceState(path)
	if busy {
		return map[string]any{"msg": n.Name + " : fermeture déjà en cours"}, nil
	}
	in := a.forceInput(p, t)
	ok, why, refused := core.ForceCloseable(in)
	switch {
	case refused != "":
		return map[string]any{"msg": n.Name + " : " + refused}, nil
	case !ok:
		return map[string]any{"msg": n.Name + " : rien à forcer (rafraîchir d'abord)"}, nil
	}
	a.forceAsked.Store(path, true)
	items := []any{} // []any : la même forme avant et après la sérialisation JSON
	for _, it := range core.ForceItems(in) {
		items = append(items, it)
	}
	return map[string]any{
		"question": "Forcer la fermeture de " + n.Name + " ? " + why + " · o/N",
		"why":      why,
		"items":    items,
	}, nil
}

func (a *App) forceState(path string) (n core.Node, p core.ProjInfo, t *projTerm, busy bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n, _ = a.node(path)
	_, busy = a.closing[path]
	return n, a.projInfo(n), a.terms[path], busy
}

// forceCloseConfirm : l'état est relu et la décision reprise (il a pu changer
// depuis la question), puis le nettoyage est fait, sans /exit ni attente.
func (a *App) forceCloseConfirm(path string) (any, error) {
	if err := a.checkProjectArg(true, path); err != nil {
		return nil, err
	}
	n, p, t, busy := a.forceState(path)
	if _, asked := a.forceAsked.LoadAndDelete(path); !asked {
		return map[string]any{"ok": false, "msg": n.Name + " : aucune fermeture forcée en attente de confirmation"}, nil
	}
	if busy {
		return map[string]any{"ok": false, "msg": n.Name + " : fermeture déjà en cours"}, nil
	}
	// l'état a pu changer depuis la question (la session a pu disparaître, et
	// l'onglet avec elle) : le nettoyage est idempotent et demandé par
	// l'utilisateur ; seul un autre launcher vivant l'interdit
	if _, _, refused := core.ForceCloseable(a.forceInput(p, t)); refused != "" {
		return map[string]any{"ok": false, "msg": n.Name + " : " + refused}, nil
	}
	a.mu.Lock()
	a.closing[path] = "fermeture forcée…"
	a.mu.Unlock()
	a.changed()
	notes, err := a.runForceClose(path, p, t)
	a.mu.Lock()
	delete(a.closing, path)
	a.mu.Unlock()
	go a.readCtr()
	go a.readSessions()
	a.changed()
	a.cardsChanged()
	msg := "✓ " + n.Name + " : fermeture forcée"
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, ", ") + ")"
	}
	if err != nil {
		msg += " — " + err.Error()
	}
	go a.setMsg(msg)
	return map[string]any{"ok": true, "msg": msg}, nil
}

// runForceClose : onglet retiré, session locale et panes cachés tués, marque de
// sortie propre, verrou périmé et fichier d'ordre supprimés, arrêt du conteneur
// demandé s'il existe encore (erreur rapportée, jamais bloquante). notes : ce
// qui a été fait.
func (a *App) runForceClose(path string, p core.ProjInfo, t *projTerm) (notes []string, stopErr error) {
	if t != nil {
		a.detachTab(path)
		notes = append(notes, "onglet retiré")
	}
	var local core.SessionTmux
	if !core.ContainerProject(p) { // projet sur l'hôte : tmux local dédié
		sock, sess := deskSocket(), hostSession(path)
		local = func(args ...string) (string, error) {
			c := platform.NoWindow(exec.Command("tmux", append([]string{"-L", sock}, args...)...))
			c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
			b, err := c.Output()
			return strings.TrimSpace(string(b)), err
		}
		if _, err := local("kill-session", "-t", "="+sess); err == nil {
			notes = append(notes, "session tmux locale tuée")
		}
	}
	r := core.CleanupProject(path, local, orderFilePath(path))
	if r.Hidden > 0 {
		notes = append(notes, fmt.Sprintf("%d pane(s) caché(s) tué(s)", r.Hidden))
	}
	if r.Lock {
		notes = append(notes, "verrou de création supprimé")
	}
	if be, ok := core.FindBackend(p.Backend); ok && p.CtrKnown {
		if e, ok := core.EngineOf(be); ok {
			e = e.Of(p.Ctr)
			if p.Ctr.Running { // panes cachés du conteneur
				tc := func(args ...string) (string, error) { return e.Output(p.Ctr.Name, core.TmuxIn(args...)...) }
				if n := core.KillHiddenPanes(tc, core.HomeCtr); n > 0 {
					notes = append(notes, fmt.Sprintf("%d pane(s) caché(s) du conteneur tué(s)", n))
				}
			}
			if stopErr = e.Stop(path, p.Ctr.Name); stopErr == nil {
				notes = append(notes, "arrêt du conteneur demandé")
			}
		}
	}
	return notes, stopErr
}
