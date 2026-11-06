package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Conteneurs dans le navigateur ─────────────────────────────────────────

// ctrView : ce que le navigateur sait des backends, relu toutes les 10 s.
type ctrView struct {
	env    map[string]bool      // projet -> a un .claude-launcher/env.yaml
	assign map[string]string    // projet -> backend (sans entrée : host, ou « prefer »)
	found  map[string]Found     // projet -> son conteneur, s'il existe
	errs   map[string]string    // backend -> erreur de listing (moteur absent…)
	diag   map[string][]problem // backend affecté à un projet -> ce qui l'empêche de marcher
}

type ctrMsg ctrView

func ctrCmd(paths []string, delay time.Duration) tea.Cmd {
	read := func(time.Time) tea.Msg { return ctrMsg(readCtrView(paths)) }
	if delay <= 0 {
		return func() tea.Msg { return read(time.Now()) }
	}
	return tea.Tick(delay, read)
}

func readCtrView(paths []string) ctrView {
	v := ctrView{env: map[string]bool{}, assign: map[string]string{}, found: map[string]Found{}, errs: map[string]string{}, diag: map[string][]problem{}}
	any := false
	for _, p := range paths {
		if hasEnv(p) {
			v.env[p], any = true, true
			v.assign[p] = projectBackend(p).Name
		}
	}
	bs, _ := loadBackends()
	for _, b := range bs {
		e, ok := engineOf(b)
		if !ok || !any && !usedBackend(b.Name) {
			continue
		}
		if assignedTo(v.assign, b.Name) {
			if ps := diagnoseCached(b); len(ps) > 0 {
				v.diag[b.Name] = ps
				continue
			}
		}
		fs, err := e.list()
		if err != nil {
			v.errs[b.Name] = err.Error()
			continue
		}
		for _, f := range fs {
			v.found[f.Project] = f
		}
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
	for _, b := range readAssignments() {
		if b == name {
			return true
		}
	}
	return false
}

// ctrIndicator : backend du projet sur la ligne du navigateur (projets en conteneur seulement).
func ctrIndicator(p projInfo) (indicator, bool) {
	if p.backend == "" || p.backend == "host" {
		if p.hasEnv {
			return indicator{icon: "⬡", style: dim, text: "env.yaml présent : exécution locale (F2 : choisir un conteneur)"}, true
		}
		return indicator{}, false
	}
	switch {
	case len(p.problems) > 0:
		return indicator{icon: "⚠", style: yellow, text: "backend " + p.backend + " inutilisable : " + p.problems[0].String() +
			" (détails : claude-launcher-ng2 backends)"}, true
	case !p.ctrKnown:
		return indicator{icon: "⬡", style: dim, text: "conteneur " + p.backend + " pas encore créé (bootstrap à l'ouverture)"}, true
	case p.ctr.Running:
		return indicator{icon: "⬢", style: cyan, text: "conteneur " + p.backend + " en marche (" + p.ctr.Name + ")"}, true
	default:
		return indicator{icon: "⬢", style: dim, text: "conteneur " + p.backend + " arrêté (redémarré à l'ouverture)"}, true
	}
}

// cycleBackend (F2) : backend suivant pour le projet sélectionné.
func (m model) cycleBackend() model {
	if len(m.visible) == 0 {
		return m
	}
	n := m.nodes[m.visible[m.cursor]]
	if !n.Project {
		return m
	}
	if !hasEnv(n.Path) {
		m.msg = "F2 : pas de " + envFile + " dans ce projet (exécution locale seulement)"
		return m
	}
	if m.windows[n.Key] {
		m.msg = "F2 : projet ouvert — le fermer avant de changer de backend"
		return m
	}
	if _, err := loadEnv(n.Path); err != nil {
		m.msg = "⚠ " + err.Error()
		return m
	}
	bs, err := loadBackends()
	if err != nil {
		m.msg = "⚠ " + err.Error()
		return m
	}
	cur := projectBackend(n.Path).Name
	next := bs[0]
	for i, b := range bs {
		if b.Name == cur {
			next = bs[(i+1)%len(bs)]
		}
	}
	if err := writeAssignment(n.Path, next.Name); err != nil {
		m.msg = "⚠ " + err.Error()
		return m
	}
	if m.ctr.assign != nil {
		m.ctr.assign[n.Path] = next.Name
	}
	m.msg = "backend de " + n.Name + " : " + next.Name
	return m
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
		bs, _ := loadBackends()
		open := tmuxWindows(targetSession(session))
		var back []string
		for _, b := range bs {
			e, ok := engineOf(b)
			if !ok {
				continue
			}
			fs, err := e.list()
			if err != nil {
				continue
			}
			for _, f := range fs {
				n, ok := byPath[f.Project]
				if !ok || !f.Running || open[n.Key] || projectBackend(f.Project).Name != b.Name || !e.hasSession(f.Name) {
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
func ctrAttachMain(args []string) error {
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
func runBackends() error {
	bs, err := loadBackends()
	if err != nil {
		return err
	}
	fmt.Println("Backends (" + backendsPath() + ") :")
	for _, b := range bs {
		line := "  " + padRight(b.Name, 12) + b.Type
		if b.Host != "" {
			line += " " + b.Host
		}
		if ps := diagnose(b); len(ps) > 0 {
			for _, p := range ps {
				line += "\n      ⚠ " + p.what + "\n        → " + p.fix
			}
			fmt.Println(line)
			continue
		}
		if e, ok := engineOf(b); ok {
			fs, err := e.list()
			if err != nil {
				line += "  ⚠ " + strings.TrimSpace(err.Error())
			} else {
				line += fmt.Sprintf("  %d conteneur(s)", len(fs))
				for _, f := range fs {
					st := "arrêté"
					if f.Running {
						st = "en marche"
					}
					line += "\n      " + f.Name + "  " + st + "  " + f.Project
				}
			}
		}
		fmt.Println(line)
	}
	return nil
}
