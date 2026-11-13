package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// Création d'un projet, comme l'entrée __new__ du menu bash. Deux façons de la
// lancer :
//   - double-clic sur une catégorie (titre de groupe : EMBY, INFRA…) ou sur une
//     racine : le projet est créé dans ce dossier, le formulaire ne demande que
//     le nom et le remote ;
//   - l'entrée « ✦ Nouveau projet » : dans une racine (choix si plusieurs).
// Après le nom, le type de projet :
//   - géré : `git init`, remote `origin` (facultatif), puis claude démarre
//     directement sur /init-project ;
//   - brut : le dossier et un CLAUDE.md minimal, ni git ni init-project.
// Le projet est ensuite ouvert.

type formMode int

const (
	formNone formMode = iota
	formRoot
	formName
	formKind
	formRemote
	formExec // socle d'exécution : hôte ou conteneur (image)
)

type newForm struct {
	mode   formMode
	root   int
	base   string // dossier fixé (catégorie ou racine) ; vide tant que la racine n'est pas choisie
	name   string
	raw    bool // projet brut (sinon géré)
	remote string
	err    string
	// exécution (formExec) : 0 = hôte, i = images[i-1] dans le backend ctrBe
	exec     int
	images   []string
	ctrBe    string
	existing string // F2 ou première ouverture : dossier du projet existant (pas de création)
	openNext bool   // première ouverture : lancer le projet une fois configuré
}

// execStep : formulaire suivant l'étape « type » ou « remote » : le choix du
// socle s'il existe un backend conteneur, sinon la création directe.
func (m model) execStep() (model, bool) {
	f, ok := execForm(m.form)
	if !ok {
		return m, false
	}
	m.form = f
	return m, true
}

// execForm : étape « Exécution » présélectionnée sur defaults: de backends.yaml
// (mode host → « hôte » ; backend conteneur → son socle defaults.image).
func execForm(f newForm) (newForm, bool) {
	b, ok := core.DefaultCtrBackend()
	if !ok {
		return f, false
	}
	defBe, defImg := core.ExecDefaults()
	images := core.BaseImages()
	idx := 0
	if defImg != "" {
		idx = -1
		for i, im := range images {
			if im == defImg {
				idx = i
			}
		}
		if idx < 0 { // socle par défaut hors liste : proposé en tête
			images, idx = append([]string{defImg}, images...), 0
		}
	}
	f.mode, f.images, f.ctrBe = formExec, images, b.Name
	f.exec = 0
	if defBe != "" && defBe != "host" && defBe == b.Name {
		f.exec = idx + 1
	}
	return f, true
}

// cycleCtrBe (←→) : backend conteneur suivant / précédent.
func (f *newForm) cycleCtrBe(d int) {
	cs := core.CtrBackends()
	for i, b := range cs {
		if b.Name == f.ctrBe {
			f.ctrBe = cs[(i+d+len(cs))%len(cs)].Name
			return
		}
	}
}

// startExecChoice : configuration d'un projet existant — F2, ou sa première
// ouverture (openNext). Préremplie avec son état : socle de son env.yaml et
// backend affecté, ou « hôte » ; sinon les défauts de backends.yaml.
func (m model) startExecChoice(n core.Node, openNext bool) model {
	f, ok := execForm(newForm{existing: n.Path, name: n.Name, openNext: openNext})
	if !ok {
		m.msg = "F2 : aucun backend conteneur disponible (claude-launcher-ng2 backends)"
		return m
	}
	if core.Assigned(n.Path) || core.HasEnv(n.Path) {
		b := core.ProjectBackend(n.Path)
		f.exec = 0
		if _, ok := core.EngineOf(b); ok {
			f.ctrBe = b.Name
		}
		if e, err := core.LoadEnv(n.Path); err == nil {
			i := indexOf(f.images, e.Image)
			if i < 0 {
				f.images, i = append([]string{e.Image}, f.images...), 0
			}
			if _, ok := core.EngineOf(b); ok {
				f.exec = i + 1
			}
		}
	}
	m.form = f
	return m
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// applyExec : affectation selon le choix (« host » compris) ; un socle crée
// env.yaml, ou change sa ligne image: s'il existe déjà (hôte : env.yaml gardé).
// Le socle d'env-template.yaml choisi tel quel : env.yaml ni créé ni modifié.
func applyExec(dir string, f newForm) error {
	img := ""
	if f.exec > 0 && f.exec <= len(f.images) {
		img = f.images[f.exec-1]
	}
	return core.ApplyExec(dir, img, f.ctrBe)
}

// withSpecial place l'entrée « Nouveau projet » en tête de la liste.
func withSpecial(nodes []core.Node) []core.Node {
	return append([]core.Node{{Name: "✦ Nouveau projet", Special: "new"}}, nodes...)
}

// startNewProject : base est le dossier choisi par double-clic sur une catégorie
// ou une racine ; vide pour l'entrée « Nouveau projet ».
func (m model) startNewProject(base string) model {
	m.form = newForm{mode: formName, base: base}
	if base == "" {
		if len(m.cfg.Roots) > 1 {
			m.form.mode = formRoot
		} else {
			m.form.base = filepath.Clean(m.cfg.Roots[0])
		}
	}
	return m
}

func (m model) onFormKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	switch k.String() {
	case "esc", "ctrl+c":
		m.form = newForm{}
	case "up":
		switch f.mode {
		case formRoot:
			f.root = max(0, f.root-1)
		case formKind:
			f.raw = false
		case formExec:
			f.exec = max(0, f.exec-1)
		}
	case "down":
		switch f.mode {
		case formRoot:
			f.root = min(len(m.cfg.Roots)-1, f.root+1)
		case formKind:
			f.raw = true
		case formExec:
			f.exec = min(len(f.images), f.exec+1)
		}
	case "left", "right":
		if f.mode == formExec {
			d := 1
			if k.String() == "left" {
				d = -1
			}
			f.cycleCtrBe(d)
		}
	case "enter":
		f.err = ""
		switch f.mode {
		case formRoot:
			f.base = filepath.Clean(m.cfg.Roots[f.root])
			f.mode = formName
		case formName:
			if err := validName(f.name); err != "" {
				f.err = err
			} else if _, err := os.Stat(m.projectDir()); err == nil {
				f.err = "ce dossier existe déjà"
			} else {
				f.mode = formKind
			}
		case formKind:
			if f.raw {
				if mm, ok := m.execStep(); ok {
					return mm, nil
				}
				return m.createProject(), nil
			}
			f.mode = formRemote
		case formRemote:
			if mm, ok := m.execStep(); ok {
				return mm, nil
			}
			return m.createProject(), nil
		case formExec:
			if f.existing != "" {
				return m.applyExecExisting(), ctrNowCmd(m.projectPaths(), 0)
			}
			return m.createProject(), nil
		}
	case "backspace":
		switch f.mode {
		case formName:
			f.name = dropLast(f.name)
		case formRemote:
			f.remote = dropLast(f.remote)
		}
	default:
		if k.Type != tea.KeyRunes {
			break
		}
		s := string(k.Runes)
		switch f.mode {
		case formRoot: // un chiffre choisit la racine
			if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(m.cfg.Roots) {
				f.root = int(s[0] - '1')
			}
		case formName:
			f.name += s
		case formKind: // g / b choisissent et valident
			switch strings.ToLower(s) {
			case "g":
				f.raw = false
				f.mode = formRemote
			case "b":
				f.raw = true
				if mm, ok := m.execStep(); ok {
					return mm, nil
				}
				return m.createProject(), nil
			}
		case formExec: // un chiffre choisit (0 : hôte)
			if len(s) == 1 && s[0] >= '0' && int(s[0]-'0') <= len(f.images) {
				f.exec = int(s[0] - '0')
			}
		case formRemote:
			f.remote += s
		}
	}
	return m, nil
}

func dropLast(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func validName(s string) string { return core.ValidProjectName(s) }

func (m model) projectDir() string {
	return filepath.Join(m.form.base, strings.TrimSpace(m.form.name))
}

// where : « dans EMBY » — le dossier fixé, relatif à sa racine.
func (m model) where() string {
	base := m.form.base
	for _, r := range m.cfg.Roots {
		root := filepath.Clean(r)
		if rel, err := filepath.Rel(root, base); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return filepath.Base(root)
			}
			return rel
		}
	}
	return base
}

func (m model) createProject() model {
	dir := m.projectDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.form.err = err.Error()
		return m
	}
	if err := applyExec(dir, m.form); err != nil {
		m.form.err = "env.yaml : " + err.Error()
		return m
	}
	mode := launchNewManaged
	if m.form.raw {
		mode = launchNewRaw
	}
	if err := core.InitProject(dir, m.form.raw, m.form.remote); err != nil {
		m.form.err = err.Error()
		return m
	}
	m.form = newForm{}
	m.filter = ""
	m.nodes = withSpecial(core.ScanTree(m.cfg.Roots))
	if m.mgr != nil {
		m.mgr.SetProjects(m.projectPaths())
	}
	m.refilter()
	for i, idx := range m.visible {
		if m.nodes[idx].Path == dir {
			m.cursor = i
		}
	}
	m = m.openSelectedAs(mode) // comme le bash : le projet créé est lancé aussitôt
	if m.msg == "" {
		m.msg = "✓ projet créé"
	}
	return m
}

// applyExecExisting : fin du choix F2 sur un projet existant.
func (m model) applyExecExisting() model {
	f := m.form
	if err := applyExec(f.existing, f); err != nil {
		m.form.err = err.Error()
		return m
	}
	m.form = newForm{}
	if f.exec == 0 {
		m.msg = f.name + " : exécution sur l'hôte"
	} else {
		m.msg = "⬡ " + f.name + " : " + core.ImageLabel(f.images[f.exec-1]) + " sur " + f.ctrBe + " (F2 pour reconfigurer)"
	}
	if f.openNext {
		for i, idx := range m.visible {
			if m.nodes[idx].Path == f.existing {
				m.cursor = i
			}
		}
		msg := m.msg
		if m = m.openSelectedAs(launchNormal); m.msg == "" {
			m.msg = msg
		}
	}
	return m
}

// minimalClaudeMD : contenu du CLAUDE.md d'un projet brut.
func minimalClaudeMD(name string) string { return core.MinimalClaudeMD(name) }

// renderExec : liste des socles (0 : hôte).
func (m model) renderExec(w int) string {
	f := m.form
	var sb strings.Builder
	sb.WriteString(" " + core.Dim.Render("Exécution :") + "\n")
	opts := append([]string{"hôte (cette machine)"}, f.images...)
	for i, o := range opts {
		mark, style := "  ", core.Dim
		if i == f.exec {
			mark, style = core.Green.Render("▶ "), core.Bold
		}
		label := o
		if i > 0 {
			label = core.ImageLabel(o)
		}
		sb.WriteString("  " + mark + style.Render(fmt.Sprintf("%d %s", i, shorten(label, w-9))) + "\n")
	}
	if f.exec > 0 {
		sb.WriteString(" " + core.Dim.Render("conteneur sur ") + core.Cyan.Render(f.ctrBe) + core.Dim.Render(shorten("  ←→ autre backend", w-16-len(f.ctrBe))) + "\n")
	}
	return sb.String()
}

func (m model) renderForm(w int) string {
	f := m.form
	var sb strings.Builder
	if f.existing != "" {
		sb.WriteString(" " + core.Cyan.Render("⬢ Exécution de "+f.name) + "\n")
		if f.openNext {
			sb.WriteString(" " + core.Dim.Render("projet pas encore configuré : choisir où il tourne,") + "\n " + core.Dim.Render("puis ↵ l'ouvre") + "\n")
		}
		sb.WriteString("\n")
		sb.WriteString(m.renderExec(w))
		if f.err != "" {
			sb.WriteString("\n " + core.Red.Render("✗ "+f.err) + "\n")
		}
		sb.WriteString("\n " + core.Dim.Render("↑↓ 0-9 socle · ←→ backend · ↵ · esc"))
		return truncLines(sb.String(), w)
	}
	sb.WriteString(" " + core.Cyan.Render("✦ Nouveau projet") + "\n\n")
	if f.mode == formRoot {
		sb.WriteString(" " + core.Dim.Render("Dossier racine :") + "\n")
		for i, r := range m.cfg.Roots {
			mark, style := "  ", core.Dim
			if i == f.root {
				mark, style = core.Green.Render("▶ "), core.Bold
			}
			sb.WriteString("  " + mark + style.Render(fmt.Sprintf("%d %s", i+1, shorten(r, w-9))) + "\n")
		}
	} else {
		sb.WriteString(" " + core.Dim.Render("dans ") + core.Cyan.Render(shorten(m.where(), w-8)) + "\n\n")
	}
	cur := core.Dim.Render("▏")
	if f.mode >= formName {
		c := ""
		if f.mode == formName {
			c = cur
		}
		sb.WriteString(" " + core.Dim.Render("Nom :") + " " + f.name + c + "\n")
	}
	if f.mode >= formKind {
		sb.WriteString("\n " + core.Dim.Render("Type de projet :") + "\n")
		for _, k := range []struct {
			raw        bool
			key, label string
			desc       string
		}{
			{false, "g", "géré", "git + /init-project"},
			{true, "b", "brut", "dossier + CLAUDE.md"},
		} {
			mark, style := "  ", core.Dim
			if k.raw == f.raw {
				mark, style = core.Green.Render("▶ "), core.Bold
			}
			if f.mode == formKind || k.raw == f.raw {
				sb.WriteString("  " + mark + style.Render(k.key+" "+k.label) + core.Dim.Render("  "+k.desc) + "\n")
			}
		}
	}
	if f.mode >= formRemote && !f.raw {
		sb.WriteString(" " + core.Dim.Render("Remote git (facultatif) :") + "\n")
		c := ""
		if f.mode == formRemote {
			c = cur
		}
		sb.WriteString("  " + f.remote + c + "\n")
	}
	if f.mode == formExec {
		sb.WriteString("\n" + m.renderExec(w))
	}
	if f.err != "" {
		sb.WriteString("\n " + core.Red.Render("✗ "+f.err) + "\n")
	}
	hint := "↵ valider · esc annuler"
	switch f.mode {
	case formRoot:
		hint = "↑↓ ou 1-9 · ↵ valider · esc annuler"
	case formKind:
		hint = "↑↓ ou g/b · ↵ valider · esc annuler"
	case formExec:
		hint = "↑↓ 0-9 socle · ←→ backend · ↵ · esc"
	}
	sb.WriteString("\n " + core.Dim.Render(hint))
	return truncLines(sb.String(), w)
}
