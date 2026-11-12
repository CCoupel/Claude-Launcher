package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Vue « ◈ État » : pane de la coque, sous le navigateur, qui suit la sélection du
// navigateur (fichier selection.json) et montre l'état du projet sans
// l'ouvrir : git, template, team (teammates, versions) et fichiers.

type (
	selTick   time.Time
	stateData struct {
		path    string
		git     GitInfo
		detail  GitDetail
		tpl     TemplateState
		files   []FileEntry
		tag     string // template installé dans le projet
		updated time.Time
	}
	paneData struct {
		path  string
		open  bool
		panes []paneInfo
		team  []teammate
		sess  Resumable // session en suspens (badge ↺)
		hasS  bool
	}
)

type paneInfo struct{ ID, PID, Cmd string }

type stateModel struct {
	cfg      Config
	w, h     int
	sel      Selection
	hasSel   bool
	data     stateData
	loaded   bool
	panes    paneData
	loadedAt time.Time
	paneAt   time.Time
	scroll   int // défilement de la liste de fichiers

	ms         MsState   // milestones du projet (cache du navigateur, milestone.go)
	msMtime    time.Time // mtime du cache des milestones déjà lu
	cacheMtime time.Time // mtime du fichier de cache déjà affiché
	selAt      time.Time
	fallback   bool      // calcul direct si le cache n'arrive pas
	navAt      time.Time // dernière vérification de la présence du navigateur
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
			if exe, err := selfExe(); err == nil && ensureLayout(exe) == nil {
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
			if exe, err := selfExe(); err == nil {
				_ = ensureLayout(exe)
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
			git:     readGit(s.Path),
			detail:  readDetail(s.Path),
			tpl:     readTemplate(s.Path, latest),
			files:   readFiles(s.Path),
			tag:     templateTag(s.Path),
			updated: time.Now(),
		}
	}
}

func loadPanesCmd(s Selection, cfg Config) tea.Cmd {
	return func() tea.Msg {
		pd := paneData{path: s.Path}
		pd.sess, pd.hasS = cfg.pendingSession(s.Path)
		pd.open = tmuxWindows(hubSession)[s.Key]
		if pd.open {
			pw := projectOf(exactW(hubSession, s.Key))
			b, err := pw.t.Output("list-panes", "-t", pw.win, "-F",
				"#{pane_id}\t#{pane_pid}\t#{pane_current_command}")
			if err == nil {
				for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
					f := strings.Split(l, "\t")
					if len(f) == 3 {
						pd.panes = append(pd.panes, paneInfo{f[0], f[1], f[2]})
					}
				}
			}
			if pw.ctrExec != nil {
				ctrPaneVersions(pw, pd.panes)
			}
		}
		pd.team = liveTeam(readTeammates(s.Path), s.Path, pd.panes)
		return pd
	}
}

func (m stateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.MouseMsg:
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
			m.ms, m.msMtime = MsState{}, time.Time{}
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
	}
	return m, nil
}

// reloadCache relit le fichier de cache du projet sélectionné s'il a changé.
func (m *stateModel) reloadCache() {
	if st, err := os.Stat(msCachePath()); err == nil && !st.ModTime().Equal(m.msMtime) {
		m.ms, m.msMtime = readMsCache()[m.sel.Path], st.ModTime()
	}
	st, err := os.Stat(statePath(m.sel.Path))
	if err != nil || (m.loaded && st.ModTime().Equal(m.cacheMtime)) {
		return
	}
	ps, mt, ok := readState(m.sel.Path)
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
func (m stateModel) topCards() string {
	w := min(m.w-2, 110)
	var sb strings.Builder
	sb.WriteString(" " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("◈ "+m.sel.Name) + "\n")
	age := ""
	if m.loaded && !m.data.updated.IsZero() {
		age = "  " + dim.Render("· mis à jour "+ageText(m.data.updated))
		if time.Since(m.data.updated) > 2*time.Minute {
			age = "  " + yellow.Render("· mis à jour "+ageText(m.data.updated))
		}
	}
	sb.WriteString(" " + dim.Render(shorten(m.sel.Path, m.w-2-lipgloss.Width(age))) + age + "\n")
	if !m.loaded {
		return sb.String() + "\n " + dim.Render("préparation en cours…")
	}

	// légende : les icônes de la ligne du navigateur, dans le même ordre
	g := m.data.git
	inner := w - 4 // carte encadrée : bordure et marge de chaque côté
	if m.narrow() {
		inner = w - 1
	}
	pl := legendLines(projInfo{open: m.panes.open, git: g, gitOK: true, tpl: m.data.tpl,
		tplTag: m.data.tag, tplLatest: readTrim(tplVersionPath()), ms: m.ms, sess: m.panes.sess, hasSess: m.panes.hasS,
		dot: sessState(m.sel.Dot), dotWhy: m.sel.DotWhy}, inner)
	if !g.IsRepo {
		pl = append(pl, dim.Render("pas un dépôt git"))
	} else {
		d := m.data.detail
		pl = append(pl, "", kv("branche", g.Branch))
		pl = append(pl, kv("version", "locale  "+orNone(d.VerLocal)))
		pl = append(pl, kv("", "release "+releaseText(m.ms, d.VerRelease)))
		pl = append(pl, kv("dernier", shorten(d.LastMsg, w-16)))
	}
	sb.WriteString(m.card("Projet", strings.Join(pl, "\n"), w) + "\n")
	if l := msLines(m.ms, inner); l != nil {
		title := "Milestone"
		if !m.ms.At.IsZero() {
			title += " · lu " + ageText(m.ms.At)
		}
		sb.WriteString(m.card(title, strings.Join(l, "\n"), w) + "\n")
	}

	sb.WriteString(m.card(m.teamTitle(), m.teamBody(w), w) + "\n")
	return sb.String()
}

func (m stateModel) teamTitle() string {
	n := len(m.panes.team)
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
	for _, t := range m.panes.team {
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

// teamBody : une ligne par teammate : nom, version du template (+ si
// compagnon), modèle et effort, version de claude s'il est lancé.
func (m stateModel) teamBody(w int) string {
	tm := m.panes.team
	if len(tm) == 0 {
		if !m.panes.open {
			return dim.Render("Pas de team template (Entrée pour lancer).")
		}
		return dim.Render("Pas de team template.")
	}
	narrow, common := m.narrow(), m.teamClaude()
	effW := 6
	if narrow {
		effW = 4
	}
	nameW, multi := 0, 0
	for _, t := range tm {
		nameW = max(nameW, len(t.Name))
		if t.Count > 1 {
			multi = 3 // « ×N » en fin de ligne
		}
	}
	// w-1 : la carte étroite garde une colonne de marge à gauche
	nameW = min(nameW, max(8, w-1-2-1-9-7-effW-multi))
	var lines []string
	for _, t := range tm {
		mark, name := dim.Render("○"), dim.Render(padRight(ansi.Truncate(t.Name, nameW, "…"), nameW))
		if t.Claude != "" {
			mark, name = green.Render("●"), padRight(ansi.Truncate(t.Name, nameW, "…"), nameW)
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
		l := mark + " " + name + " " + padRight(tag, 9) + padRight(orDash(model), 7) + dim.Render(padRight(orDash(effort), effW))
		if t.Claude != "" && !(narrow && common != "") {
			c := t.Claude
			if t.Count > 1 {
				c += fmt.Sprintf(" ×%d", t.Count)
			}
			l += " " + cyan.Render(c)
		} else if t.Count > 1 {
			l += " " + cyan.Render(fmt.Sprintf("×%d", t.Count))
		}
		lines = append(lines, strings.TrimRight(l, " "))
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
		return red.Render(c)
	case "?", "A":
		return green.Render(c)
	}
	return yellow.Render(c)
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
		lines = append(lines, changeMark(m.data.detail.Changes[f.Name])+" "+padRight(name, nameW)+" "+dim.Render(padRight(meta, 10)))
	}
	if len(files) == 0 {
		lines = append(lines, dim.Render("(vide)"))
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

func orNone(s string) string {
	if s == "" {
		return dim.Render("aucune")
	}
	return s
}

func kv(k, v string) string { return dim.Render(padRight(k, 9)) + v }

func (m stateModel) View() string {
	if m.w == 0 {
		return ""
	}
	if !m.hasSel {
		return "\n  " + dim.Render("Sélectionnez un projet dans le navigateur (ci-dessus).")
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

func runState() error {
	go runBrowserServer() // fichiers pour le navigateur (#106), avec ce pane supervisé
	_, err := tea.NewProgram(stateModel{cfg: loadConfig()}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
