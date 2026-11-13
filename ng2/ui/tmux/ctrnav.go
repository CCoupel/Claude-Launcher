package tmux

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// ── Conteneurs dans le navigateur ─────────────────────────────────────────

// ctrView : ce que le navigateur sait des backends, relu toutes les 10 s.
type ctrView struct {
	env    map[string]bool           // projet -> a un env.yaml ou un env-template.yaml
	assign map[string]string         // projet -> backend (sans entrée : host, ou « prefer »)
	found  map[string]core.Found     // projet -> son conteneur, s'il existe
	errs   map[string]string         // backend -> erreur de listing (moteur absent…)
	diag   map[string][]core.Problem // backend affecté à un projet -> ce qui l'empêche de marcher
	bad    map[string]string         // projet -> configuration invalide (launchBackend)
	live   map[string]core.LiveSess  // projet -> session tmux vivante dans son conteneur
	dups   map[string][]string       // projet -> serveurs où il a un conteneur (groupe, en double)
	down   map[string]string         // backend (groupe) -> serveurs injoignables
}

type ctrMsg ctrView

func ctrCmd(paths []string, delay time.Duration) tea.Cmd {
	read := func(time.Time) tea.Msg { return ctrMsg(readCtrView(paths)) }
	if delay <= 0 {
		return func() tea.Msg { return read(time.Now()) }
	}
	return tea.Tick(delay, read)
}

// ctrNowMsg : relecture ponctuelle (après une ouverture, une fermeture, F2) ;
// contrairement à ctrMsg, elle ne reprogramme pas la relecture périodique, qui
// sinon se dédoublerait à chaque fois.
type ctrNowMsg ctrView

func ctrNowCmd(paths []string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return ctrNowMsg(readCtrView(paths)) })
}

func readCtrView(paths []string) ctrView {
	v := ctrView{env: map[string]bool{}, assign: map[string]string{}, found: map[string]core.Found{}, errs: map[string]string{}, diag: map[string][]core.Problem{}, bad: map[string]string{}, live: map[string]core.LiveSess{},
		dups: map[string][]string{}, down: map[string]string{}}
	any := false
	asg := core.ReadAssignments()
	for _, p := range paths {
		if core.HasEnv(p) {
			v.env[p], any = true, true
			v.assign[p] = core.ProjectBackend(p).Name
		}
		if name, ok := asg[p]; ok && name != "host" {
			v.assign[p] = name
			if _, err := core.LaunchBackend(p); err != nil {
				v.bad[p] = err.Error()
			}
		}
	}
	bs, _ := core.LoadBackends()
	for _, b := range bs {
		e, ok := core.EngineOf(b)
		if !ok || !any && !usedBackend(b.Name) {
			continue
		}
		if assignedTo(v.assign, b.Name) {
			if ps := core.DiagnoseCached(b); len(ps) > 0 {
				v.diag[b.Name] = ps
				continue
			}
		}
		var fs []core.Found
		var down map[string]error
		if b.K8s() {
			var err error
			if fs, err = e.K8sList(); err != nil {
				down = map[string]error{"": err}
			}
		} else {
			fs, down = e.ListPool() // groupe : tous ses serveurs en parallèle (#61)
		}
		if len(fs) == 0 && len(down) > 0 && len(down) == len(b.Servers()) {
			for _, err := range down {
				v.errs[b.Name] = err.Error()
			}
			continue
		}
		if len(down) > 0 {
			v.down[b.Name] = core.DownText(down)
		}
		for _, f := range fs {
			if o, dup := v.found[f.Project]; dup && o.Backend == f.Backend && o.Server != f.Server {
				v.dups[f.Project] = append(v.dups[f.Project], f.Server)
				if len(v.dups[f.Project]) == 1 {
					v.dups[f.Project] = []string{o.Server, f.Server}
				}
			}
			v.found[f.Project] = f
		}
		// sessions des conteneurs en marche, en parallèle (un exec chacun ; k8s ~0,5 s)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, f := range fs {
			if !f.Running {
				continue
			}
			wg.Add(1)
			go func(f core.Found) {
				defer wg.Done()
				if l, ok := e.Of(f).ReadLive(f.Name); ok {
					mu.Lock()
					v.live[f.Project] = l
					mu.Unlock()
				}
			}(f)
		}
		wg.Wait()
	}
	return v
}

func assignedTo(assign map[string]string, name string) bool {
	for _, b := range assign {
		if b == name {
			return true
		}
	}
	return false
}

func usedBackend(name string) bool {
	for _, b := range core.ReadAssignments() {
		if b == name {
			return true
		}
	}
	return false
}

// ctrIndicator : backend du projet sur la ligne du navigateur (projets en conteneur seulement).
func ctrIndicator(p projInfo) (indicator, bool) {
	if p.badCfg != "" {
		return indicator{icon: "⚠", style: core.Red, text: p.badCfg}, true
	}
	if p.backend == "" || p.backend == "host" {
		if p.hasEnv {
			return indicator{icon: "⬡", style: core.Dim, text: "environnement conteneur présent (env.yaml / env-template.yaml) : exécution locale (F2 : choisir un conteneur)"}, true
		}
		return indicator{}, false
	}
	switch {
	case len(p.problems) > 0:
		return indicator{icon: "⚠", style: core.Yellow, text: "backend " + p.backend + " inutilisable : " + p.problems[0].String() +
			" (détails : claude-launcher-ng2 backends)"}, true
	case !p.ctrKnown:
		return indicator{icon: "⬡", style: core.Dim, text: "conteneur " + p.backend + " pas encore créé (bootstrap à l'ouverture)"}, true
	case p.ctr.Running:
		return indicator{icon: "⬢", style: core.Cyan, text: "conteneur " + p.backend + " en marche (" + p.ctr.Name + ")"}, true
	default:
		return indicator{icon: "⬢", style: core.Dim, text: "conteneur " + p.backend + " arrêté (redémarré à l'ouverture)"}, true
	}
}

// cycleBackend (F2) : backend suivant pour le projet sélectionné.
func (m model) cycleBackend() model { // F2
	if len(m.visible) == 0 {
		return m
	}
	n := m.nodes[m.visible[m.cursor]]
	if !n.Project {
		return m
	}
	if m.windows[n.Key] {
		m.msg = "F2 : projet ouvert — le fermer avant de changer de backend"
		return m
	}
	if core.HasEnv(n.Path) {
		if _, err := core.LoadEnv(n.Path); err != nil {
			m.msg = "⚠ " + err.Error()
			return m
		}
	}
	// formulaire « Exécution » prérempli avec l'état du projet
	return m.startExecChoice(n, false)
}

// reattachContainers : au démarrage du hub, une fenêtre est recréée pour chaque
// projet dont le conteneur tourne encore avec sa session (launcher ou hub
// tombés entre-temps) : la session est retrouvée telle quelle.
func reattachCmd(cfg core.Config, session string, nodes []core.Node) tea.Cmd {
	return func() tea.Msg {
		byPath := map[string]core.Node{}
		for _, n := range nodes {
			if n.Project {
				byPath[n.Path] = n
			}
		}
		bs, _ := core.LoadBackends()
		open := tmuxWindows(targetSession(session))
		var back []string
		for _, b := range bs {
			e, ok := core.EngineOf(b)
			if !ok {
				continue
			}
			fs, err := e.List()
			if err != nil {
				continue
			}
			for _, f := range fs {
				n, ok := byPath[f.Project]
				if !ok || !f.Running || open[n.Key] || core.ProjectBackend(f.Project).Name != b.Name || !e.Of(f).HasSession(f.Name) {
					continue
				}
				// seulement les sessions de ce launcher (#48) : pas celles d'une autre machine
				if o := e.Of(f).Owner(f.Name); o.Id != "" && o.Id != core.LauncherID() {
					continue
				}
				if err := launchProject(cfg, session, n, readLatestTemplate(), launchNormal); err != nil {
					back = append(back, n.Name+" ⚠ "+err.Error())
				} else {
					back = append(back, n.Name)
				}
			}
		}
		if len(back) == 0 {
			return nil
		}
		return reattachMsg(back)
	}
}

type reattachMsg []string

// CtrAttachMain : sous-commande ctr-attach ; en cas d'échec, l'erreur reste
// affichée dans la fenêtre jusqu'à Entrée (sinon tmux la fermerait aussitôt).
func CtrAttachMain(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage : claude-launcher-ng2 ctr-attach DOSSIER FICHIER_DEMARRAGE")
	}
	err := runCtrAttach(args[0], args[1])
	if err != nil {
		fmt.Printf("\n\033[1;31m⚠ %v\033[0m\n\nEntrée pour fermer la fenêtre…", err)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return err
}

// RunBackends : sous-commande « backends » : backends et conteneurs NG2.
func RunBackends() error {
	bs, err := core.LoadBackends()
	if err != nil {
		return err
	}
	fmt.Println("Backends (" + core.BackendsPath() + ") :")
	for _, b := range bs {
		line := "  " + core.PadRight(b.Name, 12) + " " + b.Type
		if b.Host != "" {
			line += " " + b.Host
		}
		if b.IsPool() {
			g := fmt.Sprintf("  groupe de %d serveurs", len(b.Hosts))
			if b.Pool != "" {
				g = fmt.Sprintf("  pool %s (%d machines", b.Pool, len(b.Hosts))
				if len(b.Drain) > 0 {
					g += fmt.Sprintf(", %d en drain", len(b.Drain))
				}
				if len(b.Mates) > 0 {
					var ns []string
					for _, m := range b.Mates {
						ns = append(ns, m.Name)
					}
					g += ", charge commune avec " + strings.Join(ns, ", ")
				}
				g += ")"
			}
			fmt.Println(line + g)
			printPool(b)
			continue
		}
		if b.K8s() {
			line += "  (" + b.PlacementText() + ")"
		}
		if ps := core.Diagnose(b); len(ps) > 0 {
			for _, p := range ps {
				line += "\n      ⚠ " + p.What + "\n        → " + p.Fix
			}
			fmt.Println(line)
			continue
		}
		if e, ok := core.EngineOf(b); ok {
			fs, err := e.List()
			if err != nil {
				line += "  ⚠ " + strings.TrimSpace(err.Error())
			} else {
				line += fmt.Sprintf("  %d conteneur(s)", len(fs))
				var nodes map[string]string
				if b.K8s() {
					nodes = e.K8sNodes()
				}
				for _, f := range fs {
					st := "arrêté"
					if f.Running {
						st = "en marche"
					}
					line += "\n      " + f.Name + "  " + st + "  " + f.Project
					if n := nodes[f.Name]; n != "" {
						line += "  (node " + n + ")"
					}
				}
			}
		}
		fmt.Println(line)
	}
	core.PrintBrowserServer()
	core.PrintBrowserDevice()
	core.PrintBrowserPaths()
	core.PrintHubPaths()
	core.PrintTools()
	return nil
}

// printPool : chaque serveur d'un groupe (#61) — état, conteneurs NG2, diagnostic.
func printPool(b core.BackendConf) {
	e := core.Engine{B: b}
	fs, down := e.ListPool()
	probs := core.PoolProblems(b)
	load := core.LoadOf(b, fs, down)
	own := map[string]int{}
	for h, n := range load {
		own[h] = n
	}
	core.AddMates(b, load)
	for _, h := range b.Hosts {
		line := "      " + h
		if b.Drain[h] {
			line += "  [drain : plus de nouveaux projets]"
		}
		switch {
		case len(probs[h]) > 0:
			for _, p := range probs[h] {
				line += "\n        ⚠ " + p.What + "\n          → " + p.Fix
			}
		case down[h] != nil:
			line += "  ⚠ " + strings.TrimSpace(down[h].Error())
		default:
			n := 0
			for _, f := range fs {
				if f.Server == h {
					n++
				}
			}
			line += fmt.Sprintf("  %d conteneur(s), %d en marche", n, own[h])
			if o := load[h] - own[h]; o > 0 {
				line += fmt.Sprintf(" (+%d d'autres backends du pool)", o)
			}
			for _, f := range fs {
				if f.Server == h {
					st := "arrêté"
					if f.Running {
						st = "en marche"
					}
					line += "\n        " + f.Name + "  " + st + "  " + f.Project
				}
			}
		}
		fmt.Println(line)
	}
	seen := map[string][]string{}
	for _, f := range fs {
		seen[f.Project] = append(seen[f.Project], f.Server)
	}
	for p, hs := range seen {
		if len(hs) > 1 {
			fmt.Printf("      ⚠ %s en double sur %s (Entrée sur le projet pour choisir)\n", p, strings.Join(hs, " et "))
		}
	}
}
