package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// hostRefused : exécution sur l'hôte impossible (Windows : pas de tmux local).
func hostRefused() bool { return runtime.GOOS == "windows" }

// ── Onglets ───────────────────────────────────────────────────────────────

// addTab : enregistre t, sauf si le projet a déjà un onglet (deux ouvertures
// simultanées : double-clic et Entrée, ou reattach) ; false : t n'est pas pris.
func (a *App) addTab(t *projTerm) bool {
	a.mu.Lock()
	if a.terms[t.path] != nil {
		a.mu.Unlock()
		return false
	}
	a.terms[t.path] = t
	a.openTabs.Store(t.path, true)
	a.order = append(a.order, t.path)
	a.mu.Unlock()
	a.changed()
	return true
}

// removeTab : retire l'onglet du projet path, s'il est encore celui de t
// (t nil : quel qu'il soit) — une ancienne session qui se termine ne retire
// pas l'onglet qui l'a remplacée.
func (a *App) removeTab(path string, t *projTerm) {
	a.mu.Lock()
	if cur := a.terms[path]; cur == nil || t != nil && cur != t {
		a.mu.Unlock()
		return
	}
	delete(a.terms, path)
	a.openTabs.Delete(path)
	for i, p := range a.order {
		if p == path {
			a.order = append(a.order[:i], a.order[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
	a.changed()
	a.cardsChanged()
}

// detachTab : retire l'onglet sans fermer la session (elle continue dans tmux).
func (a *App) detachTab(path string) {
	t := a.term(path)
	if t != nil {
		t.detach()
	}
	a.removeTab(path, t)
}

// open : Entrée / double-clic sur un projet. Projet déjà ouvert : son onglet
// (rouvert s'il est en lecture seule, ce qui repose la question rejoindre /
// prendre la main) ; première ouverture d'un projet jamais configuré : choix de
// l'exécution d'abord, s'il existe un backend conteneur.
func (a *App) open(path string, mode core.LaunchMode) (any, error) {
	a.mu.Lock()
	n, ok := a.node(path)
	t := a.terms[path]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("projet inconnu : %s", path)
	}
	if t != nil {
		if !t.isReadOnly() && t.info().State != "error" {
			return map[string]any{"tab": path}, nil
		}
		a.detachTab(path)
	}
	if mode == core.LaunchNormal && !core.Assigned(path) && len(core.CtrBackends()) > 0 {
		if info := a.newInfo(path); info["exec"] != nil {
			return map[string]any{"execChoice": info}, nil
		}
	}
	t = newProjTerm(a, n, mode)
	if !a.addTab(t) {
		return map[string]any{"tab": path}, nil // ouvert entre-temps par un autre appel
	}
	a.setMsg("")
	go func() {
		t.run()
		go a.readCtr()
	}()
	return map[string]any{"tab": path}, nil
}

// reattach : au démarrage, un onglet pour chaque session de projet encore
// vivante — sur l'hôte (tmux dédié), ou en conteneur si elle est à ce launcher
// ou à personne (comme la coque après un arrêt du hub).
func (a *App) reattach() {
	a.mu.Lock()
	byPath := map[string]core.Node{}
	byHost := map[string]core.Node{}
	for _, n := range a.nodes {
		if n.Project {
			byPath[n.Path] = n
			byHost[hostSession(n.Path)] = n
		}
	}
	a.mu.Unlock()
	var back []string
	if !hostRefused() {
		c := platform.NoWindow(exec.Command("tmux", "-L", deskSocket(), "list-sessions", "-F", "#{session_name}"))
		c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
		if out, err := c.Output(); err == nil {
			for _, s := range strings.Fields(string(out)) {
				if n, ok := byHost[s]; ok && core.ProjectBackend(n.Path).Name == "host" {
					if _, err := a.open(n.Path, core.LaunchNormal); err == nil {
						back = append(back, n.Name)
					}
				}
			}
		}
	}
	bs, _ := core.LoadBackends()
	for _, b := range bs {
		e, ok := core.EngineOf(b)
		if !ok || len(core.DiagnoseCached(b)) > 0 {
			continue
		}
		fs, err := e.List()
		if err != nil {
			continue
		}
		for _, f := range fs {
			n, ok := byPath[f.Project]
			if !ok || !f.Running || a.term(n.Path) != nil || core.ProjectBackend(f.Project).Name != b.Name || !e.Of(f).HasSession(f.Name) {
				continue
			}
			// seulement les sessions de ce launcher (#48) : pas celles d'une autre machine
			if o := e.Of(f).Owner(f.Name); o.Id != "" && o.Id != core.LauncherID() {
				continue
			}
			if _, err := a.open(n.Path, core.LaunchNormal); err == nil {
				back = append(back, n.Name)
			}
		}
	}
	if len(back) > 0 {
		a.setMsg("⬢ sessions retrouvées : " + strings.Join(back, ", "))
	}
}

// ── Fermeture (#50) : même plan que la coque ──────────────────────────────

type closePlan struct {
	path, name string
	question   string
	session    bool
	tab        bool
	abandon    bool
	engine     *core.Engine
	ctr        string
	host       *projTerm // projet sur l'hôte ouvert dans un onglet
}

// askClose : Ctrl+clic ou Suppr ; rend la question à confirmer (o/N), ou un
// message si rien n'est à fermer, ou lance la fermeture sans question
// (conteneur vide).
func (a *App) askClose(path string) (any, error) {
	a.mu.Lock()
	n, ok := a.node(path)
	if !ok {
		a.mu.Unlock()
		return nil, nil
	}
	if _, busy := a.closing[path]; busy {
		a.mu.Unlock()
		return map[string]any{"msg": n.Name + " : fermeture déjà en cours"}, nil
	}
	p := a.projInfo(n)
	t := a.terms[path]
	a.mu.Unlock()
	switch p.Dot {
	case core.SessReadOnly, core.SessElse:
		return map[string]any{"msg": n.Name + " : piloté par " + p.Live.Owner.Id + " — Entrée pour prendre la main, puis Ctrl+clic"}, nil
	}
	pl := closePlan{path: path, name: n.Name, tab: p.Open, session: p.Open || p.ClaudeLive()}
	if b, ok := core.FindBackend(p.Backend); ok && p.CtrKnown {
		if e, ok := core.EngineOf(b); ok {
			e = e.Of(p.Ctr)
			pl.engine, pl.ctr = &e, p.Ctr.Name
		}
	}
	if t != nil && t.host {
		pl.host = t
	}
	running := pl.engine != nil && (p.Ctr.Running || p.Ctr.Failed)
	stopText := ""
	if running {
		stopText = "conteneur arrêté"
		if pl.engine.B.K8s() {
			stopText = "pod k8s arrêté"
		}
		stopText += " (rien n'est perdu)"
	}
	var what []string
	switch p.Dot {
	case core.SessNone:
		return map[string]any{"msg": n.Name + " : rien ne tourne, rien à fermer"}, nil
	case core.SessReady:
		if !running {
			return map[string]any{"msg": n.Name + " : rien à fermer"}, nil
		}
		a.startClose(pl) // conteneur sans claude : arrêté sans question
		return map[string]any{"msg": "⏻ " + n.Name + " : arrêt du conteneur vide"}, nil
	case core.SessAnomaly:
		switch {
		case p.HasSess && !pl.session:
			pl.abandon = true
			what = append(what, "reprise abandonnée (la conversation reste dans /resume)")
		case !running:
			return map[string]any{"msg": n.Name + " : " + p.DotWhy}, nil
		}
	default:
		c := "claude"
		if tm := a.teammates(p, t); tm > 0 {
			c += fmt.Sprintf(" + %d teammate(s)", tm)
		}
		what = append(what, c)
	}
	if stopText != "" {
		what = append(what, stopText)
	}
	verb := "Fermer "
	if pl.abandon {
		verb = "Abandonner "
	}
	pl.question = verb + n.Name + " ? " + strings.Join(what, " · ") + " · o/N"
	a.mu.Lock()
	if a.plans == nil {
		a.plans = map[string]closePlan{}
	}
	a.plans[path] = pl
	a.mu.Unlock()
	return map[string]any{"question": pl.question}, nil
}

func (a *App) teammates(p core.ProjInfo, t *projTerm) int {
	n := 0
	switch {
	case p.HasLive:
		n = p.Live.Panes
	case t != nil:
		if l := t.lastLayout(); l != nil {
			n = len(l.Panes) + t.hiddenCount() // teammates cachés compris (#136)
		}
	}
	return max(n-1, 0)
}

// confirmClose : « o » à la question de fermeture.
func (a *App) confirmClose(path string) error {
	a.mu.Lock()
	pl, ok := a.plans[path]
	delete(a.plans, path)
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("aucune fermeture en attente pour %s", path)
	}
	a.startClose(pl)
	return nil
}

func (a *App) startClose(pl closePlan) {
	a.mu.Lock()
	a.closing[pl.path] = "fermeture…"
	a.msg = "⏻ " + pl.name + " : fermeture…"
	a.mu.Unlock()
	a.changed()
	go func() {
		err := a.runClose(pl, func(s string) {
			a.mu.Lock()
			a.closing[pl.path] = s
			a.msg = "⏻ " + pl.name + " : " + s
			a.mu.Unlock()
			a.changed()
		})
		a.mu.Lock()
		delete(a.closing, pl.path)
		if err != nil {
			a.msg = "⚠ " + pl.name + " : " + err.Error()
		} else {
			a.msg = "✓ " + pl.name + " fermé"
		}
		a.mu.Unlock()
		a.changed()
		go a.readCtr()
		go a.readSessions()
	}()
}

func (a *App) runClose(pl closePlan, step func(string)) error {
	if pl.abandon {
		// la conversation reste lisible (/resume) ; elle n'est plus proposée à la reprise
		m := core.CleanExitMarker(pl.path)
		if err := os.MkdirAll(filepath.Dir(m), 0o755); err == nil {
			_ = os.WriteFile(m, nil, 0o644)
			now := time.Now()
			_ = os.Chtimes(m, now, now)
		}
		step("reprise abandonnée")
	}
	if pl.session {
		switch {
		case pl.engine != nil && pl.ctr != "":
			if o := pl.engine.Owner(pl.ctr); o.Alive() && o.Id != core.LauncherID() {
				return fmt.Errorf("piloté par %s depuis la question : fermeture annulée", o.Id)
			}
			e, name := *pl.engine, pl.ctr
			t := func(args ...string) (string, error) { return e.Output(name, core.TmuxIn(args...)...) }
			if err := core.StopSession(t, "="+core.CtrSession+":", true, closeWait, step); err != nil {
				return err
			}
		case pl.host != nil:
			h := pl.host
			t := func(args ...string) (string, error) {
				b, err := h.localTmux(args...).Output()
				return strings.TrimSpace(string(b)), err
			}
			if err := core.StopSession(t, h.win, false, closeWait, step); err != nil {
				return err
			}
			_ = h.localTmux("kill-session", "-t", "="+h.sess).Run()
		}
	}
	if pl.tab {
		a.detachTab(pl.path)
	}
	if pl.engine != nil {
		step("arrêt du conteneur…")
		if err := pl.engine.Stop(pl.path, pl.ctr); err != nil {
			return err
		}
		if pl.engine.B.K8s() {
			step("pod arrêté (0 réplique)")
		} else {
			step("conteneur arrêté")
		}
	}
	return nil
}

var closeWait = 30 * time.Second

func (t *projTerm) lastLayout() *LayoutMsg {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.layout
}

// ── Création d'un projet, choix de l'exécution (F2) ───────────────────────

// newInfo : racines, socles et backends pour le formulaire de création, ou pour
// le choix de l'exécution d'un projet existant (path) prérempli avec son état.
func (a *App) newInfo(path string) map[string]any {
	out := map[string]any{"roots": a.conf().Roots}
	b, ok := core.DefaultCtrBackend()
	if !ok {
		return out
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
		if idx < 0 {
			images, idx = append([]string{defImg}, images...), 0
		}
	}
	ctrBe, exec := b.Name, 0
	if defBe != "" && defBe != "host" && defBe == b.Name {
		exec = idx + 1
	}
	if path != "" && (core.Assigned(path) || core.HasEnv(path)) {
		pb := core.ProjectBackend(path)
		exec = 0
		if _, ok := core.EngineOf(pb); ok {
			ctrBe = pb.Name
		}
		if e, err := core.LoadEnv(path); err == nil {
			i := -1
			for k, im := range images {
				if im == e.Image {
					i = k
				}
			}
			if i < 0 {
				images, i = append([]string{e.Image}, images...), 0
			}
			if _, ok := core.EngineOf(pb); ok {
				exec = i + 1
			}
		}
	}
	var bes []string
	for _, c := range core.CtrBackends() {
		bes = append(bes, c.Name)
	}
	labels := make([]string, len(images))
	for i, im := range images {
		labels[i] = core.ImageLabel(im)
	}
	out["exec"] = exec
	out["images"], out["labels"], out["backends"], out["backend"] = images, labels, bes, ctrBe
	if path != "" {
		out["path"], out["name"] = path, filepath.Base(path)
	}
	return out
}

// execApply : fin du choix de l'exécution (F2, ou première ouverture : openNext).
func (a *App) execApply(path, image, backend string, openNext bool) (any, error) {
	if t := a.term(path); t != nil {
		return nil, fmt.Errorf("projet ouvert — le fermer avant de changer de backend")
	}
	if err := core.ApplyExec(path, image, backend); err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	if image == "" {
		a.setMsg(name + " : exécution sur l'hôte")
	} else {
		a.setMsg("⬡ " + name + " : " + core.ImageLabel(image) + " sur " + backend + " (F2 pour reconfigurer)")
	}
	go a.readCtr()
	if openNext {
		return a.open(path, core.LaunchNormal)
	}
	return nil, nil
}

// create : nouveau projet dans base (catégorie, racine), puis ouvert.
func (a *App) create(base, name string, raw bool, remote, image, backend string) (any, error) {
	if base == "" {
		if len(a.conf().Roots) != 1 {
			return nil, fmt.Errorf("racine à choisir")
		}
		base = filepath.Clean(a.conf().Roots[0])
	}
	if why := core.ValidProjectName(name); why != "" {
		return nil, fmt.Errorf("%s", why)
	}
	dir := filepath.Join(base, strings.TrimSpace(name))
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("ce dossier existe déjà")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := core.ApplyExec(dir, image, backend); err != nil {
		return nil, fmt.Errorf("env.yaml : %v", err)
	}
	mode := core.LaunchNewManaged
	if raw {
		mode = core.LaunchNewRaw
	}
	if err := core.InitProject(dir, raw, remote); err != nil {
		return nil, err
	}
	a.rescan()
	a.mu.Lock()
	a.sel = dir
	a.mu.Unlock()
	go a.gitOne(dir, latestTemplate())
	r, err := a.open(dir, mode) // comme le bash : le projet créé est lancé aussitôt
	if err == nil {
		a.setMsg("✓ projet créé")
	}
	return r, err
}

// ── Connexion claude.ai, diagnostic, outils, sortie ───────────────────────

// login : clic sur l'alerte de connexion : un onglet « login » lance
// claude /login sur ce poste, dans un pseudo-terminal.
func (a *App) login() error {
	bin := core.ClaudeBin()
	if strings.HasPrefix(bin, "'") {
		bin = strings.Trim(bin, "'")
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("claude introuvable sur ce poste : lancer /login dans le leader d'un projet ouvert")
	}
	key := "login:"
	if a.term(key) != nil {
		return nil
	}
	t := newProjTerm(a, core.Node{Path: key, Key: "login", Name: "login"}, core.LaunchNormal)
	a.addTab(t)
	go t.runPTY([]string{path, "/login"})
	return nil
}

// runPTY : onglet fait d'un seul terminal (pseudo-terminal local).
func (t *projTerm) runPTY(argv []string) {
	go t.flusher()
	w, h := t.size()
	home, _ := os.UserHomeDir()
	term, err := platform.StartTerminal(platform.TermSpec{Argv: argv, Dir: home, Cols: w, Rows: h})
	if err != nil {
		fmt.Fprintf(t, "\r\n⚠ %v\r\n", err)
		t.setState("error", err.Error())
		return
	}
	t.mu.Lock()
	t.pty = term
	t.mu.Unlock()
	t.setState("pty", "")
	buf := make([]byte, 32*1024)
	for {
		n, err := term.Read(buf)
		if n > 0 {
			_, _ = t.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	_ = term.Wait()
	_ = term.Close()
	t.a.removeTab(t.path, t)
	go t.a.readUsage()
}

// diagText : sortie de « claude-launcher-ng2 backends » (même diagnostic).
func diagText() string {
	exe, err := platform.SelfExe()
	if err != nil {
		return err.Error()
	}
	c := platform.NoWindow(exec.Command(exe, "backends"))
	c.Env = append(os.Environ(), "CL_NO_UPDATE=1")
	out, err := c.CombinedOutput()
	if err != nil && len(out) == 0 {
		return err.Error()
	}
	return string(out)
}

// toolsData : outils externes requis (écran du premier lancement, diag).
func toolsData() map[string]any {
	ts := core.CheckTools()
	required := 0 // l'écran du premier lancement ne s'impose que pour un outil requis
	for _, t := range core.MissingTools(ts) {
		if !t.Optional {
			required++
		}
	}
	return map[string]any{"tools": ts, "missing": required, "os": runtime.GOOS}
}

// quitApp : « l » : launcher seul (projets gardés) ; « t » : tout fermer
// (sessions des projets de l'hôte arrêtées, conteneurs laissés à leur session,
// comme la coque qui ferme claude-hub).
func (a *App) quitApp(mode string) error {
	if mode == "t" && !hostRefused() {
		c := platform.NoWindow(exec.Command("tmux", "-L", deskSocket(), "kill-server"))
		c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
		_ = c.Run()
	}
	go func() {
		a.Stop()
		quitHook()
	}()
	return nil
}
