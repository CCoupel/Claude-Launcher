// Package desktop : interface native du launcher (v6, #111) — fenêtre Wails
// (WebView2 sous Windows) ou page locale du navigateur (mode web, tests), avec
// la même parité fonctionnelle que la coque tmux (ui/tmux) : arbre des projets
// et ses icônes, cartes ◈ État, annonces de mise à jour, consommation, un
// onglet par projet ouvert dont les panes (leader, teammates) sont des
// terminaux xterm.js branchés sur le tmux du projet en mode contrôle, onglet
// ⌗ Teamleaders.
//
// Toute la logique vient de core (légende, mise en page des teams, ouverture
// en conteneur, script de démarrage, fermeture, mises à jour) ; ce package ne
// fait que l'orchestrer et l'exposer à l'interface (Dispatch, événements).
package desktop

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
)

// Emitter : envoi d'un événement à l'interface (Wails ou mode web).
type Emitter func(name string, data any)

// App : état du launcher vu par l'interface.
type App struct {
	cfg    core.Config
	confMu sync.RWMutex // cfg relue à chaud (config.go)
	emit   Emitter

	mu         sync.Mutex
	nodes      []core.Node
	git        map[string]core.GitInfo
	tpl        map[string]core.TemplateState
	detail     map[string]core.GitDetail
	sessions   map[string]core.Resumable
	ctr        core.CtrView
	milestones map[string]core.MsState
	usage      core.Usage
	tplVer     string
	sel        string // projet sélectionné
	msg        string
	closing    map[string]string    // projet -> étape de sa fermeture en cours
	plans      map[string]closePlan // fermetures en attente de confirmation
	terms      map[string]*projTerm
	fromCache  map[string]bool // projets dont l'état vient d'un cache (git en tâche de fond inutile)
	order      []string        // onglets, dans l'ordre d'ouverture
	started    time.Time
	loadedAt   time.Duration
	mgr        *core.StateMgr
	upd        updState
	perf       perfLog
	treeCached bool     // arbre repris du lancement précédent (relu en tâche de fond)
	hubCache   string   // cache d'état du hub Linux (hubs.<machine>.hub_cache)
	hubWarns   []string // clés inconnues de hubs: dans backends.yaml (#127), ignorées

	// instantanés lus sans a.mu par les méthodes de la fenêtre Réglages (#118) :
	// un rafraîchissement lent (conteneurs, partage réseau) ne doit pas les retarder
	projSet   atomic.Pointer[map[string]bool] // projets de l'arbre (chemins), lisible sans a.mu
	liveSnap  atomic.Pointer[map[string]bool] // projets dont la session vit dans un conteneur (autre launcher compris)
	openTabs  sync.Map                        // projets ouverts (chemin → true), lisible sans a.mu
	hubGen    atomic.Uint64                   // génération de la dernière relecture des avertissements hubs
	ctrFounds atomic.Pointer[[]core.Found]
	bkSnap    atomic.Pointer[[]core.BackendConf]

	dirty     chan struct{}
	cardDirty chan struct{}
	quit      chan struct{}
}

// updState : annonces de mise à jour (même logique que le navigateur de la coque).
type updState struct {
	newLauncher, newTpl, tplSha                   string
	launcherPub, launcherOK, tplOK                bool
	prefTag, prefStep, prefErr, prefFile, prefSum string
	prefBusy, updating, checking                  bool
	hideLauncher, hideTpl, hideHub                bool
}

// NewApp : état initial (arborescence, caches) ; Start lance les relectures.
func NewApp(emit Emitter) *App {
	t0 := time.Now()
	cfg := core.LoadConfig()
	dConf := time.Since(t0)
	a := &App{cfg: cfg, emit: emit,
		git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{}, detail: map[string]core.GitDetail{},
		sessions: map[string]core.Resumable{}, milestones: core.ReadMsCache(), closing: map[string]string{},
		terms: map[string]*projTerm{}, started: time.Now(), fromCache: map[string]bool{},
		dirty: make(chan struct{}, 1), cardDirty: make(chan struct{}, 1), quit: make(chan struct{})}
	a.perf.trace("démarrage : config", core.ConfigPath(), dConf)
	a.tplVer = core.ReadTrim(core.TplVersionPath())
	go core.WarmEngineEnv() // contexte docker lu d'avance (Windows : jusqu'à 4 s), pas à l'ouverture de Réglages
	a.perf.timed("démarrage : hubs", core.BackendsPath(), func() {
		a.hubCache = core.HubCacheDir()
		a.hubWarns = core.HubConfigWarnings() // faute de frappe dans hubs: (config partagée) : signalée
	})
	// arbre du lancement précédent (affiché tout de suite, relu en tâche de
	// fond) ; à défaut, parcours des racines (#125)
	if nodes, ok := cachedTree(cfg.Roots); ok {
		a.nodes, a.treeCached = nodes, true
	} else {
		a.perf.time("parcours", func() {
			a.perf.timed("démarrage : parcours", strings.Join(cfg.Roots, " ; "), func() { a.nodes = core.ScanTree(cfg.Roots) })
		})
		saveTree(cfg.Roots, a.nodes)
	}
	a.storeProjects()
	// conso : lue en tâche de fond (Start), CLAUDE_CONFIG_DIR peut être sur 9P
	a.usage.Loading = true
	// badges immédiats : cache LOCAL seulement (état préparé, badges git du
	// lancement précédent) ; le cache du hub Linux (hub_cache, souvent par 9P)
	// est lu ensuite en tâche de fond (#130)
	a.perf.time("caches", func() {
		t := time.Now()
		gc := loadGitCache()
		n := 0
		for _, p := range a.projectPaths() {
			if ps, _, ok := core.ReadState(p); ok {
				a.git[p], a.tpl[p], a.detail[p] = ps.Git, ps.Tpl, ps.Detail
				a.fromCache[p] = true
				n++
			} else if g, ok := gc[p]; ok {
				a.git[p], a.tpl[p] = g.Git, g.Tpl
			}
		}
		a.perf.trace("démarrage : caches", core.CacheDir(), time.Since(t), fmt.Sprintf("%d états en cache sur %d projets", n, len(a.projectPaths())))
	})
	if mgr, err := core.NewStateMgr(); err == nil {
		a.mgr = mgr
		// relecture des projets froids espacée sur un disque réseau ; durée de
		// chaque calcul d'état notée (#130)
		cold := time.Duration(0)
		if gitWorkers(cfg.Roots) < 8 {
			cold = 30 * time.Minute
		}
		mgr.Tune(cold, func(path string, d time.Duration) { a.perf.trace("état (git, fichiers)", path, d) })
		mgr.SetProjects(a.projectPaths())
	}
	for _, p := range a.projectPaths() {
		a.sel = p
		break
	}
	return a
}

func (a *App) projectPaths() []string {
	var out []string
	for _, n := range a.nodes {
		if n.Project {
			out = append(out, n.Path)
		}
	}
	return out
}

func (a *App) node(path string) (core.Node, bool) {
	for _, n := range a.nodes {
		if n.Project && n.Path == path {
			return n, true
		}
	}
	return core.Node{}, false
}

// changed : l'état a bougé ; l'interface sera mise à jour (regroupé, 80 ms).
func (a *App) changed() {
	select {
	case a.dirty <- struct{}{}:
	default:
	}
}

func (a *App) cardsChanged() {
	select {
	case a.cardDirty <- struct{}{}:
	default:
	}
}

func (a *App) setMsg(m string) {
	a.mu.Lock()
	a.msg = m
	a.mu.Unlock()
	a.changed()
}

// Start : relectures en tâche de fond (même rythme que la coque).
func (a *App) Start() {
	// liens et réglages dans CLAUDE_CONFIG_DIR (souvent sur le stockage
	// partagé, par 9P sous Windows) : en tâche de fond, jamais avant la fenêtre
	go func() {
		a.perf.timed("démarrage : liens conso", core.ClaudeDir(), core.LinkUsageCache)
		a.perf.timed("démarrage : settings.json", core.ClaudeDir(), func() { _ = core.SyncLauncherEnv(a.conf()) })
	}()
	go a.hubCacheLoop()
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-a.quit:
				a.perf.flushTrace()
				return
			case <-t.C:
				a.perf.flushTrace()
			}
		}
	}()
	go core.RunBrowserServer() // fichiers pour le navigateur (#106)
	go a.emitter()
	go a.treeLoop()
	go func() {
		if a.treeCached {
			a.perf.time("parcours", a.rescan)
		}
		a.perf.time("git", a.gitAll)
		a.perf.write(len(a.projectPaths()))
		a.changed()
	}()
	// sessions en suspens : lues dans ~/.claude, toutes les 10 s (30 s sur un disque réseau)
	sessEvery := 10 * time.Second
	if gitWorkers(a.conf().Roots) < 8 {
		sessEvery = 30 * time.Second
	}
	go a.every(sessEvery, a.readSessions)
	go a.everyTimed("conteneurs", 10*time.Second, a.readCtr)
	go a.everyTimed("conso", 30*time.Second, a.readUsage)
	go a.everyTimed("milestones", time.Minute, func() { a.readMilestones(false) })
	go a.every(2*time.Second, a.refreshCards)
	go a.updateLoop()
	if a.mgr != nil {
		go func() {
			for ps := range a.mgr.Done {
				a.mu.Lock()
				a.git[ps.Path], a.tpl[ps.Path], a.detail[ps.Path] = ps.Git, ps.Tpl, ps.Detail
				sel := a.sel == ps.Path
				a.mu.Unlock()
				a.changed()
				if sel {
					a.cardsChanged()
				}
			}
		}()
		a.mgr.Select(a.sel)
	}
	go a.reattach()
}

// Stop : l'interface se ferme ; les sessions des projets continuent dans tmux.
func (a *App) Stop() {
	select {
	case <-a.quit:
		return
	default:
		close(a.quit)
	}
	a.mu.Lock()
	terms := make([]*projTerm, 0, len(a.terms))
	for _, t := range a.terms {
		terms = append(terms, t)
	}
	a.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range terms {
		wg.Add(1)
		go func(t *projTerm) { defer wg.Done(); t.detach() }(t)
	}
	wg.Wait()
}

// everyTimed : every, la première exécution mesurée (journal #125).
func (a *App) everyTimed(step string, d time.Duration, f func()) {
	first := true
	a.every(d, func() {
		if first {
			first = false
			a.perf.time(step, f)
			return
		}
		f()
	})
}

func (a *App) every(d time.Duration, f func()) {
	f()
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			f()
		}
	}
}

func (a *App) emitter() {
	for {
		select {
		case <-a.quit:
			return
		case <-a.dirty:
			a.emit("state", a.Snapshot())
			time.Sleep(80 * time.Millisecond)
		case <-a.cardDirty:
			// hors de cette goroutine : une carte lente (projet par 9P) ne doit
			// pas retarder l'arbre (#130) ; une seule relecture à la fois
			if cardsMu.TryLock() {
				go func() {
					defer cardsMu.Unlock()
					a.mu.Lock()
					sel := a.sel
					a.mu.Unlock()
					c := a.cardsFull(sel)
					if a.selected(sel) {
						a.emit("cards", c)
					}
				}()
			}
		}
	}
}

// ── Relectures ────────────────────────────────────────────────────────────

func (a *App) treeLoop() {
	ch := core.WatchTree(a.conf().Roots)
	for {
		select {
		case <-a.quit:
			return
		case <-ch:
			a.rescan()
		}
	}
}

// rescan : arborescence relue ; l'état des nouveaux projets est calculé.
func (a *App) rescan() {
	nodes := core.ScanTree(a.conf().Roots)
	a.mu.Lock()
	if reflect.DeepEqual(nodes, a.nodes) {
		a.mu.Unlock()
		return
	}
	known := map[string]bool{}
	for _, p := range a.projectPaths() {
		known[p] = true
	}
	a.nodes = nodes
	a.storeProjects()
	var fresh []string
	for _, p := range a.projectPaths() {
		if !known[p] {
			fresh = append(fresh, p)
		}
		delete(known, p)
	}
	for p := range known {
		delete(a.git, p)
		delete(a.tpl, p)
		delete(a.detail, p)
		delete(a.milestones, p)
	}
	if a.mgr != nil {
		a.mgr.SetProjects(a.projectPaths())
	}
	roots := a.conf().Roots
	a.mu.Unlock()
	saveTree(roots, nodes)
	a.changed()
	for _, p := range fresh {
		go a.gitOne(p, latestTemplate())
	}
}

var gitSem = make(chan struct{}, 8)

func (a *App) gitAll() {
	a.mu.Lock()
	first := map[string]bool{a.sel: true}
	for p := range a.terms {
		first[p] = true
	}
	var paths []string
	for _, p := range a.projectPaths() {
		// état déjà en cache : le gestionnaire d'état le tient à jour, inutile
		// de relancer git ici (sur 9P, un git par projet coûte cher)
		if !a.fromCache[p] {
			paths = append(paths, p)
		}
	}
	a.started = time.Now()
	roots := a.conf().Roots
	a.mu.Unlock()
	latest := latestTemplate()
	sem := make(chan struct{}, gitWorkers(roots))
	var wg sync.WaitGroup
	for _, p := range gitOrder(paths, first) {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) { defer wg.Done(); defer func() { <-sem }(); a.gitOne(p, latest) }(p)
	}
	wg.Wait()
	a.mu.Lock()
	a.loadedAt = time.Since(a.started)
	gc := map[string]gitEntry{}
	for p, g := range a.git {
		gc[p] = gitEntry{Git: g, Tpl: a.tpl[p]}
	}
	a.mu.Unlock()
	writeJSON(gitCachePath(), gc)
	a.changed()
}

func (a *App) gitOne(p, latest string) {
	gitSem <- struct{}{}
	var g core.GitInfo
	var t core.TemplateState
	a.perf.timed("git (badges)", p, func() { g, t = core.ReadGit(p), core.ReadTemplate(p, latest) })
	<-gitSem
	a.mu.Lock()
	a.git[p], a.tpl[p] = g, t
	a.mu.Unlock()
	a.changed()
}

func (a *App) readSessions() {
	a.mu.Lock()
	paths := a.projectPaths()
	a.mu.Unlock()
	sess := map[string]core.Resumable{}
	t0, slow, slowD := time.Now(), "", time.Duration(0)
	for _, p := range paths {
		t := time.Now()
		if r, ok := a.conf().PendingSession(p); ok {
			sess[p] = r
		}
		if d := time.Since(t); d > slowD {
			slow, slowD = p, d
		}
	}
	a.perf.trace("sessions (~/.claude)", core.ClaudeDir(), time.Since(t0), fmt.Sprintf("%d projets, le plus lent %s (%s)", len(paths), slow, slowD.Round(time.Millisecond)))
	a.mu.Lock()
	a.sessions = sess
	a.mu.Unlock()
	a.changed()
}

func (a *App) readCtr() {
	a.mu.Lock()
	paths := a.projectPaths()
	a.mu.Unlock()
	var v core.CtrView
	a.perf.timed("conteneurs", core.BackendsPath(), func() { v = core.ReadCtrView(paths) })
	a.mu.Lock()
	a.ctr = v
	a.mu.Unlock()
	a.storeCtrSnapshots(v)
	a.changed()
}

// storeProjects : ensemble des projets de l'arbre, pour vérifier sans verrou
// qu'un chemin fourni par l'interface (mode --web : le client n'est pas de
// confiance) désigne bien un projet connu (a.nodes ; a.mu tenu ou démarrage).
func (a *App) storeProjects() {
	set := map[string]bool{}
	for _, n := range a.nodes {
		if n.Project {
			set[n.Path] = true
		}
	}
	a.projSet.Store(&set)
}

// checkProjectArg : quand l'appel vise un projet (need), le chemin fourni par
// l'interface doit être un projet de l'arbre.
func (a *App) checkProjectArg(need bool, path string) error {
	if need && !a.knownProject(path) {
		return fmt.Errorf("projet inconnu : %s", path)
	}
	return nil
}

// knownProject : le chemin est un projet de l'arbre (sous les racines).
func (a *App) knownProject(path string) bool {
	p := a.projSet.Load()
	return p != nil && (*p)[path]
}

// storeCtrSnapshots : instantanés des conteneurs et des backends pour la fenêtre
// Réglages, lisibles sans verrou (backendStates).
func (a *App) storeCtrSnapshots(v core.CtrView) {
	founds := make([]core.Found, 0, len(v.Found))
	for _, f := range v.Found {
		founds = append(founds, f)
	}
	a.ctrFounds.Store(&founds)
	live := make(map[string]bool, len(v.Live))
	for p := range v.Live {
		live[p] = true
	}
	a.liveSnap.Store(&live)
	a.storeBackends()
}

// storeBackends : backends déclarés, relus (après un enregistrement de
// backends.yaml, et à chaque lecture des conteneurs).
func (a *App) storeBackends() {
	bs, _ := core.LoadBackends()
	a.bkSnap.Store(&bs)
}

func (a *App) readUsage() {
	var u core.Usage
	a.perf.timed("conso", core.ClaudeDir(), func() {
		core.LinkUsageCache()
		if core.NeedsFetch(a.conf().UsageRefreshSec) {
			core.FetchUsage(a.conf().UsageRefreshSec)
		}
		u = core.ReadUsage()
	})
	a.mu.Lock()
	a.usage = u
	a.mu.Unlock()
	a.changed()
}

// readMilestones : milestones GitHub de plus de MsRefresh (de tous si force).
func (a *App) readMilestones(force bool) {
	a.mu.Lock()
	var todo []string
	for _, p := range a.projectPaths() {
		if st, ok := a.milestones[p]; force || !ok || time.Since(st.At) >= core.MsRefresh {
			todo = append(todo, p)
		}
	}
	a.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range todo {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			st := core.MsState{At: time.Now()}
			if core.OnGitHub(p) {
				core.MsSem <- struct{}{}
				var s core.MsState
				var err error
				a.perf.timed("milestones (gh)", p, func() { s, err = core.ReadMilestones(p, a.conf().GithubToken) })
				<-core.MsSem
				if err != nil {
					return
				}
				st, st.At = s, time.Now()
			}
			a.mu.Lock()
			old := a.milestones[p]
			if !reflect.DeepEqual(old.WithoutAt(), st.WithoutAt()) || old.At.IsZero() {
				a.milestones[p] = st
				core.WriteMsCache(a.milestones)
			}
			sel := a.sel == p
			a.mu.Unlock()
			if sel {
				a.cardsChanged()
			}
		}(p)
	}
	wg.Wait()
	a.changed()
}

// latestTemplate : commit du dernier tag du template adopté.
func latestTemplate() string { return core.ReadTrim(core.TplCommitPath()) }

// ── Ce que l'interface affiche ────────────────────────────────────────────

// Row : une ligne de l'arbre.
type Row struct {
	Kind    string           `json:"kind"` // root, cat, project
	Name    string           `json:"name"`
	Path    string           `json:"path"`
	Key     string           `json:"key,omitempty"`
	Depth   int              `json:"depth"`
	Last    bool             `json:"last,omitempty"`
	Missing bool             `json:"missing,omitempty"`
	Dot     *core.Indicator  `json:"dot,omitempty"`
	Badges  []core.Indicator `json:"badges,omitempty"`
	GitOK   bool             `json:"gitOK,omitempty"`
	Release string           `json:"release,omitempty"`
	Open    bool             `json:"open,omitempty"`
	Color   string           `json:"color,omitempty"` // couleur du projet (#rrggbb)
	Closing string           `json:"closing,omitempty"`
}

// Alert : annonce de mise à jour.
type Alert struct {
	Kind  string `json:"kind"`  // launcher, template
	Text  string `json:"text"`  // sans le ✕
	Color string `json:"color"` // green, yellow, orange
	Click bool   `json:"click"` // un clic agit (installer, adopter, réessayer)
}

// UsageRow : une ligne de consommation.
type UsageRow struct {
	Label  string `json:"label"`
	Pct    int    `json:"pct"`
	Reset  string `json:"reset"`
	Tone   string `json:"tone"` // blue, yellow, red, dim
	Passed bool   `json:"passed,omitempty"`
}

// Snapshot : tout ce que montre la colonne de gauche, et les onglets.
type Snapshot struct {
	Version  string     `json:"version"`
	Tpl      string     `json:"tpl"`
	Alerts   []Alert    `json:"alerts"`
	Usage    []UsageRow `json:"usage"`
	UsageMsg string     `json:"usageMsg,omitempty"`
	Stale    string     `json:"stale,omitempty"`
	Login    string     `json:"login,omitempty"`    // alerte de connexion claude.ai
	LoginBad bool       `json:"loginBad,omitempty"` // expirée (rouge)
	Rows     []Row      `json:"rows"`
	Selected string     `json:"selected"`
	Msg      string     `json:"msg,omitempty"`
	Status   string     `json:"status"`
	Perf     string     `json:"perf,omitempty"` // durées du chargement (#125)
	Tabs     []TabInfo  `json:"tabs"`
	Roots    []string   `json:"roots"`
	OS       string     `json:"os"`
}

// TabInfo : un onglet de projet ouvert.
type TabInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Busy     bool   `json:"busy"`
	State    string `json:"state"` // opening, ready, closed, error
	ReadOnly bool   `json:"readOnly"`
}

func (a *App) projInfo(n core.Node) core.ProjInfo {
	g, ok := a.git[n.Path]
	r, hasSess := a.sessions[n.Path]
	f, ctrKnown := a.ctr.Found[n.Path]
	l, hasLive := a.ctr.Live[n.Path]
	t := a.terms[n.Path]
	p := core.ProjInfo{Open: t != nil, Git: g, GitOK: ok, Tpl: a.tpl[n.Path], TplLatest: a.tplVer,
		Ms: a.milestones[n.Path], Sess: r, HasSess: hasSess,
		HasEnv: a.ctr.Env[n.Path], Backend: a.ctr.Assign[n.Path], Ctr: f, CtrKnown: ctrKnown,
		Problems: a.ctr.Diag[a.ctr.Assign[n.Path]], BadCfg: a.ctr.Bad[n.Path], Live: l, HasLive: hasLive, Dups: a.ctr.Dups[n.Path]}
	if t != nil && t.isReadOnly() && !hasLive {
		// lecture seule connue de l'onglet avant la relecture des conteneurs
		p.HasLive, p.Live = true, core.LiveSess{Owner: core.OwnerInfo{Id: t.ownerID(), At: time.Now()}, Claude: true}
	}
	p.Dot, p.DotWhy = core.SessionOf(p)
	return p
}

// Snapshot : état courant pour l'interface.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Snapshot{Version: core.NgName + " " + core.NgVersion, Tpl: core.OrQ(a.tplVer), Selected: a.sel, Msg: a.msg,
		Roots: a.conf().Roots, OS: goos}
	s.Alerts = a.alerts()
	a.usageRows(&s)
	for _, n := range a.nodes {
		r := Row{Name: n.Name, Path: n.Path, Depth: n.Depth, Last: n.Last, Missing: n.Missing}
		switch {
		case n.Root:
			r.Kind = "root"
		case !n.Project:
			r.Kind = "cat"
		default:
			r.Kind, r.Key = "project", n.Key
			p := a.projInfo(n)
			d := core.DotIndicator(p)
			r.Dot, r.Badges, r.GitOK = &d, core.BadgeIndicators(p), p.GitOK
			r.Release = a.milestones[n.Path].Release
			r.Open = p.Open
			r.Color = core.Colour256Hex(a.conf().ProjectColor(n.Key))
			r.Closing = a.closing[n.Path]
		}
		s.Rows = append(s.Rows, r)
	}
	s.Perf = a.perf.summary()
	if a.loadedAt > 0 {
		s.Status = fmt.Sprintf("%d projets en %s", len(a.projectPaths()), a.loadedAt.Round(time.Millisecond))
	} else {
		s.Status = fmt.Sprintf("git %d/%d", len(a.git), len(a.projectPaths()))
	}
	for _, p := range a.order {
		if t := a.terms[p]; t != nil {
			s.Tabs = append(s.Tabs, t.info())
		}
	}
	return s
}

func (a *App) usageRows(s *Snapshot) {
	u := a.usage
	switch {
	case u.Expired:
		s.Login, s.LoginBad = "✗ connexion claude.ai expirée · cliquer pour /login", true
	case !u.LoginBy.IsZero():
		s.Login = "⚠ connexion claude.ai : expiration " + core.FormatReset(u.LoginBy.UTC().Format(time.RFC3339)) + " · cliquer pour /login"
	}
	if u.Expired && u.Err == core.AuthErr {
		u.Err = "" // déjà dit au-dessus
	}
	if len(u.Limits) == 0 {
		s.UsageMsg = "chargement…"
		if u.Expired && u.Err == "" {
			s.UsageMsg = "indisponible"
		}
		if u.Err != "" {
			s.UsageMsg = u.Err
		}
		return
	}
	if u.Err != "" || time.Since(u.ReadAt) > time.Duration(a.conf().UsageRefreshSec)*5*time.Second {
		s.Stale = "⚠ données de " + u.ReadAt.Format("15:04")
		if u.Err != "" {
			s.Stale += " — " + u.Err
		}
	}
	for _, l := range u.Limits {
		pct := int(l.Percent + 0.5)
		reset := core.FormatReset(l.ResetsAt)
		tone := "blue"
		switch {
		case s.Stale != "" || reset == "passée":
			tone = "dim"
		case l.Severity == "critical" || pct >= 90:
			tone = "red"
		case l.Severity == "warning" || pct >= 70:
			tone = "yellow"
		}
		s.Usage = append(s.Usage, UsageRow{Label: l.Label(), Pct: min(100, max(0, pct)), Reset: reset, Tone: tone, Passed: reset == "passée"})
	}
}

// alerts : annonces de mise à jour, textes de la coque (version large).
func (a *App) alerts() []Alert {
	u := a.upd
	var out []Alert
	if u.newLauncher != "" && !u.hideLauncher {
		al := Alert{Kind: "launcher", Color: "orange"}
		tag := u.newLauncher
		switch {
		case u.updating:
			al.Text, al.Color = "↑ NG "+tag+" : installation…", "green"
		case u.launcherOK:
			al.Text, al.Color, al.Click = "↑ NG "+tag+" · cliquer pour installer", "green", true
		case u.launcherPub && u.prefBusy:
			al.Text, al.Color = "↑ NG "+tag+" · "+core.StepText(u.prefStep, true), "yellow"
		case u.launcherPub && u.prefErr != "":
			al.Text, al.Color, al.Click = "↑ NG "+tag+" · disponible mais inaccessible ("+u.prefErr+") · cliquer pour réessayer", "yellow", true
		case u.launcherPub:
			al.Text, al.Color = "↑ NG "+tag+" · "+core.StepText("start", true), "yellow"
		default:
			al.Text = "↑ NG " + tag + " · nouvelle version en préparation"
		}
		out = append(out, al)
	}
	if len(a.hubWarns) > 0 && !u.hideHub {
		// réglages par machine ignorés (#127) : un clic ouvre le diagnostic
		t := "⚠ " + a.hubWarns[0]
		if len(a.hubWarns) > 1 {
			t += fmt.Sprintf(" (+%d : voir le diagnostic)", len(a.hubWarns)-1)
		}
		out = append(out, Alert{Kind: "hubcfg", Text: t, Color: "yellow", Click: true})
	}
	if u.newTpl != "" && !u.hideTpl {
		al := Alert{Kind: "template", Color: "orange", Text: "↑ tpl " + u.newTpl + " · nouvelle version en préparation"}
		if u.tplOK {
			al.Text, al.Color, al.Click = "↑ tpl "+u.newTpl+" · cliquer pour installer", "green", true
		}
		out = append(out, al)
	}
	return out
}

// ── Cartes ◈ État du projet sélectionné ───────────────────────────────────

// Cards : cartes Projet (légende, git, versions), Milestone, Team, Fichiers.
type Cards struct {
	Path      string           `json:"path"`
	Name      string           `json:"name"`
	Loaded    bool             `json:"loaded"`
	Updated   string           `json:"updated,omitempty"`
	StaleUpd  bool             `json:"staleUpd,omitempty"`
	Legend    []core.Indicator `json:"legend"`
	IsRepo    bool             `json:"isRepo"`
	Branch    string           `json:"branch,omitempty"`
	VerLocal  string           `json:"verLocal,omitempty"`
	Release   []Span           `json:"release,omitempty"`
	LastMsg   string           `json:"lastMsg,omitempty"`
	Milestone *MsCard          `json:"milestone,omitempty"`
	Team      []TeamRow        `json:"team"`
	Open      bool             `json:"open"`
	Files     []FileRow        `json:"files"`
	Pending   []string         `json:"pending,omitempty"` // encore à compléter (état, team) : en cours
}

// Span : texte et sa couleur (dim, yellow… ; vide : normal).
type Span struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

// MsCard : carte Milestone (même contenu que core.MsLines).
type MsCard struct {
	Kind    string     `json:"kind"` // working, releasing, idle, done
	Title   string     `json:"title"`
	Closed  int        `json:"closed"`
	Total   int        `json:"total"`
	Phases  []Span     `json:"phases,omitempty"`
	Blocked int        `json:"blocked,omitempty"`
	Pending []MsRowOut `json:"pending,omitempty"`
	More    int        `json:"more,omitempty"`
	DoneAgo string     `json:"doneAgo,omitempty"`
	ReadAgo string     `json:"readAgo,omitempty"`
}

type MsRowOut struct {
	Title  string `json:"title"`
	Closed int    `json:"closed"`
	Total  int    `json:"total"`
}

// TeamRow : un teammate de la carte Team (#136) : chaque instance lancée, puis
// les teammates du template non démarrés (contracts/desktop-dispatch.md).
type TeamRow struct {
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	Companion bool   `json:"companion,omitempty"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Claude    string `json:"claude,omitempty"`
	Pane      string `json:"pane,omitempty"`
	Started   bool   `json:"started"`
	Active    bool   `json:"active"`
	Hidden    bool   `json:"hidden"`
	Pinned    bool   `json:"pinned"`
	IdleLeft  int    `json:"idleLeft"`

	// état du template (cycle 4) : ok / late / "" ; version cible ; compagnon
	TplState      string `json:"tplState"`
	TplTarget     string `json:"tplTarget,omitempty"`
	CompanionFile string `json:"companionFile,omitempty"`
}

// FileRow : une entrée de premier niveau du projet.
type FileRow struct {
	Name   string `json:"name"`
	Dir    bool   `json:"dir,omitempty"`
	Meta   string `json:"meta"`
	Change string `json:"change,omitempty"`
}

func (a *App) refreshCards() { a.cardsChanged() }

// Cards : cartes complètes du projet sélectionné (cards.go).
func (a *App) Cards() Cards {
	a.mu.Lock()
	sel := a.sel
	a.mu.Unlock()
	return a.cardsFull(sel)
}

var direct = struct {
	sync.Mutex
	m map[string]core.ProjState
}{m: map[string]core.ProjState{}}

// directCached : état calculé directement il y a moins de 30 s, sans calcul.
func directCached(p string) (core.ProjState, bool) {
	direct.Lock()
	defer direct.Unlock()
	ps, ok := direct.m[p]
	return ps, ok && time.Since(ps.UpdatedAt) < 30*time.Second
}

func (a *App) directState(p string) core.ProjState {
	direct.Lock()
	defer direct.Unlock()
	if ps, ok := direct.m[p]; ok && time.Since(ps.UpdatedAt) < 30*time.Second {
		return ps
	}
	ps := core.ComputeState(p)
	direct.m[p] = ps
	return ps
}

// releaseSpans : core.ReleaseText, en morceaux colorés.
func releaseSpans(ms core.MsState, tag string) []Span {
	if ms.Release == "" {
		if tag == "" {
			return []Span{{Text: core.OrNone(tag)}}
		}
		return []Span{{Text: tag}, {Text: " (tag git)", Tone: "dim"}}
	}
	out := []Span{{Text: ms.Release}}
	if !ms.ReleaseAt.IsZero() {
		out = append(out, Span{Text: " GitHub, il y a " + core.AgeLabel(time.Since(ms.ReleaseAt)), Tone: "dim"})
	}
	if tag != "" && tag != ms.Release {
		out = append(out, Span{Text: " · tag git " + tag, Tone: "yellow"})
	}
	return out
}

const msMaxRows = 6

func msCard(st core.MsState) *MsCard {
	c := &MsCard{Title: st.Title, Closed: st.Closed, Total: st.Open + st.Closed}
	if !st.At.IsZero() {
		c.ReadAgo = ageText(st.At)
	}
	switch st.Kind {
	case core.MsWorking:
		c.Kind = "working"
		for _, p := range st.Phases {
			sp := Span{Text: fmt.Sprintf("%s %d", strings.ToLower(p.Name), p.N)}
			if p.Name == "SANS PHASE" {
				sp.Tone = "dim"
			}
			c.Phases = append(c.Phases, sp)
		}
		c.Blocked = st.Blocked
	case core.MsReleasing:
		c.Kind = "releasing"
	case core.MsIdle:
		c.Kind = "idle"
		rows := st.Pending
		if len(rows) > msMaxRows {
			c.More = len(rows) - msMaxRows
			rows = rows[:msMaxRows]
		}
		for _, r := range rows {
			c.Pending = append(c.Pending, MsRowOut{Title: r.Title, Closed: r.Closed, Total: r.Open + r.Closed})
		}
	case core.MsDone:
		c.Kind = "done"
		c.DoneAgo = core.AgeLabel(time.Since(st.DoneAt))
	default:
		return nil
	}
	return c
}

func ageText(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 5*time.Second:
		return "à l'instant"
	case d < time.Minute:
		return fmt.Sprintf("il y a %d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("il y a %d min", int(d.Minutes()))
	}
	return fmt.Sprintf("il y a %d h", int(d.Hours()))
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f Mo", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f Ko", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d o", n)
}

// ── Appels de l'interface ─────────────────────────────────────────────────

// Dispatch : un appel de l'interface (méthode, arguments JSON) ; la réponse est
// sérialisée en JSON. Seul point d'entrée, commun à Wails et au mode web.
func (a *App) Dispatch(method string, raw json.RawMessage) (any, error) {
	// méthodes aux arguments structurés (le décodage générique ci-dessous
	// attend des chaînes : « backend » y est un nom, ici un objet)
	switch method {
	case "projectSettings":
		var in struct {
			Path    string
			Changes []core.ConfigChange
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
		return a.projectSettings(in.Path, in.Changes)
	case "projectApply":
		var in struct {
			Path    string
			Backend *string
			Image   *string
			Color   *string
			Env     *struct {
				MTime   string
				Changes []core.ConfigChange
			}
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
		ch := core.ProjectChange{Backend: in.Backend, Image: in.Image, Color: in.Color}
		if in.Env != nil {
			ch.Env = &core.EnvChange{MTime: in.Env.MTime, Changes: in.Env.Changes}
		}
		return a.projectApply(in.Path, ch)
	case "backendTest":
		var in struct {
			Backend core.BackendEntry `json:"backend"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
		return a.backendTest(in.Backend), nil
	case "configApply":
		var in struct {
			Kind, Path, MTime string
			Changes           []core.ConfigChange
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
		// le chemin d'un projet fourni par le client doit être un projet de l'arbre
		if err := a.checkProjectArg(core.ConfigKind(in.Kind) == core.ConfProjectEnv, in.Path); err != nil {
			return nil, err
		}
		return a.configApply(core.ConfigKind(in.Kind), in.Path, in.MTime, in.Changes)
	}
	var arg struct {
		Path, Pane, Data, Base, Name, Remote, Image, Backend, Kind, Mode string
		Content, MTime                                                   string
		Cols, Rows                                                       int
		Raw, OpenNext, Binary                                            bool
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &arg); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
	}
	switch method {
	case "init":
		a.mu.Lock()
		sel := a.sel
		a.mu.Unlock()
		return map[string]any{"state": a.Snapshot(), "cards": a.selectCards(sel), "help": helpData(), "prefs": loadPrefs()}, nil
	case "setPrefs": // préférences d'affichage (#123, #124) : objet fusionné
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			return nil, fmt.Errorf("setPrefs : %v", err)
		}
		return savePrefs(patch), nil
	case "select":
		a.mu.Lock()
		changed := a.sel != arg.Path
		a.sel = arg.Path
		a.mu.Unlock()
		if changed && a.mgr != nil {
			a.mgr.Select(arg.Path)
		}
		a.changed()
		// cartes en cache tout de suite, complétées en tâche de fond (#130)
		return a.selectCards(arg.Path), nil
	case "setTheme": // thème Auto / Jour / Nuit (#133) : barre de titre de la fenêtre
		if themeHook != nil {
			themeHook(arg.Mode)
		}
		return nil, nil
	case "cardsFor": // projet survolé ou voisin : préparé à l'avance (#130)
		return a.prefetchCards(arg.Path), nil
	case "open":
		return a.open(arg.Path, core.LaunchNormal)
	case "close":
		return a.askClose(arg.Path)
	case "closeConfirm":
		return nil, a.confirmClose(arg.Path)
	case "detach":
		a.detachTab(arg.Path)
		return nil, nil
	case "refresh": // F5
		go a.manualRefresh()
		return nil, nil
	case "rescan": // Ctrl+R
		go func() { a.rescan(); a.gitAll() }()
		return nil, nil
	case "dismiss":
		a.mu.Lock()
		switch arg.Kind {
		case "template":
			a.upd.hideTpl = true
		case "launcher":
			a.upd.hideLauncher = true
		case "hubcfg":
			a.upd.hideHub = true
		default:
			a.upd.hideTpl, a.upd.hideLauncher = true, true
		}
		a.mu.Unlock()
		a.changed()
		return nil, nil
	case "alert":
		a.clickAlert(arg.Kind)
		return nil, nil
	case "login":
		return nil, a.login()
	case "diag":
		return diagText(), nil
	case "tools":
		return toolsData(), nil
	case "newInfo":
		return a.newInfo(arg.Path), nil
	case "create":
		return a.create(arg.Base, arg.Name, arg.Raw, arg.Remote, arg.Image, arg.Backend)
	case "execApply":
		return a.execApply(arg.Path, arg.Image, arg.Backend, arg.OpenNext)
	case "termInput":
		if t := a.term(arg.Path); t != nil {
			if arg.Binary {
				arg.Data = binaryBytes(arg.Data)
			}
			t.input(arg.Pane, arg.Data)
		}
		return nil, nil
	case "resizePane": // bordure glissée (#135) : Kind « x » / « y », Cols : taille
		if t := a.term(arg.Path); t != nil {
			t.resizePane(arg.Pane, arg.Kind, arg.Cols)
		}
		return nil, nil
	case "launchInput":
		if t := a.term(arg.Path); t != nil {
			t.launchInput(arg.Data)
		}
		return nil, nil
	case "termResize":
		if t := a.term(arg.Path); t != nil {
			t.resize(arg.Cols, arg.Rows)
		}
		return nil, nil
	case "termFocus":
		if t := a.term(arg.Path); t != nil {
			t.focus(arg.Pane)
		}
		return nil, nil
	case "termZoom":
		if t := a.term(arg.Path); t != nil {
			t.zoom(arg.Pane)
		}
		return nil, nil
	case "teamToggle": // double-clic sur un teammate de la carte Team (#136)
		t := a.term(arg.Path)
		if t == nil {
			return map[string]any{"ok": false, "msg": "projet fermé"}, nil
		}
		ok, msg := t.teamToggle(arg.Pane)
		if ok {
			return map[string]any{"ok": true}, nil
		}
		return map[string]any{"ok": false, "msg": msg}, nil
	case "relayout":
		if t := a.term(arg.Path); t != nil {
			go t.relayout(true)
		}
		return nil, nil
	case "termSync": // l'interface (re)demande l'état complet d'un onglet
		if t := a.term(arg.Path); t != nil {
			go t.resync()
		}
		return nil, nil
	case "configFiles": // éditeur de configuration (#117) ; Path : projet (facultatif)
		if err := a.checkProjectArg(arg.Path != "", arg.Path); err != nil {
			return nil, err
		}
		return core.ConfigFiles(arg.Path), nil
	case "configOpen":
		if err := a.checkProjectArg(core.ConfigKind(arg.Kind) == core.ConfProjectEnv, arg.Path); err != nil {
			return nil, err
		}
		return a.configOpen(core.ConfigKind(arg.Kind), arg.Path)
	case "configValidate":
		if err := a.checkProjectArg(core.ConfigKind(arg.Kind) == core.ConfProjectEnv, arg.Path); err != nil {
			return nil, err
		}
		ps, merged := a.configCheck(core.ConfigKind(arg.Kind), arg.Path, arg.Content)
		return map[string]any{"problems": ps, "merged": merged}, nil
	case "configSave":
		if err := a.checkProjectArg(core.ConfigKind(arg.Kind) == core.ConfProjectEnv, arg.Path); err != nil {
			return nil, err
		}
		return a.configSave(core.ConfigKind(arg.Kind), arg.Path, arg.Content, arg.MTime)
	case "configModel": // écran Réglages (#118) : fichier lu en modèle
		return a.configModel(core.ConfigKind(arg.Kind), arg.Path)
	case "backendStates": // états et charge des pools, depuis le cache du diagnostic
		return a.backendStates(), nil
	case "quit":
		return nil, a.quitApp(arg.Mode)
	}
	return nil, fmt.Errorf("appel inconnu : %s", method)
}

func (a *App) term(path string) *projTerm {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.terms[path]
}

// manualRefresh : F5 — mises à jour, consommation, milestones et arbre maintenant.
func (a *App) manualRefresh() {
	a.mu.Lock()
	if a.upd.checking {
		a.mu.Unlock()
		return
	}
	a.upd.checking, a.upd.hideLauncher, a.upd.hideTpl = true, false, false
	a.msg = "recherche des mises à jour…"
	a.mu.Unlock()
	a.changed()
	go func() {
		core.FetchUsage(0)
		u := core.ReadUsage()
		a.mu.Lock()
		a.usage = u
		a.mu.Unlock()
		a.changed()
	}()
	go a.readMilestones(true)
	go a.rescan()
	a.applyUpd(core.CheckUpdatesNow(true))
}

var goos = runtime.GOOS
