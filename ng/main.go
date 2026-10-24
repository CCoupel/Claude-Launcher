// Draft Go du menu de claude-launcher : même sources de données que le bash
// (conf, arbre de projets, badges git/template, consommation Claude, fenêtres
// tmux), mais rafraîchies en parallèle sans jamais bloquer l'affichage.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var gitSem = make(chan struct{}, 8) // au plus 8 git simultanés

type (
	gitMsg struct {
		path string
		info GitInfo
		tpl  TemplateState
	}
	windowsMsg map[string]bool
	usageMsg   Usage
	tickMsg    time.Time
	usageTick  time.Time
	detailMsg  struct {
		path string
		d    GitDetail
	}
)

type model struct {
	cfg          Config
	nodes        []Node
	visible      []int
	cursor       int
	top          int
	filter       string
	w, h         int
	git          map[string]GitInfo
	detail       map[string]GitDetail
	asked        map[string]bool
	tpl          map[string]TemplateState
	latest       string
	windows      map[string]bool
	usage        Usage
	session      string
	launch       bool
	shell        bool
	lastSel      string
	launcherVer  string
	tplVer       string
	newLauncher  string
	newTpl       string
	mgr          *stateMgr
	form         newForm
	quitStep     int // 0, quitAsk (launcher seul ou tout ?), quitAll (confirmer la fermeture des projets)
	wantState    bool
	navW         int // largeur demandée à tmux (appuis rapprochés)
	navAt        time.Time
	wantTeam     bool // clic hors projet : bascule sur « ⌗ Teamleaders »
	lastShow     time.Time
	hideLauncher bool
	updating     bool    // mise à jour lancée d'un clic, en cours
	pendingCmd   tea.Cmd // commande produite par onMouse (qui ne renvoie que le modèle)
	hideTpl      bool
	lastClickIdx int
	lastClickAt  time.Time
	lastWinW     int
	lastNavW     int
	msg          string
	pending      int
	started      time.Time
	loadedAt     time.Duration
}

func newModel(launch bool) model {
	cfg := loadConfig()
	m := model{
		cfg: cfg, git: map[string]GitInfo{}, detail: map[string]GitDetail{}, asked: map[string]bool{}, tpl: map[string]TemplateState{},
		windows: map[string]bool{}, launch: launch, started: time.Now(), shell: os.Getenv("CL_SHELL") == "1",
	}
	m.session = tmuxSession()
	m.launcherVer = localLauncherVersion()
	m.tplVer = readTrim(tplVersionPath())
	m.nodes = withSpecial(scanTree(cfg.Roots))
	m.usage = readUsage()
	m.refilter()
	m.cursor = m.firstProject()
	if m.shell {
		// cache d'état en tâche de fond (inotify) : la vue « ◈ État » ne fait que lire
		if mgr, err := newStateMgr(); err == nil {
			m.mgr = mgr
			m.mgr.SetProjects(m.projectPaths())
			for _, p := range m.projectPaths() { // badges immédiats depuis le cache précédent
				if ps, _, ok := readState(p); ok {
					m.git[p], m.tpl[p], m.detail[p] = ps.Git, ps.Tpl, ps.Detail
				}
			}
		}
	}
	return m
}

func (m model) projectPaths() []string {
	var out []string
	for _, n := range m.nodes {
		if n.Project {
			out = append(out, n.Path)
		}
	}
	return out
}

type stateDoneMsg ProjState

func waitState(ch <-chan ProjState) tea.Cmd {
	return func() tea.Msg { return stateDoneMsg(<-ch) }
}

func (m *model) refilter() {
	m.visible = m.visible[:0]
	f := strings.ToLower(m.filter)
	for i, n := range m.nodes {
		if f == "" || (n.Project && strings.Contains(strings.ToLower(n.Name), f)) {
			m.visible = append(m.visible, i)
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
}

func (m model) Init() tea.Cmd {
	m.latest = readLatestTemplate()
	cmds := []tea.Cmd{tickCmd(), usageTickCmd(), windowsCmd(m.session), usageFetchCmd(m.cfg.UsageRefreshSec), updTickCmd(6 * time.Second)}
	if m.mgr != nil {
		cmds = append(cmds, waitState(m.mgr.Done))
	}
	if m.shell {
		// onglet « ⌗ Teamleaders » : une vignette par projet ouvert
		cfg := m.cfg
		cmds = append(cmds, func() tea.Msg { runMirrorManager(cfg); return nil })
	}
	for _, n := range m.nodes {
		if n.Project {
			cmds = append(cmds, gitCmd(n.Path, readLatestTemplate()))
		}
	}
	return tea.Batch(cmds...)
}

func gitCmd(path, latest string) tea.Cmd {
	return func() tea.Msg {
		gitSem <- struct{}{}
		defer func() { <-gitSem }()
		return gitMsg{path, readGit(path), readTemplate(path, latest)}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func usageTickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return usageTick(t) })
}
func windowsCmd(session string) tea.Cmd {
	return func() tea.Msg { return windowsMsg(tmuxWindows(targetSession(session))) }
}
func usageFetchCmd(sec int) tea.Cmd {
	return func() tea.Msg {
		if needsFetch(sec) {
			fetchUsage(sec)
		}
		return usageMsg(readUsage())
	}
}

// readLatestTemplate : commit du dernier tag du template. La source de référence est
// le cache à côté du script (celui que met à jour checkUpdates, comme le bash) ; les
// autres emplacements ne servent que de repli.
func readLatestTemplate() string {
	if c := readTrim(tplCommitPath()); c != "" {
		return c
	}
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	for _, p := range []string{
		filepath.Join(home, ".template-commit"),
		filepath.Join(filepath.Dir(exe), ".template-commit"),
		filepath.Join(filepath.Dir(exe), "..", ".template-commit"),
	} {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.shell {
			return m, winWidthCmd(msg.Width)
		}
	case winMsg:
		// Largeur totale inchangée mais navigateur modifié = bordure glissée à la
		// souris : on la mémorise. Si le terminal a changé de taille, c'est le hook
		// fixwidth qui rétablit la largeur mémorisée. Navigateur pleine largeur =
		// zone tmux fermée, pas un glissement : on ne mémorise pas.
		if m.lastWinW != 0 && msg.win == m.lastWinW && msg.nav != m.lastNavW && msg.nav < msg.win {
			saveNavWidth(msg.nav)
		}
		m.lastWinW, m.lastNavW = msg.win, msg.nav
	case gitMsg:
		m.git[msg.path] = msg.info
		m.tpl[msg.path] = msg.tpl
		if len(m.git) == m.countProjects() {
			m.loadedAt = time.Since(m.started)
		}
	case updTickMsg:
		return m, checkUpdates()
	case updMsg:
		if msg.ok {
			if tag := ""; versionNewer(ngVersion, msg.launcherTag) {
				tag = msg.launcherTag
				if tag != m.newLauncher {
					m.hideLauncher = false
				}
				m.newLauncher = tag
			} else {
				m.newLauncher = tag
			}
			// nouveau tag du template annoncé (comme l'entrée « ↑ template … disponible » du bash)
			if msg.tplChanged {
				m.newTpl, m.hideTpl = msg.tplTag, false
			}
			// le commit de référence a pu changer : réévalue les badges template
			m.tplVer = readTrim(tplVersionPath())
			latest := readTrim(tplCommitPath())
			for p := range m.tpl {
				m.tpl[p] = readTemplate(p, latest)
			}
		}
		return m, updTickCmd(10 * time.Minute)
	case detailMsg:
		m.detail[msg.path] = msg.d
	case stateDoneMsg:
		ps := ProjState(msg)
		m.git[ps.Path], m.tpl[ps.Path], m.detail[ps.Path] = ps.Git, ps.Tpl, ps.Detail
		return m, waitState(m.mgr.Done)
	case windowsMsg:
		m.windows = msg
		if m.mgr != nil {
			open := map[string]bool{}
			for _, n := range m.nodes {
				if n.Project && m.windows[n.Key] {
					open[n.Path] = true
				}
			}
			m.mgr.SetOpen(open)
		}
	case usageMsg:
		m.usage = Usage(msg)
	case tickMsg:
		return m, tea.Batch(tickCmd(), windowsCmd(m.session))
	case usageTick:
		return m, tea.Batch(usageTickCmd(), usageFetchCmd(m.cfg.UsageRefreshSec))
	case tea.MouseMsg:
		mm := m.onMouse(msg)
		mm.syncSelection()
		cmd := mm.pendingCmd
		mm.pendingCmd = nil
		return mm, tea.Batch(cmd, mm.detailCmd(), mm.stateCmd())
	case updDoneMsg:
		m.updating = false
		switch {
		case msg.err != nil:
			m.msg = "⚠ mise à jour : " + msg.err.Error()
		case msg.binDone && m.shell:
			restartOnNewBinary() // ne revient pas : ce pane est relancé
		case msg.binDone:
			m.msg = "✓ " + msg.tag + " installée — relancer le launcher"
		default:
			m.msg = "✓ déjà à jour"
			m.hideLauncher = true
		}
	case tea.KeyMsg:
		nm, cmd := m.onKey(msg)
		mm := nm.(model)
		mm.syncSelection()
		return mm, tea.Batch(cmd, mm.detailCmd(), mm.stateCmd())
	}
	m.syncSelection()
	return m, m.detailCmd()
}

// resizeNav : chaque « ] » (Ctrl+→) élargit la zone navigation de 4 colonnes,
// chaque « [ » (Ctrl+←) la réduit ; la largeur est mémorisée pour les prochains
// lancements.
//
// La largeur n'est connue qu'après la réponse de tmux : pour une touche maintenue
// ou des appuis rapprochés, on part de la largeur déjà demandée (navW) et non de
// m.w, sinon les appuis suivants repartent de l'ancienne valeur.
func (m model) resizeNav(keys string) model {
	w := m.w
	if m.navW > 0 && time.Since(m.navAt) < 1500*time.Millisecond {
		w = m.navW
	}
	for _, r := range keys {
		if r == ']' {
			w += 4
		} else if r == '[' {
			w -= 4
		}
	}
	w = max(20, w)
	_ = otmux("resize-pane", "-t", outerSession+":0.0", "-x", strconv.Itoa(w)).Run()
	saveNavWidth(w)
	m.navW, m.navAt = w, time.Now()
	return m
}

// stateCmd : après une action de navigation, remet tmux sur l'onglet « ◈ État »
// (le projet sélectionné y est détaillé). Limité à un appel / 250 ms pour les
// répétitions de touche.
func (m *model) stateCmd() tea.Cmd {
	target := ""
	switch {
	case m.wantTeam:
		target = teamWindow
	case m.wantState:
		target = stateWindow
	}
	m.wantState, m.wantTeam = false, false
	if target == "" || !m.shell || time.Since(m.lastShow) < 250*time.Millisecond {
		return nil
	}
	m.lastShow = time.Now()
	return func() tea.Msg {
		_ = dtmux("select-window", "-t", exactW(hubSession, target)).Run()
		return nil
	}
}

// openSelected lance (ou rouvre) le projet sous le curseur.
func (m model) openSelected() model { return m.openSelectedAs(launchNormal) }

func (m model) openSelectedAs(mode launchMode) model {
	if len(m.visible) == 0 {
		return m
	}
	n := m.nodes[m.visible[m.cursor]]
	if n.Special == "new" {
		return m.startNewProject("")
	}
	if !n.Project {
		return m
	}
	m.msg = openProject(m.cfg, m.session, n, m.launch, readLatestTemplate(), mode)
	m.wantState, m.wantTeam, m.lastShow = false, false, time.Time{}
	if m.shell && m.launch && m.msg == "" {
		// le projet est à l'écran à droite : donne-lui le focus clavier
		_ = otmux("select-pane", "-t", outerSession+":0.1").Run()
	}
	return m
}

// onMouse : clic = sélectionner, double-clic = ouvrir, molette = défiler.
func (m model) onMouse(e tea.MouseMsg) model {
	if m.form.mode != formNone {
		return m
	}
	if m.quitStep != 0 {
		m.quitStep = 0 // un clic annule la sortie
		return m
	}
	switch {
	case e.Button == tea.MouseButtonWheelUp:
		m.move(-3)
		m.wantState = true
	case e.Button == tea.MouseButtonWheelDown:
		m.move(3)
		m.wantState = true
	case e.Button == tea.MouseButtonLeft && e.Action == tea.MouseActionPress:
		_, bodyH, _ := m.layout()
		// sous la liste : filtre (bodyH), puis en-tête = session, versions, alertes…
		if k := e.Y - bodyH - 3; k >= 0 && k < len(m.alerts()) {
			// « ↑ NG … disponible » : le texte met à jour, le ✕ masque
			if a := m.alerts()[k]; a == "launcher" && e.X < lipgloss.Width(launcherAlert(m.newLauncher)) {
				if !m.updating {
					m.updating, m.msg = true, ""
					m.pendingCmd = updateNowCmd(m.newLauncher)
				}
			} else {
				m.dismissAlert(a)
			}
			return m
		}
		// clic hors d'un projet (zone vide, titre de groupe ou de racine, filtre,
		// en-tête, aide, entrée « Nouveau projet ») : vue d'ensemble des teamleaders
		if e.Y < 0 || e.Y >= bodyH {
			m.wantTeam = true
			return m
		}
		idx := m.listTop(bodyH) + e.Y
		if idx < len(m.visible) {
			// titre de catégorie (EMBY, INFRA…) ou de racine : double-clic = créer un
			// projet dans ce dossier ; un clic simple montre les teamleaders
			if n := m.nodes[m.visible[idx]]; !n.Project && n.Special == "" && n.Path != "" {
				double := idx == m.lastClickIdx && time.Since(m.lastClickAt) < 450*time.Millisecond
				m.lastClickIdx, m.lastClickAt = idx, time.Now()
				if double {
					m.lastClickAt = time.Time{}
					return m.startNewProject(n.Path)
				}
				m.wantTeam = true
				return m
			}
		}
		if idx >= len(m.visible) || !(m.nodes[m.visible[idx]].Project || m.nodes[m.visible[idx]].Special != "") {
			m.wantTeam = true
			return m
		}
		double := idx == m.lastClickIdx && time.Since(m.lastClickAt) < 450*time.Millisecond
		m.cursor = idx
		m.lastClickIdx, m.lastClickAt = idx, time.Now()
		if double {
			m.lastClickAt = time.Time{}
			return m.openSelected()
		}
		if m.nodes[m.visible[idx]].Special != "" {
			m.wantTeam = true // « Nouveau projet » n'est pas un projet : pas d'État à montrer
		} else {
			m.wantState = true
		}
	}
	return m
}

type winMsg struct{ win, nav int }

// winWidthCmd lit la largeur totale de la fenêtre coque (hors client tmux).
func winWidthCmd(nav int) tea.Cmd {
	return func() tea.Msg {
		b, err := otmux("display-message", "-p", "-t", outerSession+":0", "#{window_width}").Output()
		if err != nil {
			return winMsg{nav: nav}
		}
		w, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return winMsg{win: w, nav: nav}
	}
}

// syncSelection publie le projet sélectionné pour la vue « ◈ État ».
func (m *model) syncSelection() {
	if len(m.visible) == 0 {
		return
	}
	n := m.nodes[m.visible[m.cursor]]
	if !n.Project || n.Path == m.lastSel {
		return
	}
	m.lastSel = n.Path
	writeSelection(n)
	if m.mgr != nil {
		m.mgr.Select(n.Path)
	}
}

func (m model) firstProject() int {
	for i, idx := range m.visible {
		if m.nodes[idx].Project {
			return i
		}
	}
	return 0
}

// detailCmd charge dirty/dernier commit du projet sélectionné, une seule fois.
func (m model) detailCmd() tea.Cmd {
	if m.shell || len(m.visible) == 0 {
		return nil // en coque, le détail vient du cache d'état
	}
	n := m.nodes[m.visible[m.cursor]]
	if !n.Project || m.asked[n.Path] || !m.git[n.Path].IsRepo {
		return nil
	}
	m.asked[n.Path] = true
	p := n.Path
	return func() tea.Msg {
		gitSem <- struct{}{}
		defer func() { <-gitSem }()
		return detailMsg{p, readDetail(p)}
	}
}

func (m model) countProjects() int {
	c := 0
	for _, n := range m.nodes {
		if n.Project {
			c++
		}
	}
	return c
}

const (
	quitAsk = 1
	quitAll = 2
)

// openProjects : fenêtres projet ouvertes (hors onglets du launcher).
func (m model) openProjects() int {
	c := 0
	for w := range m.windows {
		if w != stateWindow && w != teamWindow && w != "[menu]" {
			c++
		}
	}
	return c
}

// quitLauncher : arrête le launcher sans toucher à claude-hub. En mode coque, le
// serveur claude-ui entier est arrêté (et pas seulement détaché) pour que la
// prochaine relance reparte du binaire installé, par exemple après une mise à jour.
func (m model) quitLauncher() tea.Cmd {
	if m.shell {
		_ = otmux("kill-server").Run()
	}
	return tea.Quit
}

// quitAllProjects : ferme claude-hub (et les sessions groupées avec elle, qui
// partagent ses fenêtres), puis le launcher.
func (m model) quitAllProjects() tea.Cmd {
	group, _ := tmuxOut("display-message", "-p", "-t", exactS(hubSession)+":", "#{session_group}")
	if group != "" {
		out, _ := tmuxOut("list-sessions", "-F", "#{session_group}|#{session_name}")
		for _, l := range strings.Split(out, "\n") {
			if g, name, ok := strings.Cut(l, "|"); ok && g == group && name != hubSession {
				_ = dtmux("kill-session", "-t", exactS(name)).Run()
			}
		}
	}
	_ = dtmux("kill-session", "-t", exactS(hubSession)).Run()
	return m.quitLauncher()
}

func (m model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitStep != 0 {
		// toute touche non prévue annule : par défaut on reste
		step := m.quitStep
		m.quitStep = 0
		switch key := strings.ToLower(k.String()); {
		case step == quitAsk && key == "l":
			return m, m.quitLauncher()
		case step == quitAsk && key == "t":
			if m.openProjects() == 0 {
				return m, m.quitAllProjects()
			}
			m.quitStep = quitAll
		case step == quitAll && (key == "o" || key == "y"):
			return m, m.quitAllProjects()
		}
		return m, nil
	}
	if m.form.mode != formNone {
		return m.onFormKey(k)
	}
	switch k.String() {
	case "ctrl+c", "esc":
		if m.filter != "" && k.String() == "esc" {
			m.filter = ""
			m.refilter()
			m.wantState = true
			return m, nil
		}
		m.quitStep = quitAsk
		return m, nil
	case "ctrl+u":
		m.hideLauncher, m.hideTpl = true, true
		return m, nil
	case "tab":
		if m.shell {
			_ = otmux("select-pane", "-t", outerSession+":0.1").Run()
		}
	case "ctrl+left", "ctrl+right":
		// Ctrl+← réduit, Ctrl+→ élargit la zone du navigateur (4 colonnes par appui)
		if m.shell {
			key := "]"
			if k.String() == "ctrl+left" {
				key = "["
			}
			return m.resizeNav(key), nil
		}
		return m, nil
	case "up", "ctrl+p":
		m.move(-1)
		m.wantState = true
	case "down", "ctrl+n":
		m.move(1)
		m.wantState = true
	case "pgup":
		m.move(-10)
		m.wantState = true
	case "pgdown":
		m.move(10)
		m.wantState = true
	case "ctrl+r":
		m.nodes = withSpecial(scanTree(m.cfg.Roots))
		if m.mgr != nil {
			m.mgr.SetProjects(m.projectPaths())
		}
		m.refilter()
		m.git = map[string]GitInfo{}
		m.detail = map[string]GitDetail{}
		m.asked = map[string]bool{}
		m.started = time.Now()
		m.loadedAt = 0
		cmds := []tea.Cmd{windowsCmd(m.session)}
		latest := readLatestTemplate()
		for _, n := range m.nodes {
			if n.Project {
				cmds = append(cmds, gitCmd(n.Path, latest))
			}
		}
		return m, tea.Batch(cmds...)
	case "enter":
		return m.openSelected(), nil
	case "backspace":
		if m.filter != "" {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
			m.refilter()
			m.wantState = true
		}
	default:
		if k.Type == tea.KeyRunes {
			m.filter += string(k.Runes)
			m.refilter()
			m.cursor = m.firstProject()
			m.wantState = true
		}
	}
	return m, nil
}

func (m *model) move(d int) {
	// le curseur ne s'arrête que sur les projets
	i := m.cursor
	for step := 0; step < len(m.visible); step++ {
		j := i + d
		if j < 0 || j >= len(m.visible) {
			break
		}
		i = j
		if n := m.nodes[m.visible[i]]; n.Project || n.Special != "" {
			m.cursor = i
			return
		}
		if abs(d) > 1 {
			d = sign(d)
		}
	}
}
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
func sign(a int) int {
	if a < 0 {
		return -1
	}
	return 1
}

// ── Rendu ────────────────────────────────────────────────────────────────

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold   = lipgloss.NewStyle().Bold(true)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

func (m model) badges(p string) string {
	g, ok := m.git[p]
	if !ok {
		return dim.Render(" …")
	}
	var b string
	switch {
	case !g.IsRepo:
	case !g.Upstream:
		b += blue.Render(" ○")
	case g.Behind > 0:
		b += red.Render(fmt.Sprintf(" ↓%d", g.Behind))
	default:
		b += green.Bold(true).Render(" ●")
	}
	switch m.tpl[p] {
	case TplOK:
		b += green.Render(" ✓")
	case TplOutdated:
		b += yellow.Render(" ⚠")
	case TplMissing:
		b += red.Render(" ⚠ fichiers manquants")
	}
	return b
}

func truncLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return strings.Join(lines, "\n")
}

// layout : géométrie commune à l'affichage et à la souris. En coque, la preview
// est toujours retirée : elle ferait doublon avec l'onglet « ◈ État » de tmux.
func (m model) layout() (listW, bodyH int, compact bool) {
	compact = m.shell || m.w < 80
	listW = m.w * 38 / 100
	if listW < 30 {
		listW = min(m.w, 30)
	}
	if compact {
		listW = m.w
	}
	hh := lipgloss.Height(truncLines(m.renderHeader(listW-2), m.w))
	footH := 1
	if compact {
		footH = 2
	}
	bodyH = max(3, m.h-hh-1-footH)
	return
}

// listTop : première ligne visible de la liste (fenêtre centrée sur le curseur).
func (m model) listTop(h int) int {
	top := m.cursor - h/2
	if top < 0 {
		top = 0
	}
	if top+h > len(m.visible) {
		top = max(0, len(m.visible)-h)
	}
	return top
}

func (m model) View() string {
	if m.w == 0 {
		return "chargement…"
	}
	listW, bodyH, compact := m.layout()
	header := truncLines(m.renderHeader(listW-2), m.w)
	list := m.renderList(listW-2, bodyH)
	if m.form.mode != formNone {
		list = m.renderForm(listW - 2)
	}

	var body string
	hint := "  ↑↓ naviguer · entrée ouvrir · ctrl-r rescanner · esc quitter"
	if compact {
		body = lipgloss.NewStyle().Width(m.w).MaxWidth(m.w).Height(bodyH).Render(list)
		hint = "  ↵ ouvrir · tab→tmux · ctrl+←→ zone"
	} else {
		prev := m.renderPreview(m.w - listW - 3)
		left := lipgloss.NewStyle().Width(listW).Height(bodyH).Render(list)
		right := lipgloss.NewStyle().Width(m.w - listW - 1).Height(bodyH).
			BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).
			PaddingLeft(1).Render(prev)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	filter := dim.Render("  filtre › ") + m.filter + dim.Render("▏")
	foot := dim.Render(hint)
	// Sortie : la question occupe les lignes du pied (deux en mode compact, où le
	// navigateur ne fait souvent que 40 colonnes).
	var q [2]string
	switch m.quitStep {
	case quitAsk:
		q = [2]string{"  [l] launcher seul (projets gardés)", "  [t] tout fermer · autre : annuler"}
	case quitAll:
		q = [2]string{fmt.Sprintf("  Fermer %d projet(s) et leur claude ?", m.openProjects()), "  [o] oui · autre : annuler"}
	}
	switch {
	case q[0] != "" && compact:
		foot = yellow.Render(q[0]) + "\n" + yellow.Render(q[1])
	case q[0] != "":
		foot = yellow.Render(q[0] + " ·" + q[1][1:])
	case compact:
		foot += "\n" + m.footStatus()
	default:
		foot += m.footStatus()
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, filter, header, truncLines(foot, m.w))
}

func (m model) footStatus() string {
	s := ""
	if m.loadedAt > 0 {
		s = dim.Render(fmt.Sprintf("  · %d projets en %s", m.countProjects(), m.loadedAt.Round(time.Millisecond)))
	} else {
		s = dim.Render(fmt.Sprintf("  · git %d/%d", len(m.git), m.countProjects()))
	}
	if m.msg != "" {
		s += "  " + yellow.Render(m.msg)
	}
	return s
}

func (m model) renderList(w, h int) string {
	if len(m.visible) == 0 {
		return dim.Render("  aucun projet")
	}
	top := m.listTop(h)
	var sb strings.Builder
	for i := top; i < len(m.visible) && i < top+h; i++ {
		n := m.nodes[m.visible[i]]
		var line string
		switch {
		case n.Special != "":
			line = " " + cyan.Render(n.Name) + dim.Render(" (dbl-clic catégorie)")
		case n.Root:
			line = bold.Render(" " + shorten(n.Name, w-2))
		default:
			indent := strings.Repeat("  ", n.Depth)
			branch := "├─ "
			if n.Last {
				branch = "└─ "
			}
			if m.filter != "" {
				indent, branch = "", ""
			}
			prefix := " " + dim.Render(indent+branch)
			if !n.Project {
				line = prefix + cyan.Render(n.Name)
			} else {
				dot, name := dim.Render("○ "), n.Name
				if m.windows[n.Key] {
					// ouvert : nom en surbrillance, sur la couleur de son pane tmux
					dot = green.Render("● ")
					name = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).
						Background(lipgloss.Color(m.cfg.projectColor(n.Key))).Render(" " + n.Name + " ")
				}
				line = prefix + dot + name + m.badges(n.Path)
			}
		}
		if i == m.cursor {
			line = "▶" + strings.TrimPrefix(line, " ")
		}
		sb.WriteString(ansi.Truncate(line, w, "…") + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m model) renderPreview(w int) string {
	if len(m.visible) == 0 {
		return ""
	}
	n := m.nodes[m.visible[m.cursor]]
	if !n.Project {
		return dim.Render(n.Path)
	}
	var sb strings.Builder
	sb.WriteString(bold.Render(n.Name) + "\n")
	sb.WriteString(dim.Render(shorten(n.Path, w)) + "\n\n")
	if m.windows[n.Key] {
		sb.WriteString(green.Render("● fenêtre tmux ouverte") + "\n")
	} else {
		sb.WriteString(dim.Render("○ non lancé") + "\n")
	}
	if g, ok := m.git[n.Path]; ok && g.IsRepo {
		sb.WriteString("\n" + dim.Render("branche  ") + g.Branch)
		d := m.detail[n.Path]
		if d.Dirty {
			sb.WriteString(yellow.Render("  *modifié"))
		}
		switch {
		case !g.Upstream:
			sb.WriteString("\n" + dim.Render("amont    ") + "aucun")
		case g.Behind > 0:
			sb.WriteString("\n" + dim.Render("amont    ") + red.Render(fmt.Sprintf("en retard de %d commit(s)", g.Behind)))
		default:
			sb.WriteString("\n" + dim.Render("amont    ") + "à jour")
		}
		sb.WriteString("\n" + dim.Render("dernier  ") + shorten(d.LastMsg, w-9))
	}
	sb.WriteString("\n")
	switch m.tpl[n.Path] {
	case TplOK:
		sb.WriteString("\n" + dim.Render("template ") + green.Render("à jour"))
	case TplOutdated:
		sb.WriteString("\n" + dim.Render("template ") + yellow.Render("mise à jour disponible"))
	case TplMissing:
		sb.WriteString("\n" + dim.Render("template ") + red.Render("fichiers manquants"))
	}
	return sb.String()
}

// bareVer : « v2.29.0 » -> « 2.29.0 » (compact), « ? » si inconnue.
func bareVer(v string) string { return strings.TrimPrefix(orQ(v), "v") }

// versionLines : versions du launcher et du template, et alertes de mise à jour.
func (m model) versionLines() string {
	// « sh » = le script claude-launcher.sh, que le NG réutilise (layout, vignettes).
	// 40 colonnes par défaut : libellés courts, complets si la place le permet.
	line := "NG " + ngVersion + " · sh " + bareVer(m.launcherVer) + " · tpl " + bareVer(m.tplVer)
	if m.w >= 70 {
		line = ngName + " " + ngVersion + " · script " + orQ(m.launcherVer) + " · dernier template " + orQ(m.tplVer)
	}
	s := "\n" + dim.Render("  "+line)
	for _, a := range m.alerts() {
		switch a {
		case "launcher":
			if m.updating {
				s += "\n  " + green.Render("↑ NG "+m.newLauncher+" : mise à jour…")
			} else {
				s += "\n" + green.Render(launcherAlert(m.newLauncher)) + dim.Render(" ✕")
			}
		case "template":
			s += "\n  " + yellow.Render("↑ template "+m.newTpl+" disponible") + dim.Render(" ✕")
		}
	}
	return s
}

// launcherAlert : partie cliquable (mise à jour) de l'annonce, avant le ✕.
func launcherAlert(tag string) string { return "  ↑ NG " + tag + " · cliquer pour installer" }

// alerts : annonces de mises à jour encore affichées, dans l'ordre d'affichage.
// Elles se masquent d'un clic (✕) ou avec ctrl+u ; les badges ⚠ par projet
// restent, eux, tant que le projet n'est pas à jour.
func (m model) alerts() []string {
	var a []string
	if m.newLauncher != "" && !m.hideLauncher {
		a = append(a, "launcher")
	}
	if m.newTpl != "" && !m.hideTpl {
		a = append(a, "template")
	}
	return a
}

func (m *model) dismissAlert(kind string) {
	switch kind {
	case "launcher":
		m.hideLauncher = true
	case "template":
		m.hideTpl = true
	}
}

func (m model) renderHeader(w int) string {
	var sb strings.Builder
	sb.WriteString(dim.Render("  session : ") + cyan.Render(m.session))
	sb.WriteString(m.versionLines())
	u := m.usage
	if len(u.Limits) == 0 {
		msg := "chargement…"
		if u.Err != "" {
			msg = u.Err
		}
		return sb.String() + "\n" + dim.Render("  consommation Claude : "+msg)
	}
	stale := ""
	if u.Err != "" || time.Since(u.ReadAt) > time.Duration(m.cfg.UsageRefreshSec)*5*time.Second {
		stale = "⚠ données de " + u.ReadAt.Format("15:04")
		if u.Err != "" {
			stale += " — " + u.Err
		}
	}
	lw, rw := 0, 0
	resets := make([]string, len(u.Limits))
	for i, l := range u.Limits {
		resets[i] = formatReset(l.ResetsAt)
		lw = max(lw, lipgloss.Width(l.Label()))
		rw = max(rw, lipgloss.Width(resets[i]))
	}
	barW := min(40, max(8, m.w-lw-rw-16))
	for i, l := range u.Limits {
		pct := int(l.Percent + 0.5)
		style := blue
		switch {
		case stale != "" || resets[i] == "passée":
			style = dim
		case l.Severity == "critical" || pct >= 90:
			style = red
		case l.Severity == "warning" || pct >= 70:
			style = yellow
		}
		p := min(100, max(0, pct))
		filled := (p*barW + 50) / 100
		label := bold.Render(padRight(l.Label(), lw))
		sb.WriteString("\n  " + label + " ")
		if resets[i] == "passée" {
			sb.WriteString(dim.Render(strings.Repeat("░", barW) + "    — réinit. passée"))
			continue
		}
		sb.WriteString(style.Render(strings.Repeat("█", filled)) + dim.Render(strings.Repeat("░", barW-filled)))
		sb.WriteString(style.Render(fmt.Sprintf(" %3d %%", pct)))
		if resets[i] != "" {
			sb.WriteString("  " + dim.Render(resets[i]))
		}
	}
	if stale != "" {
		sb.WriteString("\n  " + yellow.Render(stale))
	}
	return sb.String()
}

func main() {
	cmd := "shell"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "--") {
		cmd = os.Args[1]
	}
	launch := false
	for _, a := range os.Args[1:] {
		if a == "--launch" {
			launch = true
		}
	}
	var err error
	switch cmd {
	case "shell":
		selfUpdate()
		err = runShell()
	case "nav":
		_, err = tea.NewProgram(newModel(launch), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	case "state":
		err = runState()
	case "fixwidth":
		fixWidth()
	case "version", "--version":
		fmt.Println(ngName, ngVersion)
	default:
		err = fmt.Errorf("usage : claude-launcher-ng [shell|nav [--launch]|state|fixwidth|version]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
