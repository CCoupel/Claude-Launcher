package tmux

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// Vue « ◈ État » : pane de la coque, sous le navigateur, qui suit la sélection du
// navigateur (fichier selection.json) et montre l'état du projet sans
// l'ouvrir : git, template, team (teammates, versions) et fichiers.

type (
	selTick   time.Time
	stateData struct {
		path    string
		git     core.GitInfo
		detail  core.GitDetail
		tpl     core.TemplateState
		files   []core.FileEntry
		tag     string // template installé dans le projet
		updated time.Time
	}
	paneData struct {
		path  string
		open  bool
		lines []core.TeamLine // carte Team : une ligne par instance (#136)
		sess  core.Resumable  // session en suspens (badge ↺)
		hasS  bool
	}
	// toggleMsg : issue d'un double-clic sur un teammate de la carte Team.
	toggleMsg struct {
		path, msg string // msg : raison du refus, vide si la bascule a eu lieu
	}
)

type stateModel struct {
	cfg      core.Config
	w, h     int
	sel      Selection
	hasSel   bool
	data     stateData
	loaded   bool
	panes    paneData
	loadedAt time.Time
	paneAt   time.Time
	scroll   int // défilement de la liste de fichiers

	ms         core.MsState // milestones du projet (cache du navigateur, milestone.go)
	msMtime    time.Time    // mtime du cache des milestones déjà lu
	cacheMtime time.Time    // mtime du fichier de cache déjà affiché
	selAt      time.Time
	fallback   bool      // calcul direct si le cache n'arrive pas
	navAt      time.Time // dernière vérification de la présence du navigateur

	// carte Team : double-clic sur un teammate (même détection que le navigateur)
	lastClickPane string
	lastClickAt   time.Time
	teamMsg       string // refus de la dernière bascule, affiché 6 s sous la liste
	teamMsgAt     time.Time
}

// navKeys : commandes du navigateur aussi acceptées dans le pane des cartes —
// relayées au navigateur (touche tmux), qui prend le focus pour la suite
// (question de fermeture après Échap, etc.).
var navKeys = map[string]string{
	"esc": "Escape", "ctrl+c": "C-c", "f1": "F1", "f2": "F2", "f5": "F5",
	"ctrl+r": "C-r", "ctrl+u": "C-u", "tab": "Tab", "enter": "Enter", "delete": "DC",
	"ctrl+left": "C-Left", "ctrl+right": "C-Right",
}

// forwardToNav : relaie la touche au navigateur et lui donne le focus ; s'il a
// disparu, il est d'abord recréé (ensureLayout).
func forwardToNav(key string) tea.Cmd {
	return func() tea.Msg {
		nav := outerPane(roleNav)
		if nav == "" {
			if exe, err := platform.SelfExe(); err == nil && EnsureLayout(exe) == nil {
				nav = outerPane(roleNav)
				time.Sleep(500 * time.Millisecond) // le temps que le navigateur démarre
			}
		}
		if nav != "" {
			_ = otmux("select-pane", "-t", nav).Run()
			_ = otmux("send-keys", "-t", nav, key).Run()
		}
		return nil
	}
}

// healNavCmd : le navigateur a-t-il disparu de la coque ? Recréé si besoin.
func healNavCmd() tea.Cmd {
	return func() tea.Msg {
		if roles, _ := outerPanes(); len(roles) > 0 && roles[roleNav] == "" {
			if exe, err := platform.SelfExe(); err == nil {
				_ = EnsureLayout(exe)
			}
		}
		return nil
	}
}

func (m stateModel) Init() tea.Cmd { return selTickCmd() }

func selTickCmd() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg { return selTick(t) })
}

func loadStateCmd(s Selection) tea.Cmd {
	return func() tea.Msg {
		latest := readLatestTemplate()
		return stateData{
			path:    s.Path,
			git:     core.ReadGit(s.Path),
			detail:  core.ReadDetail(s.Path),
			tpl:     core.ReadTemplate(s.Path, latest),
			files:   core.ReadFiles(s.Path),
			tag:     core.TemplateTag(s.Path),
			updated: time.Now(),
		}
	}
}

func loadPanesCmd(s Selection, cfg core.Config) tea.Cmd {
	return func() tea.Msg {
		pd := paneData{path: s.Path}
		pd.sess, pd.hasS = cfg.PendingSession(s.Path)
		pd.open = tmuxWindows(hubSession)[s.Key]
		reg := core.ReadTeammates(s.Path)
		var panes []core.ProjPane
		var choices map[string]string
		if pd.open {
			pw := projectOf(exactW(hubSession, s.Key))
			t := tmuxFn(pw.t)
			// recensement unique : fenêtre du projet et réserve des panes cachés
			if ps, ch, err := core.ProjectPanes(t, pw.win, core.ProjectHome(t, pw.win, pw.isCtr()), pw.isCtr()); err == nil {
				panes, choices = ps, ch
			}
			if pw.ctrExec != nil {
				infos := make([]core.PaneInfo, len(panes))
				for i, p := range panes {
					infos[i] = p.PaneInfo
				}
				ctrPaneVersions(pw, infos)
				for i := range panes {
					panes[i].PaneInfo = infos[i]
				}
			}
		}
		pd.lines = core.TeamLines(reg, s.Path, panes, choices, cfg.TeamHideIdle, time.Now().Unix())
		return pd
	}
}

// toggleCmd : double-clic sur le teammate pane de la carte Team : bascule
// visible ↔ caché (inactif seulement), puis application immédiate. Refus
// (message affiché) : actif, leader, cdp, pane sans agent, lecture seule.
func toggleCmd(s Selection, cfg core.Config, pane string) tea.Cmd {
	return func() tea.Msg {
		pw := projectOf(exactW(hubSession, s.Key))
		if ro, _ := tout(hub, "display-message", "-p", "-t", pw.hubWin, "#{@cl_readonly}"); ro == "1" {
			return toggleMsg{path: s.Path, msg: "lecture seule : un autre launcher a la main sur ce projet"}
		}
		teamPath := findTeamConfig(s.Path)
		var tc *core.TeamConfig
		if teamPath != "" {
			if t, err := readTeam(teamPath); err == nil {
				tc = &t
			}
		}
		t := tmuxFn(pw.t)
		unlock := lockWin(pw) // @cl_panevis écrit sous le verrou de la fenêtre, comme le watcher
		res := core.TogglePane(t, pw.win, core.ProjectHome(t, pw.win, pw.isCtr()), tc, pane, pw.isCtr())
		unlock()
		if !res.OK {
			return toggleMsg{path: s.Path, msg: res.Msg}
		}
		if ws, err := readWin(pw); err == nil && teamPath != "" {
			applyVis(cfg, pw, ws.leader(), teamPath, time.Now())
		}
		return toggleMsg{path: s.Path}
	}
}

func (m stateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.loaded {
			if i := msg.Y - m.teamTop(); i >= 0 && i < len(m.panes.lines) {
				if l := m.panes.lines[i]; l.Started {
					double := l.Pane == m.lastClickPane && time.Since(m.lastClickAt) < 450*time.Millisecond
					m.lastClickPane, m.lastClickAt = l.Pane, time.Now()
					if double {
						m.lastClickAt = time.Time{}
						return m, toggleCmd(m.sel, m.cfg, l.Pane)
					}
				}
			}
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scroll -= 3
		case tea.MouseButtonWheelDown:
			m.scroll += 3
		}
		m.clampScroll()
	case tea.KeyMsg:
		if key, ok := navKeys[msg.String()]; ok {
			// plus de « q » / Ctrl+C qui fermaient ce pane : les commandes vont au navigateur
			return m, forwardToNav(key)
		}
		switch msg.String() {
		case "up", "k":
			m.scroll--
		case "down", "j":
			m.scroll++
		case "pgup":
			m.scroll -= 10
		case "pgdown":
			m.scroll += 10
		case "home":
			m.scroll = 0
		case "end":
			m.scroll = 1 << 30
		}
		m.clampScroll()
	case selTick:
		cmds := []tea.Cmd{selTickCmd()}
		if time.Since(m.navAt) > 3*time.Second {
			m.navAt = time.Now()
			cmds = append(cmds, healNavCmd())
		}
		s, ok := readSelection()
		switch {
		case ok && s.Path != m.sel.Path:
			m.sel, m.hasSel, m.scroll = s, true, 0
			m.loaded, m.cacheMtime, m.fallback = false, time.Time{}, false
			m.ms, m.msMtime = core.MsState{}, time.Time{}
			m.panes = paneData{}
			m.paneAt, m.selAt = time.Now(), time.Now()
			cmds = append(cmds, loadPanesCmd(s, m.cfg))
		case ok && time.Since(m.paneAt) > 2*time.Second:
			m.sel.Dot, m.sel.DotWhy = s.Dot, s.DotWhy // point d'état mis à jour par le navigateur
			m.paneAt = time.Now()
			cmds = append(cmds, loadPanesCmd(s, m.cfg))
		}
		if m.hasSel {
			// l'état vient du cache préparé en tâche de fond par le navigateur ;
			// calcul direct seulement s'il n'arrive pas (navigateur absent)
			m.reloadCache()
			if !m.loaded && !m.fallback && time.Since(m.selAt) > 4*time.Second {
				m.fallback = true
				cmds = append(cmds, loadStateCmd(m.sel))
			}
		}
		return m, tea.Batch(cmds...)
	case stateData:
		if msg.path == m.sel.Path {
			m.data, m.loaded = msg, true
			m.clampScroll()
		}
	case paneData:
		if msg.path == m.sel.Path {
			m.panes = msg
		}
	case toggleMsg:
		if msg.path == m.sel.Path {
			m.teamMsg, m.teamMsgAt = msg.msg, time.Now()
			return m, loadPanesCmd(m.sel, m.cfg)
		}
	}
	return m, nil
}

// reloadCache relit le fichier de cache du projet sélectionné s'il a changé.
func (m *stateModel) reloadCache() {
	if st, err := os.Stat(core.MsCachePath()); err == nil && !st.ModTime().Equal(m.msMtime) {
		m.ms, m.msMtime = core.ReadMsCache()[m.sel.Path], st.ModTime()
	}
	st, err := os.Stat(core.StatePath(m.sel.Path))
	if err != nil || (m.loaded && st.ModTime().Equal(m.cacheMtime)) {
		return
	}
	ps, mt, ok := core.ReadState(m.sel.Path)
	if !ok {
		return
	}
	m.data = stateData{path: ps.Path, git: ps.Git, detail: ps.Detail, tpl: ps.Tpl,
		files: ps.Files, tag: ps.TplTag, updated: ps.UpdatedAt}
	m.loaded, m.cacheMtime = true, mt
	m.clampScroll()
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

func (m *stateModel) clampScroll() {
	maxS := max(0, len(m.data.files)-m.filesRows())
	m.scroll = min(max(0, m.scroll), maxS)
}

// topCards : en-tête + cartes fixes (tout sauf les fichiers).
func (m stateModel) topCards() string { s, _ := m.topCardsAt(); return s }

// teamTop : ligne d'écran de la première ligne de la carte Team (-1 : absente).
func (m stateModel) teamTop() int { _, y := m.topCardsAt(); return y }

// topCardsAt : topCards et la ligne où commence la liste de la carte Team.
func (m stateModel) topCardsAt() (string, int) {
	w := min(m.w-2, 110)
	var sb strings.Builder
	sb.WriteString(" " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("◈ "+m.sel.Name) + "\n")
	age := ""
	if m.loaded && !m.data.updated.IsZero() {
		age = "  " + core.Dim.Render("· mis à jour "+ageText(m.data.updated))
		if time.Since(m.data.updated) > 2*time.Minute {
			age = "  " + core.Yellow.Render("· mis à jour "+ageText(m.data.updated))
		}
	}
	sb.WriteString(" " + core.Dim.Render(shorten(m.sel.Path, m.w-2-lipgloss.Width(age))) + age + "\n")
	if !m.loaded {
		return sb.String() + "\n " + core.Dim.Render("préparation en cours…"), -1
	}

	// légende : les icônes de la ligne du navigateur, dans le même ordre
	g := m.data.git
	inner := w - 4 // carte encadrée : bordure et marge de chaque côté
	if m.narrow() {
		inner = w - 1
	}
	pl := legendLines(projInfo{open: m.panes.open, git: g, gitOK: true, tpl: m.data.tpl,
		tplTag: m.data.tag, tplLatest: core.ReadTrim(core.TplVersionPath()), ms: m.ms, sess: m.panes.sess, hasSess: m.panes.hasS,
		dot: sessState(m.sel.Dot), dotWhy: m.sel.DotWhy}, inner)
	if !g.IsRepo {
		pl = append(pl, core.Dim.Render("pas un dépôt git"))
	} else {
		d := m.data.detail
		pl = append(pl, "", kv("branche", g.Branch))
		pl = append(pl, kv("version", "locale  "+core.OrNone(d.VerLocal)))
		pl = append(pl, kv("", "release "+core.ReleaseText(m.ms, d.VerRelease)))
		pl = append(pl, kv("dernier", shorten(d.LastMsg, w-16)))
	}
	sb.WriteString(m.card("Projet", strings.Join(pl, "\n"), w) + "\n")
	if l := core.MsLines(m.ms, inner); l != nil {
		title := "Milestone"
		if !m.ms.At.IsZero() {
			title += " · lu " + ageText(m.ms.At)
		}
		sb.WriteString(m.card(title, strings.Join(l, "\n"), w) + "\n")
	}

	// la liste commence sous le cadre (bordure + bandeau) ou sous le bandeau seul
	top := strings.Count(sb.String(), "\n") + 2
	if m.narrow() {
		top--
	}
	sb.WriteString(m.card(m.teamTitle(), m.teamBody(w), w) + "\n")
	return sb.String(), top
}

func (m stateModel) teamTitle() string {
	n := 0
	for _, l := range m.panes.lines {
		if l.Started && l.Name != "leader" {
			n++
		}
	}
	if n == 0 {
		return "Team"
	}
	t := fmt.Sprintf("Team · %d teammates", n)
	if v := m.teamClaude(); v != "" && m.narrow() {
		t += " · claude " + v
	}
	return t
}

// teamClaude : version de claude commune aux teammates lancés ("" si aucun
// ou si elles diffèrent). En pane étroit, elle passe dans le titre.
func (m stateModel) teamClaude() string {
	v := ""
	for _, t := range m.panes.lines {
		switch {
		case t.Claude == "":
		case v == "":
			v = t.Claude
		case v != t.Claude:
			return ""
		}
	}
	return v
}

// shortEffort : effort abrégé pour le pane étroit.
func shortEffort(e string) string {
	switch e {
	case "medium":
		return "med"
	case "xhigh":
		return "xhi"
	}
	return e
}

var teamShown = lipgloss.NewStyle().Bold(true) // nom d'un pane visible : surbrillance

// teamState : état d'un teammate lancé, en fin de ligne.
func teamState(l core.TeamLine) string {
	switch {
	case l.Hidden:
		return core.Dim.Render("caché")
	case l.Pinned:
		return core.Cyan.Render("fixé")
	case l.Active:
		return core.Green.Render("actif")
	case l.IdleLeft > 0:
		return core.Yellow.Render(fmt.Sprintf("−%d s", l.IdleLeft))
	}
	return ""
}

// teamBody : une ligne par instance de teammate (la liste est cliquable, une
// ligne d'écran chacune) : ● lancé / ○ non lancé, surbrillance si son pane est
// visible, nom, version du template (+ si compagnon), modèle et effort, version
// de claude, état (actif, caché, fixé, délai avant masquage).
func (m stateModel) teamBody(w int) string {
	tm := m.panes.lines
	if len(tm) == 0 {
		if !m.panes.open {
			return core.Dim.Render("Pas de team template (Entrée pour lancer).")
		}
		return core.Dim.Render("Pas de team template.")
	}
	narrow, common := m.narrow(), m.teamClaude()
	inner := w - 4 // largeur utile : carte encadrée (bordure + marge), ou marge de 1 colonne
	if narrow {
		inner = w - 1
	}
	effW := 6
	if narrow {
		effW = 4
	}
	nameW := 0
	for _, t := range tm {
		nameW = max(nameW, len(t.Name))
	}
	nameW = min(nameW, max(8, w-1-2-1-9-7-effW))
	var lines []string
	for _, t := range tm {
		mark := core.Dim.Render("○")
		name := core.Dim.Render(core.PadRight(ansi.Truncate(t.Name, nameW, "…"), nameW))
		if t.Started {
			mark, name = core.Green.Render("●"), core.Dim.Render(core.PadRight(ansi.Truncate(t.Name, nameW, "…"), nameW))
			if !t.Hidden {
				name = teamShown.Render(core.PadRight(ansi.Truncate(t.Name, nameW, "…"), nameW))
			}
		}
		tag := t.Tag
		if tag == "" {
			tag = "—"
		}
		if t.Companion {
			tag += "+"
		}
		model, effort := strings.TrimPrefix(t.Model, "claude-"), t.Effort
		if narrow {
			effort = shortEffort(effort)
		}
		l := mark + " " + name + " " + core.PadRight(tag, 9) + core.PadRight(orDash(model), 7) + core.Dim.Render(core.PadRight(orDash(effort), effW))
		if t.Claude != "" && !(narrow && common != "") {
			l += " " + core.Cyan.Render(t.Claude)
		}
		if st := teamState(t); st != "" {
			l += " " + st
		}
		l = ansi.Truncate(strings.TrimRight(l, " "), inner, "…")
		lines = append(lines, l)
	}
	if m.teamMsg != "" && time.Since(m.teamMsgAt) < 6*time.Second {
		lines = append(lines, core.Yellow.Render(ansi.Truncate("⚠ "+m.teamMsg, inner, "…")))
	}
	return strings.Join(lines, "\n")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// filesRows : lignes disponibles pour la liste de fichiers (reste de l'écran).
func (m stateModel) filesRows() int {
	if !m.loaded || m.h == 0 {
		return 5
	}
	used := strings.Count(m.topCards(), "\n") + 1 // cartes du haut
	if m.narrow() {
		return max(2, m.h-used-1) // titre seul
	}
	return max(4, m.h-used-3) // bordure haute/basse + titre
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

func changeMark(c string) string {
	switch c {
	case "":
		return " "
	case "D":
		return core.Red.Render(c)
	case "?", "A":
		return core.Green.Render(c)
	}
	return core.Yellow.Render(c)
}

func (m stateModel) filesCard(w int) string {
	files := m.data.files
	rows := m.filesRows()
	title := fmt.Sprintf("Fichiers · %d", len(files))
	if len(files) > rows {
		title += fmt.Sprintf(" · %d–%d · ↑↓ ou molette", m.scroll+1, min(len(files), m.scroll+rows))
	}
	nameW := min(w-24, 44)
	var lines []string
	for i := m.scroll; i < len(files) && i < m.scroll+rows; i++ {
		f := files[i]
		name, meta := f.Name, humanSize(f.Size)
		if f.Dir {
			name = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(f.Name + "/")
			meta = fmt.Sprintf("%d élém.", f.Count)
		}
		name = ansi.Truncate(name, nameW, "…")
		lines = append(lines, changeMark(m.data.detail.Changes[f.Name])+" "+core.PadRight(name, nameW)+" "+core.Dim.Render(core.PadRight(meta, 10)))
	}
	if len(files) == 0 {
		lines = append(lines, core.Dim.Render("(vide)"))
	}
	return m.card(title, strings.Join(lines, "\n"), w)
}

// narrow : pane d'infos sous le navigateur (≈ 40 colonnes) : sections sans
// cadre, pour tenir dans la hauteur.
func (m stateModel) narrow() bool { return m.w < 60 }

// Bandeau de titre des cartes : fond sur toute la ligne, pour que chaque carte
// ressorte ; le nom en gras, la précision (« · lu il y a 18 h ») en atténué.
var (
	cardBarBg   = lipgloss.Color("237")
	cardBarName = lipgloss.NewStyle().Background(cardBarBg).Foreground(lipgloss.Color("255")).Bold(true)
	cardBarRest = lipgloss.NewStyle().Background(cardBarBg).Foreground(lipgloss.Color("248"))
)

// cardBar : « ␣NOM · précision␣… » sur w colonnes, fond cardBarBg.
func cardBar(title string, w int) string {
	name, rest, _ := strings.Cut(strings.ToUpper(title), " · ")
	s := cardBarName.Render(" " + name)
	if rest != "" {
		s += cardBarRest.Render(" · " + rest)
	}
	s = ansi.Truncate(s, w, "…")
	if d := w - lipgloss.Width(s); d > 0 {
		s += cardBarRest.Render(strings.Repeat(" ", d))
	}
	return s
}

func (m stateModel) card(title, body string, w int) string {
	if m.narrow() {
		return cardBar(title, w) + "\n" + lipgloss.NewStyle().PaddingLeft(1).Width(w).Render(body)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238"))
	return box.Render(cardBar(title, w-2) + "\n" + lipgloss.NewStyle().Padding(0, 1).Width(w-2).Render(body))
}

func kv(k, v string) string { return core.Dim.Render(core.PadRight(k, 9)) + v }

func (m stateModel) View() string {
	if m.w == 0 {
		return ""
	}
	if !m.hasSel {
		return "\n  " + core.Dim.Render("Sélectionnez un projet dans le navigateur (ci-dessus).")
	}
	out := m.topCards()
	if !m.loaded {
		return out
	}
	out = truncLines(out+m.filesCard(min(m.w-2, 110)), m.w)
	if l := strings.Split(out, "\n"); m.h > 0 && len(l) > m.h {
		out = strings.Join(l[:m.h], "\n") // jamais plus haut que le pane : l'en-tête resterait hors champ
	}
	return out
}

func RunState() error {
	go core.RunBrowserServer() // fichiers pour le navigateur (#106), avec ce pane supervisé
	_, err := tea.NewProgram(stateModel{cfg: core.LoadConfig()}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
