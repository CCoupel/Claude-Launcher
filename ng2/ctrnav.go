package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Conteneurs dans le navigateur ─────────────────────────────────────────

// ctrView : ce que le navigateur sait des backends, relu toutes les 10 s.
type ctrView struct {
	env    map[string]bool      // projet -> a un env.yaml ou un env-template.yaml
	assign map[string]string    // projet -> backend (sans entrée : host, ou « prefer »)
	found  map[string]Found     // projet -> son conteneur, s'il existe
	errs   map[string]string    // backend -> erreur de listing (moteur absent…)
	diag   map[string][]Problem // backend affecté à un projet -> ce qui l'empêche de marcher
	bad    map[string]string    // projet -> configuration invalide (launchBackend)
	live   map[string]LiveSess  // projet -> session tmux vivante dans son conteneur
	dups   map[string][]string  // projet -> serveurs où il a un conteneur (groupe, en double)
	down   map[string]string    // backend (groupe) -> serveurs injoignables
}

// liveSess : session tmux du conteneur d'un projet (#50) : propriétaire, nombre
// de panes (claude et ses teammates) et claude encore en marche dans le leader.
type LiveSess struct {
	Owner  OwnerInfo
	Panes  int
	Claude bool // le leader (plus petit %id) n'est pas revenu à son shell
}

// readLive : session du conteneur name, si elle existe (une seule commande :
// propriétaire et panes, puis la liste des panes).
func (e Engine) ReadLive(name string) (LiveSess, bool) {
	s, err := e.Output(name, TmuxIn("display-message", "-p", "-t", "="+CtrSession+":", OwnerFormat+"|#{window_panes}", ";",
		"list-panes", "-s", "-t", "="+CtrSession, "-F", "#{pane_id} #{pane_current_command}")...)
	if err != nil {
		return LiveSess{}, false
	}
	return ParseLive(s)
}

func ParseLive(s string) (LiveSess, bool) {
	head, panes, _ := strings.Cut(s, "\n")
	i := strings.LastIndex(head, "|")
	if i < 0 {
		return LiveSess{}, false
	}
	n, _ := strconv.Atoi(head[i+1:])
	l := LiveSess{Owner: ParseOwner(head[:i]), Panes: n}
	if id, cmd := LeaderOf(panes); id != "" {
		l.Claude = !Shells[cmd]
	}
	return l, true
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
	v := ctrView{env: map[string]bool{}, assign: map[string]string{}, found: map[string]Found{}, errs: map[string]string{}, diag: map[string][]Problem{}, bad: map[string]string{}, live: map[string]LiveSess{},
		dups: map[string][]string{}, down: map[string]string{}}
	any := false
	asg := ReadAssignments()
	for _, p := range paths {
		if HasEnv(p) {
			v.env[p], any = true, true
			v.assign[p] = ProjectBackend(p).Name
		}
		if name, ok := asg[p]; ok && name != "host" {
			v.assign[p] = name
			if _, err := LaunchBackend(p); err != nil {
				v.bad[p] = err.Error()
			}
		}
	}
	bs, _ := LoadBackends()
	for _, b := range bs {
		e, ok := EngineOf(b)
		if !ok || !any && !usedBackend(b.Name) {
			continue
		}
		if assignedTo(v.assign, b.Name) {
			if ps := DiagnoseCached(b); len(ps) > 0 {
				v.diag[b.Name] = ps
				continue
			}
		}
		var fs []Found
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
			v.down[b.Name] = DownText(down)
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
			go func(f Found) {
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
	for _, b := range ReadAssignments() {
		if b == name {
			return true
		}
	}
	return false
}

// ctrIndicator : backend du projet sur la ligne du navigateur (projets en conteneur seulement).
func ctrIndicator(p projInfo) (indicator, bool) {
	if p.badCfg != "" {
		return indicator{icon: "⚠", style: Red, text: p.badCfg}, true
	}
	if p.backend == "" || p.backend == "host" {
		if p.hasEnv {
			return indicator{icon: "⬡", style: Dim, text: "environnement conteneur présent (env.yaml / env-template.yaml) : exécution locale (F2 : choisir un conteneur)"}, true
		}
		return indicator{}, false
	}
	switch {
	case len(p.problems) > 0:
		return indicator{icon: "⚠", style: Yellow, text: "backend " + p.backend + " inutilisable : " + p.problems[0].String() +
			" (détails : claude-launcher-ng2 backends)"}, true
	case !p.ctrKnown:
		return indicator{icon: "⬡", style: Dim, text: "conteneur " + p.backend + " pas encore créé (bootstrap à l'ouverture)"}, true
	case p.ctr.Running:
		return indicator{icon: "⬢", style: Cyan, text: "conteneur " + p.backend + " en marche (" + p.ctr.Name + ")"}, true
	default:
		return indicator{icon: "⬢", style: Dim, text: "conteneur " + p.backend + " arrêté (redémarré à l'ouverture)"}, true
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
	if HasEnv(n.Path) {
		if _, err := LoadEnv(n.Path); err != nil {
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
func reattachCmd(cfg Config, session string, nodes []Node) tea.Cmd {
	return func() tea.Msg {
		byPath := map[string]Node{}
		for _, n := range nodes {
			if n.Project {
				byPath[n.Path] = n
			}
		}
		bs, _ := LoadBackends()
		open := tmuxWindows(targetSession(session))
		var back []string
		for _, b := range bs {
			e, ok := EngineOf(b)
			if !ok {
				continue
			}
			fs, err := e.List()
			if err != nil {
				continue
			}
			for _, f := range fs {
				n, ok := byPath[f.Project]
				if !ok || !f.Running || open[n.Key] || ProjectBackend(f.Project).Name != b.Name || !e.Of(f).HasSession(f.Name) {
					continue
				}
				// seulement les sessions de ce launcher (#48) : pas celles d'une autre machine
				if o := e.Of(f).Owner(f.Name); o.Id != "" && o.Id != LauncherID() {
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

// ctrAttachMain : sous-commande ctr-attach ; en cas d'échec, l'erreur reste
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

// runBackends : sous-commande « backends » : backends et conteneurs NG2.
func RunBackends() error {
	bs, err := LoadBackends()
	if err != nil {
		return err
	}
	fmt.Println("Backends (" + BackendsPath() + ") :")
	for _, b := range bs {
		line := "  " + PadRight(b.Name, 12) + " " + b.Type
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
		if ps := Diagnose(b); len(ps) > 0 {
			for _, p := range ps {
				line += "\n      ⚠ " + p.What + "\n        → " + p.Fix
			}
			fmt.Println(line)
			continue
		}
		if e, ok := EngineOf(b); ok {
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
	PrintBrowserServer()
	PrintBrowserDevice()
	PrintBrowserPaths()
	return nil
}

// printPool : chaque serveur d'un groupe (#61) — état, conteneurs NG2, diagnostic.
func printPool(b BackendConf) {
	e := Engine{b}
	fs, down := e.ListPool()
	probs := PoolProblems(b)
	load := LoadOf(b, fs, down)
	own := map[string]int{}
	for h, n := range load {
		own[h] = n
	}
	AddMates(b, load)
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
