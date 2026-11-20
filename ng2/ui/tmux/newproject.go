package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	formCategory // catégorie : liste des dossiers de la base, « racine » ou « ＋ nouvelle » (#214)
	formNewCat   // saisie d'une nouvelle catégorie
	formName
	formSource  // source : vide, template ou dépôt git (#215)
	formURL     // URL du dépôt à cloner
	formCloning // clonage en cours (progression)
	formKind
	formRemote
	formExec // socle d'exécution : hôte ou conteneur (image)
)

type newForm struct {
	mode formMode
	root int
	base string // racine (base) ; vide tant qu'elle n'est pas choisie
	// catégorie (#214) : chemin relatif à la base (« A/B », vide = racine) ; newCat : à créer avec le projet
	category string
	newCat   bool
	cats     []core.Category
	catIdx   int // 0 : racine ; 1..len(cats) : cats[i-1] ; len(cats)+1 : « ＋ nouvelle »
	catInput string
	// source (#215) : "" ou "empty" (vide), "template", "git"
	source string
	srcIdx int // 0 vide, 1 template, 2 git
	url    string
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
// ou une racine (la racine et la catégorie en sont déduites) ; vide pour l'entrée « Nouveau projet ».
func (m model) startNewProject(base string) model {
	m.form = newForm{mode: formName}
	if base != "" {
		root, cat, ok := splitUnderRoot(m.cfg.Roots, base)
		if !ok {
			m.form = newForm{}
			m.msg = "⚠ base inconnue : " + base
			return m
		}
		m.form.base, m.form.category = root, cat
		return m
	}
	if len(m.cfg.Roots) > 1 {
		m.form.mode = formRoot
		return m
	}
	m.form.base = filepath.Clean(m.cfg.Roots[0])
	return m.enterCategory()
}

// splitUnderRoot : racine contenant dir, et chemin relatif (« / ») de dir sous cette racine.
func splitUnderRoot(roots []string, dir string) (root, cat string, ok bool) {
	d := filepath.Clean(dir)
	for _, r := range roots {
		root = filepath.Clean(r)
		if d == root {
			return root, "", true
		}
		if rel, err := filepath.Rel(root, d); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return root, filepath.ToSlash(rel), true
		}
	}
	return "", "", false
}

// enterCategory : étape « catégorie » de la base choisie.
func (m model) enterCategory() model {
	cats, err := core.ListCategoriesIn(m.cfg.Roots, m.form.base)
	if err != nil {
		m.form.err = err.Error()
		return m
	}
	m.form.cats, m.form.catIdx, m.form.mode = cats, 0, formCategory
	return m
}

func (m model) onFormKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	switch k.String() {
	case "esc", "ctrl+c":
		if f.mode == formCloning {
			if m.job != nil {
				m.job.cancelNow() // l'arrêt se termine par cloneDoneMsg (nettoyage)
			}
			return m, nil
		}
		m.form = newForm{}
	case "up":
		switch f.mode {
		case formRoot:
			f.root = max(0, f.root-1)
		case formCategory:
			f.catIdx = max(0, f.catIdx-1)
		case formSource:
			f.srcIdx = max(0, f.srcIdx-1)
		case formKind:
			f.raw = false
		case formExec:
			f.exec = max(0, f.exec-1)
		}
	case "down":
		switch f.mode {
		case formRoot:
			f.root = min(len(m.cfg.Roots)-1, f.root+1)
		case formCategory:
			f.catIdx = min(len(f.cats)+1, f.catIdx+1)
		case formSource:
			f.srcIdx = min(2, f.srcIdx+1)
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
			return m.enterCategory(), nil
		case formCategory:
			switch {
			case f.catIdx == 0:
				f.category, f.newCat, f.mode = "", false, formName
			case f.catIdx > len(f.cats):
				f.catInput, f.mode = "", formNewCat
			default:
				f.category, f.newCat, f.mode = f.cats[f.catIdx-1].Path, false, formName
			}
		case formNewCat:
			cat := strings.TrimSpace(f.catInput)
			p, err := core.ResolveCategoryPathIn(m.cfg.Roots, f.base, cat)
			if err != nil {
				f.err = err.Error()
				break
			}
			if st, serr := os.Stat(p); serr == nil && !st.IsDir() {
				f.err = "un fichier porte déjà ce nom"
				break
			}
			_, serr := os.Stat(p)
			f.category, f.newCat, f.mode = cat, serr != nil, formName
		case formName:
			if p, err := core.ResolveProjectPathIn(m.cfg.Roots, f.base, f.category, f.name); err != nil {
				f.err = err.Error()
			} else if _, err := os.Stat(p); err == nil {
				f.err = "ce dossier existe déjà"
			} else {
				f.mode = formSource
			}
		case formSource:
			return m.chooseSource(f.srcIdx)
		case formURL:
			if _, err := core.ValidateCloneURL(f.url); err != nil {
				f.err = err.Error()
			} else if mm, ok := m.execStep(); ok {
				return mm, nil
			} else {
				return m.createProject()
			}
		case formKind:
			if f.raw {
				if mm, ok := m.execStep(); ok {
					return mm, nil
				}
				return m.createProject()
			}
			f.mode = formRemote
		case formRemote:
			if mm, ok := m.execStep(); ok {
				return mm, nil
			}
			return m.createProject()
		case formExec:
			if f.existing != "" {
				return m.applyExecExisting(), ctrNowCmd(m.projectPaths(), 0)
			}
			return m.createProject()
		}
	case "backspace":
		switch f.mode {
		case formName:
			f.name = dropLast(f.name)
		case formNewCat:
			f.catInput = dropLast(f.catInput)
		case formURL:
			f.url = dropLast(f.url)
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
		case formNewCat:
			f.catInput += s
		case formURL:
			f.url += s
		case formSource: // v / t / g choisissent et valident
			switch strings.ToLower(s) {
			case "v":
				return m.chooseSource(0)
			case "t":
				return m.chooseSource(1)
			case "g":
				return m.chooseSource(2)
			}
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
				return m.createProject()
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

// where : « dans EMBY » — la base et la catégorie, relatives à la racine.
func (m model) where() string {
	root := filepath.Base(m.form.base)
	if m.form.category != "" {
		return root + "/" + m.form.category
	}
	return root
}

// chooseSource : source choisie (0 vide, 1 template, 2 dépôt git) et étape suivante.
func (m model) chooseSource(i int) (tea.Model, tea.Cmd) {
	f := &m.form
	f.srcIdx = i
	f.err = ""
	switch i {
	case 1:
		f.source, f.raw, f.mode = "template", false, formRemote
	case 2:
		f.source, f.mode = "git", formURL
	default:
		f.source, f.mode = "empty", formKind
	}
	return m, nil
}

// cloneJob : clonage de la coque en cours (progression lue par le rendu).
type cloneJob struct {
	mu     sync.Mutex
	step   string
	pct    int
	cancel context.CancelFunc
	undo   func() string
	dir    string
}

func (j *cloneJob) set(step string, pct int) {
	j.mu.Lock()
	j.step, j.pct = step, pct
	j.mu.Unlock()
}

func (j *cloneJob) get() (string, int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.step, j.pct
}

func (j *cloneJob) cancelNow() {
	if j.cancel != nil {
		j.cancel()
	}
}

type cloneTickMsg struct{}

// cloneDoneMsg : fin du clonage (err nil : réussi ; context.Canceled : annulé).
type cloneDoneMsg struct {
	job *cloneJob
	err error
}

func cloneTickCmd() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return cloneTickMsg{} })
}

// createProject : crée le projet (vide, template) ou lance le clonage (git, asynchrone : cloneDoneMsg).
func (m model) createProject() (tea.Model, tea.Cmd) {
	f := m.form
	// règle commune appliquée au moment de créer
	dir, undo, err := core.CreateProjectDir(m.cfg.Roots, f.base, f.category, f.newCat, f.name)
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	fail := func(msg string) (tea.Model, tea.Cmd) { // ce que cet appel a créé (projet, catégories vides) : supprimé
		m.form.err = msg + " — " + undo()
		return m, nil
	}
	if f.source == "git" { // l'affectation (applyExec) vient APRÈS un clonage réussi, dans cloneDone
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		job := &cloneJob{step: "préparation", cancel: cancel, undo: undo, dir: dir}
		m.job = job
		m.form.mode, m.form.err = formCloning, ""
		auth := core.CloneAuth{Token: m.cfg.GithubToken, Backend: f.ctrBe}
		url := f.url
		run := func() tea.Msg {
			defer cancel()
			return cloneDoneMsg{job: job, err: core.CloneProject(ctx, url, dir, auth, job.set)}
		}
		return m, tea.Batch(run, cloneTickCmd())
	}
	if err := applyExec(dir, m.form); err != nil {
		return fail("env.yaml : " + err.Error())
	}
	mode := launchNormal // ni /init-project d'office ni question ; brut seulement pour le projet sans git
	if f.raw && f.source != "template" {
		mode = launchNewRaw
	}
	if err := core.InitProject(dir, f.raw && f.source != "template", f.remote); err != nil {
		return fail(err.Error())
	}
	if f.source == "template" { // même marqueur que la fenêtre native : proposé à l'ouverture (launchProject), jamais d'office
		if err := core.MarkInitProposed(dir); err != nil {
			return fail("marqueur /init-project : " + err.Error())
		}
	}
	return m.finishCreated(dir, mode), nil
}

// finishCreated : formulaire fermé, arbre relu, projet sélectionné et ouvert.
func (m model) finishCreated(dir string, mode launchMode) model {
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

// cloneDone : fin du clonage — projet ouvert, ou échec avec nettoyage de ce que cet appel a créé.
func (m model) cloneDone(msg cloneDoneMsg) model {
	j := msg.job
	m.job = nil
	switch {
	case msg.err == nil:
		if err := applyExec(j.dir, m.form); err != nil { // après le clonage réussi : jamais d'affectation sans projet cloné
			m.form.err = "env.yaml : " + err.Error() + " — " + j.undo()
			m.form.mode = formURL
			return m
		}
		return m.finishCreated(j.dir, launchNormal)
	case errors.Is(msg.err, context.Canceled):
		m.form.err = "annulé — " + j.undo()
	default:
		m.form.err = msg.err.Error() + " — " + j.undo()
	}
	m.form.mode = formURL // l'URL reste modifiable pour une nouvelle tentative
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
	} else if f.mode == formCategory || f.mode == formNewCat {
		sb.WriteString(" " + core.Dim.Render("base ") + core.Cyan.Render(shorten(filepath.Base(f.base), w-9)) + "\n\n")
	} else {
		in := m.where()
		if f.newCat {
			in += " (nouvelle)"
		}
		sb.WriteString(" " + core.Dim.Render("dans ") + core.Cyan.Render(shorten(in, w-8)) + "\n\n")
	}
	if f.mode == formCategory {
		sb.WriteString(" " + core.Dim.Render("Catégorie :") + "\n")
		items := append([]string{"(à la racine)"}, func() []string {
			var l []string
			for _, c := range f.cats {
				l = append(l, fmt.Sprintf("%s  (%d)", c.Path, c.Projects))
			}
			return l
		}()...)
		items = append(items, "＋ nouvelle catégorie…")
		for i, o := range items {
			mark, style := "  ", core.Dim
			if i == f.catIdx {
				mark, style = core.Green.Render("▶ "), core.Bold
			}
			sb.WriteString("  " + mark + style.Render(shorten(o, w-6)) + "\n")
		}
	}
	if f.mode == formNewCat {
		sb.WriteString(" " + core.Dim.Render("Nouvelle catégorie (A ou A/B) :") + " " + f.catInput + core.Dim.Render("▏") + "\n")
	}
	cur := core.Dim.Render("▏")
	if f.mode >= formName {
		c := ""
		if f.mode == formName {
			c = cur
		}
		sb.WriteString(" " + core.Dim.Render("Nom :") + " " + f.name + c + "\n")
	}
	if f.mode >= formSource {
		sb.WriteString("\n " + core.Dim.Render("Source :") + "\n")
		for i, o := range []struct{ key, label, desc string }{
			{"v", "vide", "dossier (git ou brut)"}, {"t", "template", "géré, /init-project proposé"}, {"g", "dépôt git", "cloner une URL"},
		} {
			mark, style := "  ", core.Dim
			if i == f.srcIdx {
				mark, style = core.Green.Render("▶ "), core.Bold
			}
			if f.mode == formSource || i == f.srcIdx {
				sb.WriteString("  " + mark + style.Render(o.key+" "+o.label) + core.Dim.Render("  "+o.desc) + "\n")
			}
		}
	}
	if f.mode == formURL || f.mode == formCloning || f.mode >= formKind && f.source == "git" {
		sb.WriteString(" " + core.Dim.Render("URL du dépôt :") + " " + shorten(f.url, w-18))
		if f.mode == formURL {
			sb.WriteString(cur)
		}
		sb.WriteString("\n")
	}
	if f.mode == formCloning && m.job != nil {
		step, pct := m.job.get()
		sb.WriteString(" " + core.Cyan.Render(fmt.Sprintf("clonage : %s %d %%", step, pct)) + "\n")
	}
	if f.mode >= formKind && f.source != "git" && f.source != "template" {
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
	if f.mode >= formRemote && !f.raw && f.source != "git" {
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
	case formCategory:
		hint = "↑↓ · ↵ choisir · esc annuler"
	case formKind:
		hint = "↑↓ ou g/b · ↵ valider · esc annuler"
	case formSource:
		hint = "↑↓ ou v/t/g · ↵ choisir · esc annuler"
	case formURL:
		hint = "https://… ou git@hôte:dépôt · ↵ · esc annuler"
	case formCloning:
		hint = "esc : annuler le clonage"
	case formExec:
		hint = "↑↓ 0-9 socle · ←→ backend · ↵ · esc"
	}
	sb.WriteString("\n " + core.Dim.Render(hint))
	return truncLines(sb.String(), w)
}
