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

// Vue « ◈ État » : onglet épinglé de claude-hub qui suit la sélection du
// navigateur (fichier selection.json) et montre l'état du projet sans
// l'ouvrir : git, template, panes tmux et fichiers.

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
	}
)

type paneInfo struct{ Idx, Title, Cmd, Size string }

type stateModel struct {
	w, h     int
	sel      Selection
	hasSel   bool
	data     stateData
	loaded   bool
	panes    paneData
	loadedAt time.Time
	paneAt   time.Time
	scroll   int // défilement de la liste de fichiers

	cacheMtime time.Time // mtime du fichier de cache déjà affiché
	selAt      time.Time
	fallback   bool // calcul direct si le cache n'arrive pas
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

func loadPanesCmd(s Selection) tea.Cmd {
	return func() tea.Msg {
		pd := paneData{path: s.Path}
		pd.open = tmuxWindows(hubSession)[s.Key]
		if pd.open {
			b, err := dtmux("list-panes", "-t", hubSession+":"+s.Key, "-F",
				"#{pane_index}\t#{pane_title}\t#{pane_current_command}\t#{pane_width}x#{pane_height}").Output()
			if err == nil {
				for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
					f := strings.Split(l, "\t")
					if len(f) == 4 {
						pd.panes = append(pd.panes, paneInfo{f[0], f[1], f[2], f[3]})
					}
				}
			}
		}
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
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
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
		s, ok := readSelection()
		switch {
		case ok && s.Path != m.sel.Path:
			m.sel, m.hasSel, m.scroll = s, true, 0
			m.loaded, m.cacheMtime, m.fallback = false, time.Time{}, false
			m.panes = paneData{}
			m.paneAt, m.selAt = time.Now(), time.Now()
			cmds = append(cmds, loadPanesCmd(s))
		case ok && time.Since(m.paneAt) > 2*time.Second:
			m.paneAt = time.Now()
			cmds = append(cmds, loadPanesCmd(s))
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
	pill := dim.Render("○ non lancé")
	if m.panes.open {
		pill = green.Render("● fenêtre tmux ouverte")
	}
	sb.WriteString(" " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("◈ "+m.sel.Name) + "  " + pill + "\n")
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

	g := m.data.git
	var pl []string
	if !g.IsRepo {
		pl = append(pl, dim.Render("pas un dépôt git"))
	} else {
		br := g.Branch
		if m.data.detail.Dirty {
			br += yellow.Render("  *modifié")
		}
		pl = append(pl, kv("branche", br))
		switch {
		case !g.Upstream:
			pl = append(pl, kv("amont", "aucun"))
		case g.Behind > 0:
			pl = append(pl, kv("amont", red.Render(fmt.Sprintf("en retard de %d commit(s)", g.Behind))))
		default:
			pl = append(pl, kv("amont", green.Render("à jour")))
		}
		pl = append(pl, kv("dernier", shorten(m.data.detail.LastMsg, w-16)))
	}
	tl := dim.Render("non suivi")
	have, latest := orQ(m.data.tag), orQ(readTrim(tplVersionPath()))
	switch m.data.tpl {
	case TplOK:
		tl = green.Render(have + " · à jour")
	case TplOutdated:
		tl = yellow.Render(have+" → "+latest+" disponible") + dim.Render(" (/init-project au lancement)")
	case TplMissing:
		tl = red.Render("fichiers manquants") + dim.Render(" (/init-project au lancement)")
	}
	pl = append(pl, kv("template", tl))
	sb.WriteString(card("Projet", strings.Join(pl, "\n"), w) + "\n")

	var tp []string
	if !m.panes.open {
		tp = append(tp, dim.Render("Aucune fenêtre — Entrée dans le navigateur pour lancer."))
	}
	for i, p := range m.panes.panes {
		role := "teammate"
		if i == 0 {
			role = "leader"
		}
		tp = append(tp, fmt.Sprintf("%s %s  %s  %s", dim.Render("%"+p.Idx), padRight(role, 9), padRight(p.Cmd, 10), dim.Render(p.Size)))
	}
	sb.WriteString(card("Panes tmux", strings.Join(tp, "\n"), w) + "\n")
	return sb.String()
}

// filesRows : lignes disponibles pour la liste de fichiers (reste de l'écran).
func (m stateModel) filesRows() int {
	if !m.loaded || m.h == 0 {
		return 5
	}
	used := strings.Count(m.topCards(), "\n") + 1 // cartes du haut
	return max(4, m.h-used-3)                     // bordure haute/basse + titre
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
	return card(title, strings.Join(lines, "\n"), w)
}

func card(title, body string, w int) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).
		Padding(0, 1).Width(w - 2)
	return box.Render(dim.Render(strings.ToUpper(title)) + "\n" + body)
}

func kv(k, v string) string { return dim.Render(padRight(k, 9)) + v }

func (m stateModel) View() string {
	if m.w == 0 {
		return ""
	}
	if !m.hasSel {
		return "\n  " + dim.Render("Sélectionnez un projet dans le navigateur (à gauche).")
	}
	out := m.topCards()
	if !m.loaded {
		return out
	}
	return truncLines(out+m.filesCard(min(m.w-2, 110)), m.w)
}

func runState() error {
	_, err := tea.NewProgram(stateModel{}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
