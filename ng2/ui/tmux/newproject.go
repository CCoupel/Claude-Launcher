package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	formNone        formMode = iota
	formMovePending          // déplacement inachevé : reprendre ou annuler (#216)
	formRoot
	formCategory // catégorie : liste des dossiers de la base, « racine » ou « ＋ nouvelle » (#214)
	formNewCat   // saisie d'une nouvelle catégorie
	formName
	formMoveConfirm // confirmation du déplacement (#216)
	formSource      // source : vide, template ou dépôt git (#215)
	formURL         // URL du dépôt à cloner
	formCloning     // clonage en cours (progression)
	formTrust       // approbation de l'environnement d'un projet cloné (#215)
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
	// source (#215) : "" ou "empty" (Projet brut), "template" (Template projet), "git" (Dépôt Git)
	source string
	srcIdx int // 0 Projet brut, 1 Template projet, 2 Dépôt Git
	url    string
	// approbation (formTrust) : question, mode d'ouverture à reprendre, fichiers bruts affichés
	trust     *core.EnvTrustInfo
	trustPath string
	trustMode launchMode
	trustRaw  bool
	name      string
	remote    string
	err       string
	// exécution (formExec) : 0 = hôte, i = images[i-1] dans le backend ctrBe
	exec   int
	images []string
	ctrBe  string
	// déplacement (#216) : projet source et ses informations
	moveFrom string
	moveInfo *core.MoveInfo
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
	if !ok { // pas de backend conteneur : F2 ne propose plus que « Déplacer / renommer » (#216)
		if openNext {
			m.msg = "F2 : aucun backend conteneur disponible (claude-launcher-ng2 backends)"
			return m
		}
		return m.startMove(n.Path, n.Name)
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
	if f.mode == formTrust {
		return m.onTrustKey(k)
	}
	if f.mode == formMoveConfirm || f.mode == formMovePending {
		return m.onMoveKey(k)
	}
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
			if f.moveFrom != "" { // déplacement : destination contrôlée, puis confirmation
				if _, err := m.moveTarget(); err != nil {
					f.err = err.Error()
				} else {
					f.mode = formMoveConfirm
				}
				break
			}
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
		case formRemote:
			if why := core.ValidateRemote(f.remote); why != "" {
				f.err = why
				break
			}
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
			case "b":
				return m.chooseSource(0)
			case "t":
				return m.chooseSource(1)
			case "g":
				return m.chooseSource(2)
			}
		case formExec: // un chiffre choisit (0 : hôte) ; r : revoir l'environnement d'un dépôt cloné (F2)
			if strings.ToLower(s) == "m" && f.existing != "" { // m : déplacer / renommer le projet (#216)
				return m.startMove(f.existing, f.name), nil
			}
			if strings.ToLower(s) == "r" && f.existing != "" {
				if err := core.ReviewEnvTrust(f.existing); err != nil {
					f.err = err.Error()
				} else {
					f.err = ""
					m.msg = "↻ " + f.name + " : l'environnement du dépôt sera redemandé à la prochaine ouverture"
					m.form = newForm{}
				}
				break
			}
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
		f.source, f.mode = "template", formRemote
	case 2:
		f.source, f.mode = "git", formURL
	default:
		f.source, f.mode = "empty", formRemote // Projet brut : dossier + git init, remote seulement si une URL est fournie
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
		if err := core.RegisterClone(dir, f.url, m.formImage()); err != nil { // posée AVANT le clonage (fermé si le registre est illisible)
			cancel()
			return fail("registre d'approbation : " + err.Error())
		}
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
	mode := launchNormal // jamais de /init-project d'office : le template le propose à l'ouverture (marqueur)
	if err := core.InitProject(dir, false, f.remote); err != nil {
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
		// l'état (none / pending) est décidé sur le contenu du DÉPÔT seul, AVANT applyExec : l'env.yaml écrit ensuite est
		// celui du launcher (empreinte re-scellée), jamais un « pending » sur son propre fichier
		if _, err := core.MarkEnvPending(j.dir, m.form.url, m.formImage()); err != nil {
			m.form.err = "registre d'approbation : " + err.Error() + " — " + j.undo()
			m.form.mode = formURL
			return m
		}
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
	if f.mode == formTrust && f.trust != nil {
		return m.renderTrust(w)
	}
	if f.mode == formMoveConfirm || f.mode == formMovePending {
		return m.renderMove(w)
	}
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
		sb.WriteString("\n " + core.Dim.Render("↑↓ 0-9 socle · ←→ backend · ↵ · m déplacer / renommer · esc"))
		return truncLines(sb.String(), w)
	}
	title := "✦ Nouveau projet"
	if f.moveFrom != "" {
		title = "⇄ Déplacer / renommer " + core.SafeTerminalText(f.moveInfo.Name)
	}
	sb.WriteString(" " + core.Cyan.Render(title) + "\n\n")
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
			{"b", "Projet brut", "dossier et git init (remote si URL fournie), sans /init-project"}, {"t", "Template projet", "comme brut, /init-project proposé à l'ouverture"}, {"g", "Dépôt Git", "cloner une URL"},
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
	if f.mode == formURL || f.mode == formCloning || f.mode >= formRemote && f.source == "git" {
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
	if f.mode >= formRemote && f.source != "git" {
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
	case formSource:
		hint = "↑↓ ou b/t/g · ↵ choisir · esc annuler"
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

// formImage : socle choisi dans le formulaire ("" : hôte).
func (m model) formImage() string {
	f := m.form
	if f.exec > 0 && f.exec <= len(f.images) {
		return f.images[f.exec-1]
	}
	return ""
}

// startTrust : écran d'approbation de l'environnement d'un projet cloné, avant toute ouverture en conteneur.
func (m model) startTrust(n core.Node, mode launchMode) model {
	info, err := core.EnvTrustInfoFor(n.Path)
	if err != nil {
		m.msg = "⚠ " + err.Error()
		return m
	}
	m.form = newForm{mode: formTrust, trust: &info, trustPath: n.Path, trustMode: mode, name: n.Name}
	return m
}

// onTrustKey : a accepter (définitif), s ouvrir sans (socle choisi seul), Échap annuler, v fichiers bruts.
func (m model) onTrustKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	answer := func(choice string) (tea.Model, tea.Cmd) {
		retry, err := core.AnswerEnvTrust(f.trustPath, choice, f.trust.SHA)
		if retry { // fichiers modifiés pendant la question : relue
			if info, ierr := core.EnvTrustInfoFor(f.trustPath); ierr == nil {
				f.trust = &info
			}
			f.err = "l'environnement a encore changé : relire puis choisir"
			return m, nil
		}
		if err != nil {
			f.err = err.Error()
			return m, nil
		}
		mode := f.trustMode
		m.form = newForm{}
		return m.openSelectedAs(mode), nil
	}
	switch strings.ToLower(k.String()) {
	case "a":
		return answer("accept")
	case "s":
		return answer("ignore")
	case "v":
		f.trustRaw = !f.trustRaw
	case "esc", "ctrl+c":
		m.form = newForm{} // annuler : rien n'est écrit, rien n'est ouvert
	}
	return m, nil
}

// wrapText : s coupé en lignes d'au plus w colonnes (jamais tronqué : une commande exécutée se lit en entier).
func wrapText(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		r := []rune(line)
		if len(r) == 0 {
			out = append(out, "")
		}
		for len(r) > 0 {
			n := min(len(r), w)
			out = append(out, string(r[:n]))
			r = r[n:]
		}
	}
	return out
}

// renderTrust : la question — ce que le dépôt demande, le risque, les fichiers bruts (v). Tout texte du dépôt passe
// par core.SafeTerminalText (jamais de séquence de terminal) puis est RETOURNÉ à la ligne, jamais tronqué.
func (m model) renderTrust(w int) string {
	f := m.form
	t := f.trust
	var sb strings.Builder
	put := func(indent, text string) {
		for _, l := range wrapText(core.SafeTerminalText(text), w-len([]rune(indent))-1) {
			sb.WriteString(indent + l + "\n")
		}
	}
	title := "⚠ Environnement d'un dépôt cloné : " + core.SafeTerminalText(f.name)
	sb.WriteString(" " + core.Orange.Render(title) + "\n")
	if t.Changed {
		sb.WriteString(" " + core.Red.Render("l'environnement du dépôt a changé depuis l'approbation") + "\n")
	}
	if t.Broken != "" {
		for _, l := range wrapWords(t.Broken, w-2) {
			sb.WriteString(" " + core.Red.Render(l) + "\n")
		}
	}
	sb.WriteString(" " + core.Dim.Render("source :") + "\n")
	put("  ", t.Source)
	sb.WriteString("\n")
	if f.trustRaw {
		for _, name := range []string{"env.yaml", "env-template.yaml"} {
			if txt := t.Files[name]; txt != "" {
				sb.WriteString(" " + core.Cyan.Render(name) + "\n")
				put("  ", txt)
			}
		}
	} else {
		sp := t.Spec
		list := func(label string, items []string) {
			if len(items) > 0 {
				sb.WriteString(" " + core.Dim.Render(label) + "\n")
				for _, it := range items {
					put("   ", it)
				}
			}
		}
		sb.WriteString(" " + core.Dim.Render("image :") + "\n")
		put("   ", sp.Image+"   (choisie à la création : "+core.OrQ(t.ChosenImage)+")")
		list("paquets :", sp.Packages)
		var byMgr []string
		for mgr, l := range sp.PackagesByManager {
			byMgr = append(byMgr, mgr+" : "+strings.Join(l, " "))
		}
		sort.Strings(byMgr)
		list("paquets par gestionnaire :", byMgr)
		list("setup (exécuté une fois) :", sp.Setup)
		list("on_start (exécuté à chaque démarrage) :", sp.OnStart)
		list("watch (setup rejoué si ces fichiers changent) :", sp.Watch)
		list("local (dossiers privés au conteneur) :", sp.Local)
		if sp.Prefer != "" {
			sb.WriteString(" " + core.Dim.Render("prefer :") + "\n")
			put("   ", sp.Prefer)
		}
		var mts []string
		for _, mt := range sp.Mounts {
			mode := "lecture seule"
			if !mt.RO {
				mode = "ÉCRITURE"
			}
			mts = append(mts, mt.Path+"  ("+mode+")")
		}
		list("montages :", mts)
		r := t.Risk
		sb.WriteString("\n")
		for _, l := range wrapWords(fmt.Sprintf("risque : %d commande(s) exécutée(s), %d montage(s) hors du projet, %d paquet(s), %d watch, %d local",
			r.Exec, r.HostMounts, r.Packages, r.Watch, r.Local), w-2) {
			sb.WriteString(" " + core.Red.Render(l) + "\n")
		}
		if len(r.Scripts) > 0 { // r2 : scripts du dépôt appelés par setup / on_start, hors de l'approbation
			sb.WriteString(" " + core.Dim.Render("scripts du dépôt appelés :") + "\n")
			for _, sc := range r.Scripts {
				put("   ", sc)
			}
			for _, l := range wrapWords(t.ScriptNote, w-2) {
				sb.WriteString(" " + core.Red.Render(l) + "\n")
			}
		}
	}
	if f.err != "" {
		sb.WriteString("\n " + core.Red.Render("✗ "+f.err) + "\n")
	}
	sb.WriteString("\n " + core.Dim.Render("a accepter · s ouvrir sans · v fichiers bruts · Échap annuler"))
	return truncLines(sb.String(), w)
}
