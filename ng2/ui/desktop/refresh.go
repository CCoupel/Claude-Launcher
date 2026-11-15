package desktop

import (
	"fmt"
	"maps"
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
	a     *App
	path  string
	mu    sync.Mutex
	st    []RefreshStep
	errs  map[string]string // étape -> message
	notes []string          // étapes « sans objet » : la raison, sans erreur rouge
	fin   bool              // dernier évènement émis : plus rien n'est émis ensuite
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
		defer func() {
			if p := recover(); p != nil {
				r.set("ctr", "err", fmt.Sprintf("%v", p))
				r.set("session", "err", "état du conteneur inconnu : onglet conservé")
			}
		}()
		r.set("ctr", "run", "")
		reliable, err := a.refreshCtr(path)
		if err != nil {
			r.set("ctr", "err", err.Error())
		} else {
			r.set("ctr", "ok", "")
		}
		r.set("session", "run", "")
		if err := a.refreshSession(path, reliable); err != nil {
			r.set("session", "err", err.Error())
		} else {
			r.set("session", "ok", "")
		}
	}()
	step("git", func() (string, error) { a.refreshGit(path); return "", nil })
	step("github", func() (string, error) {
		if err := a.fetchMilestone(path, true); err != nil {
			if _, benign := core.GHKind(err); benign { // gh absent ou non connecté, dépôt hors GitHub : sans objet
				r.mu.Lock()
				r.notes = append(r.notes, "GitHub : "+err.Error())
				r.mu.Unlock()
				return "", nil
			}
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
	notes := append([]string(nil), r.notes...)
	r.mu.Unlock()
	if len(bad) > 0 {
		return strings.Join(bad, " · ")
	}
	if len(notes) > 0 {
		return strings.Join(notes, " · ")
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
func (a *App) refreshCtr(path string) (bool, error) {
	v := core.ReadCtrViewForced([]string{path})
	be := v.Assign[path]
	// copie à l'écriture : les maps de a.ctr sont partagées avec des lecteurs hors
	// verrou (readCtr / storeCtrSnapshots itèrent la vue qu'ils viennent de
	// publier) ; une vue publiée n'est donc jamais modifiée en place
	a.mu.Lock()
	c := cloneCtrView(a.ctr)
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
	a.ctr = c
	a.mu.Unlock()
	a.storeCtrSnapshots(a.ctrCopy())
	a.changed()
	bad := core.CtrUnreliable(be, v.Errs, v.Down, v.Diag)
	switch {
	case v.Bad[path] != "":
		return bad == "", fmt.Errorf("configuration d'exécution invalide : %s", v.Bad[path])
	case bad != "":
		return false, fmt.Errorf("état du conteneur inconnu — %s", bad)
	}
	return true, nil
}

// cloneCtrView : copie de chaque map de la vue (valeurs copiées telles quelles :
// tranches et structures ne sont jamais modifiées en place).
func cloneCtrView(v core.CtrView) core.CtrView {
	return core.CtrView{
		Env: maps.Clone(orEmpty(v.Env)), Assign: maps.Clone(orEmpty(v.Assign)), Found: maps.Clone(orEmpty(v.Found)),
		Errs: maps.Clone(orEmpty(v.Errs)), Diag: maps.Clone(orEmpty(v.Diag)), Bad: maps.Clone(orEmpty(v.Bad)),
		Live: maps.Clone(orEmpty(v.Live)), Dups: maps.Clone(orEmpty(v.Dups)), Down: maps.Clone(orEmpty(v.Down)),
	}
}

func orEmpty[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return m
}

func (a *App) ctrCopy() core.CtrView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctr
}

// refreshSession : session de reprise, et onglet retiré s'il n'a plus de
// session ni de conteneur derrière lui.
func (a *App) refreshSession(path string, reliable bool) error {
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
	if t != nil && !reliable && core.ContainerProject(core.ProjInfo{HasEnv: hasEnv, Backend: a.ctrBackend(path)}) {
		a.changed()
		return fmt.Errorf("état du conteneur inconnu : onglet conservé")
	}
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

// ctrBackend : backend affecté au projet, d'après la dernière lecture.
func (a *App) ctrBackend(path string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctr.Assign[path]
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
	a.mu.Lock()
	in.Unreliable, in.NotRead = a.ctrUnreliableLocked(p.Backend), a.ctr.Env == nil
	a.mu.Unlock()
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

// ctrUnreliableLocked : la dernière lecture des conteneurs n'est pas fiable (a.mu
// tenu) : jamais lue, ou backend en défaut, en erreur ou serveurs injoignables.
func (a *App) ctrUnreliableLocked(be string) string {
	return core.CtrUnreliable(be, a.ctr.Errs, a.ctr.Down, a.ctr.Diag)
}

// canForce : champ `canForce` de la ligne. Appelé avec a.mu tenu par Snapshot :
// pas d'appel tmux ici, seulement l'état de l'onglet.
func (a *App) canForce(path string, p core.ProjInfo) bool {
	t := a.terms[path]
	in := core.ForceInput{P: p, Tab: t != nil, K8s: a.backendIsK8s(p.Backend),
		Unreliable: a.ctrUnreliableLocked(p.Backend), NotRead: a.ctr.Env == nil}
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
	a.sweepForceAsked()
	a.forceAsked.Store(path, forceAsk{at: time.Now(), ctrRunning: p.CtrKnown && p.Ctr.Running})
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

// forceAsk : état au moment de la question de fermeture forcée.
type forceAsk struct {
	at         time.Time
	ctrRunning bool // le conteneur tournait déjà à la question
}

// forceAskTTL : au-delà, la question est tenue pour abandonnée (boîte annulée).
const forceAskTTL = 2 * time.Minute

// sweepForceAsked : oublie les questions abandonnées (la boîte annulée ne le dit pas).
func (a *App) sweepForceAsked() {
	a.forceAsked.Range(func(k, v any) bool {
		if f, ok := v.(forceAsk); !ok || time.Since(f.at) > forceAskTTL {
			a.forceAsked.Delete(k)
		}
		return true
	})
}

func (a *App) forceState(path string) (n core.Node, p core.ProjInfo, t *projTerm, busy bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n, _ = a.node(path)
	_, busy = a.closing[path]
	return n, a.projInfo(n), a.terms[path], busy
}

// forceCloseConfirm : juste avant d'agir, l'état est relu à la source et la
// décision reprise — le projet a pu revivre depuis la question (session locale
// ou de conteneur de nouveau vivante, conteneur de nouveau en marche) ou la
// lecture du backend échouer : rien n'est alors tué ni arrêté.
func (a *App) forceCloseConfirm(path string) (any, error) {
	if err := a.checkProjectArg(true, path); err != nil {
		return nil, err
	}
	n, _, _, busy := a.forceState(path)
	v, asked := a.forceAsked.LoadAndDelete(path)
	ask, _ := v.(forceAsk)
	if !asked || time.Since(ask.at) > forceAskTTL {
		return map[string]any{"ok": false, "msg": n.Name + " : aucune fermeture forcée en attente de confirmation"}, nil
	}
	if busy {
		return map[string]any{"ok": false, "msg": n.Name + " : fermeture déjà en cours"}, nil
	}
	refuse := func(why string) (any, error) {
		return map[string]any{"ok": false, "msg": n.Name + " : " + why + " — rien n'a été fermé"}, nil
	}
	_, p0, _, _ := a.forceState(path)
	if core.ContainerProject(p0) { // projet sur l'hôte : pas de conteneur à relire
		_, _ = a.refreshCtr(path) // lecture non aboutie : nettoyage local seulement (ci-dessous)
	}
	n, p, t, _ := a.forceState(path)
	if !core.ContainerProject(p) && hostSessionAlive(deskSocket(), hostSession(path)) {
		return refuse("sa session tmux locale est de nouveau vivante")
	}
	if p.CtrKnown && p.Ctr.Running && !ask.ctrRunning {
		return refuse("son conteneur est de nouveau en marche")
	}
	in := a.forceInput(p, t)
	ok, _, refused := core.ForceCloseable(in)
	switch {
	case refused != "":
		return refuse(refused)
	case !ok && p.HasLive:
		return refuse("une session est de nouveau vivante")
	case !ok:
		return refuse("le projet n'est plus à forcer")
	}
	a.mu.Lock()
	a.closing[path] = "fermeture forcée…"
	a.mu.Unlock()
	a.changed()
	notes, err := a.runForceClose(path, p, t, in.Unreliable != "" || len(p.Problems) > 0)
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
func (a *App) runForceClose(path string, p core.ProjInfo, t *projTerm, localOnly bool) (notes []string, stopErr error) {
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
	if localOnly { // backend injoignable : rien n'est tué ni arrêté de son côté
		return append(notes, "état local seulement (backend injoignable)"), nil
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
			if p.Ctr.Running && !p.Ctr.Failed { // en marche et sain : jamais arrêté d'ici
				notes = append(notes, "conteneur en marche laissé tel quel")
			} else if stopErr = e.Stop(path, p.Ctr.Name); stopErr == nil {
				notes = append(notes, "arrêt du conteneur demandé")
			}
		}
	}
	return notes, stopErr
}
