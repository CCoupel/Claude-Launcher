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
	"time"

	"claude-launcher-ng2/core"
)

// Emitter : envoi d'un événement à l'interface (Wails ou mode web).
type Emitter func(name string, data any)

// App : état du launcher vu par l'interface.
type App struct {
	cfg  core.Config
	emit Emitter

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
	order      []string // onglets, dans l'ordre d'ouverture
	started    time.Time
	loadedAt   time.Duration
	mgr        *core.StateMgr
	upd        updState

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
	hideLauncher, hideTpl                         bool
}

// NewApp : état initial (arborescence, caches) ; Start lance les relectures.
func NewApp(emit Emitter) *App {
	cfg := core.LoadConfig()
	a := &App{cfg: cfg, emit: emit,
		git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{}, detail: map[string]core.GitDetail{},
		sessions: map[string]core.Resumable{}, milestones: core.ReadMsCache(), closing: map[string]string{},
		terms: map[string]*projTerm{}, started: time.Now(),
		dirty: make(chan struct{}, 1), cardDirty: make(chan struct{}, 1), quit: make(chan struct{})}
	a.tplVer = core.ReadTrim(core.TplVersionPath())
	a.nodes = core.ScanTree(cfg.Roots)
	a.usage = core.ReadUsage()
	if mgr, err := core.NewStateMgr(); err == nil {
		a.mgr = mgr
		mgr.SetProjects(a.projectPaths())
		for _, p := range a.projectPaths() { // badges immédiats depuis le cache précédent
			if ps, _, ok := core.ReadState(p); ok {
				a.git[p], a.tpl[p], a.detail[p] = ps.Git, ps.Tpl, ps.Detail
			}
		}
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
	core.LinkUsageCache()
	_ = core.SyncLauncherEnv(a.cfg)
	go core.RunBrowserServer() // fichiers pour le navigateur (#106)
	go a.emitter()
	go a.treeLoop()
	go a.gitAll()
	go a.every(10*time.Second, a.readSessions)
	go a.every(10*time.Second, a.readCtr)
	go a.every(30*time.Second, a.readUsage)
	go a.every(time.Minute, func() { a.readMilestones(false) })
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
			a.emit("cards", a.Cards())
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ── Relectures ────────────────────────────────────────────────────────────

func (a *App) treeLoop() {
	ch := core.WatchTree(a.cfg.Roots)
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
	nodes := core.ScanTree(a.cfg.Roots)
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
	a.mu.Unlock()
	a.changed()
	for _, p := range fresh {
		go a.gitOne(p, latestTemplate())
	}
}

var gitSem = make(chan struct{}, 8)

func (a *App) gitAll() {
	a.mu.Lock()
	paths := a.projectPaths()
	a.started = time.Now()
	a.mu.Unlock()
	latest := latestTemplate()
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		go func(p string) { defer wg.Done(); a.gitOne(p, latest) }(p)
	}
	wg.Wait()
	a.mu.Lock()
	a.loadedAt = time.Since(a.started)
	a.mu.Unlock()
	a.changed()
}

func (a *App) gitOne(p, latest string) {
	gitSem <- struct{}{}
	g, t := core.ReadGit(p), core.ReadTemplate(p, latest)
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
	for _, p := range paths {
		if r, ok := a.cfg.PendingSession(p); ok {
			sess[p] = r
		}
	}
	a.mu.Lock()
	a.sessions = sess
	a.mu.Unlock()
	a.changed()
}

func (a *App) readCtr() {
	a.mu.Lock()
	paths := a.projectPaths()
	a.mu.Unlock()
	v := core.ReadCtrView(paths)
	a.mu.Lock()
	a.ctr = v
	a.mu.Unlock()
	a.changed()
}

func (a *App) readUsage() {
	core.LinkUsageCache()
	if core.NeedsFetch(a.cfg.UsageRefreshSec) {
		core.FetchUsage(a.cfg.UsageRefreshSec)
	}
	u := core.ReadUsage()
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
				s, err := core.ReadMilestones(p, a.cfg.GithubToken)
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
		Roots: a.cfg.Roots, OS: goos}
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
			r.Color = core.Colour256Hex(a.cfg.ProjectColor(n.Key))
			r.Closing = a.closing[n.Path]
		}
		s.Rows = append(s.Rows, r)
	}
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
	if u.Err != "" || time.Since(u.ReadAt) > time.Duration(a.cfg.UsageRefreshSec)*5*time.Second {
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

// TeamRow : un teammate du registre du template.
type TeamRow struct {
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	Companion bool   `json:"companion,omitempty"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Claude    string `json:"claude,omitempty"`
	Count     int    `json:"count,omitempty"`
}

// FileRow : une entrée de premier niveau du projet.
type FileRow struct {
	Name   string `json:"name"`
	Dir    bool   `json:"dir,omitempty"`
	Meta   string `json:"meta"`
	Change string `json:"change,omitempty"`
}

func (a *App) refreshCards() { a.cardsChanged() }

// Cards : cartes du projet sélectionné.
func (a *App) Cards() Cards {
	a.mu.Lock()
	n, ok := a.node(a.sel)
	if !ok {
		a.mu.Unlock()
		return Cards{}
	}
	p := a.projInfo(n)
	ms := a.milestones[n.Path]
	t := a.terms[n.Path]
	a.mu.Unlock()

	c := Cards{Path: n.Path, Name: n.Name, Open: p.Open}
	ps, _, loaded := core.ReadState(n.Path)
	if loaded {
		c.Loaded = true
		c.Updated = ageText(ps.UpdatedAt)
		c.StaleUpd = time.Since(ps.UpdatedAt) > 2*time.Minute
		p.Git, p.GitOK, p.Tpl, p.TplTag = ps.Git, true, ps.Tpl, ps.TplTag
		c.IsRepo = ps.Git.IsRepo
		c.Branch = ps.Git.Branch
		c.VerLocal = core.OrNone(ps.Detail.VerLocal)
		c.Release = releaseSpans(ms, ps.Detail.VerRelease)
		c.LastMsg = ps.Detail.LastMsg
		for _, f := range ps.Files {
			fr := FileRow{Name: f.Name, Dir: f.Dir, Meta: humanSize(f.Size), Change: ps.Detail.Changes[f.Name]}
			if f.Dir {
				fr.Meta = fmt.Sprintf("%d élém.", f.Count)
			}
			c.Files = append(c.Files, fr)
		}
	}
	c.Legend = core.Legend(p)
	c.Milestone = msCard(ms)
	var panes []core.PaneInfo
	if t != nil {
		panes = t.paneInfos()
	}
	for _, tm := range core.LiveTeam(core.ReadTeammates(n.Path), n.Path, panes) {
		c.Team = append(c.Team, TeamRow{Name: tm.Name, Tag: tm.Tag, Companion: tm.Companion,
			Model: strings.TrimPrefix(tm.Model, "claude-"), Effort: tm.Effort, Claude: tm.Claude, Count: tm.Count})
	}
	return c
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
	var arg struct {
		Path, Pane, Data, Base, Name, Remote, Image, Backend, Kind, Mode string
		Cols, Rows                                                       int
		Raw, OpenNext                                                    bool
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &arg); err != nil {
			return nil, fmt.Errorf("%s : arguments invalides : %v", method, err)
		}
	}
	switch method {
	case "init":
		return map[string]any{"state": a.Snapshot(), "cards": a.Cards(), "help": helpData(), "tools": toolsData()}, nil
	case "select":
		a.mu.Lock()
		changed := a.sel != arg.Path
		a.sel = arg.Path
		a.mu.Unlock()
		if changed && a.mgr != nil {
			a.mgr.Select(arg.Path)
		}
		a.cardsChanged()
		a.changed()
		return nil, nil
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
		if arg.Kind == "template" {
			a.upd.hideTpl = true
		} else if arg.Kind == "launcher" {
			a.upd.hideLauncher = true
		} else {
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
			t.input(arg.Pane, arg.Data)
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
