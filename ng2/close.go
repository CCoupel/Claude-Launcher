package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Fermeture d'un projet (#50) ───────────────────────────────────────────
//
// Ctrl+clic (ou Suppr) sur un projet du navigateur le ferme proprement :
//  1. « /exit » envoyé à claude (le leader) : sortie propre, marqueur posé, pas
//     de « ↺ » ensuite ;
//  2. attente de la fin de claude (closeWait), puis fermeture de la session tmux
//     (forcée si claude ne répond pas) : teammates compris ;
//  3. projet en conteneur : arrêt du conteneur (podman/docker stop ; k8s :
//     StatefulSet à 0 réplique, remis à 1 à l'ouverture suivante).
// Une session pilotée par un autre launcher n'est jamais fermée d'ici : il faut
// d'abord prendre la main (Entrée).

var closeWait = 30 * time.Second

// closePlan : ce que fera la fermeture, décidé au Ctrl+clic.
type closePlan struct {
	node     Node
	question string  // vide : pas de question (conteneur vide à arrêter)
	session  bool    // une session claude est à fermer
	window   bool    // fenêtre du hub à fermer
	abandon  bool    // session interrompue : la reprise est abandonnée
	engine   *Engine // projet en conteneur
	ctr      string  // … son conteneur (ou pod)
	hubSess  string  // session du hub…
	hubTgt   string  // … et fenêtre du projet (=session:=fenêtre)
}

type closeMsg struct {
	path, step string
	done       bool
	err        error
}

// planClose : plan de fermeture du projet n, ou la raison du refus.
func (m model) planClose(n Node) (closePlan, string) {
	if _, busy := m.closing[n.Path]; busy {
		return closePlan{}, n.Name + " : fermeture déjà en cours"
	}
	p := m.projInfo(n)
	switch p.dot {
	case sessReadOnly, sessElse:
		who := p.live.owner.id
		return closePlan{}, n.Name + " : piloté par " + who + " — Entrée pour prendre la main, puis Ctrl+clic"
	}
	pl := closePlan{node: n, window: p.open, session: p.open || p.claudeLive(),
		hubSess: targetSession(m.session), hubTgt: exactW(targetSession(m.session), n.Key)}
	var what []string
	if b, ok := findBackend(p.backend); ok && p.ctrKnown {
		if e, ok := engineOf(b); ok {
			pl.engine, pl.ctr = &e, p.ctr.Name
		}
	}
	running := pl.engine != nil && (p.ctr.Running || p.ctr.Failed)
	stopText := ""
	if running {
		stopText = "conteneur arrêté"
		if pl.engine.b.k8s() {
			stopText = "pod k8s arrêté"
		}
		stopText += " (rien n'est perdu)"
	}
	switch p.dot {
	case sessNone:
		return closePlan{}, n.Name + " : rien ne tourne, rien à fermer"
	case sessReady:
		if !running {
			return closePlan{}, n.Name + " : rien à fermer"
		}
		return pl, "" // conteneur sans claude : arrêté sans question
	case sessAnomaly:
		switch {
		case p.hasSess && !pl.session:
			pl.abandon = true
			what = append(what, "reprise abandonnée (la conversation reste dans /resume)")
		case !running:
			return closePlan{}, n.Name + " : " + p.dotWhy
		}
	default:
		c := "claude"
		if t := m.teammates(p, pl); t > 0 {
			c += fmt.Sprintf(" + %d teammate(s)", t)
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
	return pl, ""
}

// teammates : panes de la session hors leader.
func (m model) teammates(p projInfo, pl closePlan) int {
	n := 0
	switch {
	case p.hasLive:
		n = p.live.panes
	case pl.window:
		s, _ := tmuxOut("display-message", "-p", "-t", pl.hubTgt, "#{window_panes}")
		n, _ = strconv.Atoi(s)
	}
	return max(n-1, 0)
}

// startClose : lance la fermeture en tâche de fond ; l'avancement arrive par m.closeCh.
func (m *model) startClose(pl closePlan) tea.Cmd {
	if m.closing == nil {
		m.closing = map[string]string{}
	}
	first := m.closeCh == nil
	if first {
		m.closeCh = make(chan closeMsg, 32)
	}
	m.closing[pl.node.Path] = "fermeture…"
	ch := m.closeCh
	go func() {
		err := runClose(pl, func(s string) { ch <- closeMsg{path: pl.node.Path, step: s} })
		ch <- closeMsg{path: pl.node.Path, done: true, err: err}
	}()
	if first {
		return waitClose(ch)
	}
	return nil
}

func waitClose(ch <-chan closeMsg) tea.Cmd { return func() tea.Msg { return <-ch } }

// onClose : avancement d'une fermeture (pied du navigateur).
func (m model) onClose(c closeMsg) (model, tea.Cmd) {
	name := baseName(c.path)
	switch {
	case !c.done:
		m.closing[c.path] = c.step
		m.msg = "⏻ " + name + " : " + c.step
		return m, waitClose(m.closeCh)
	case c.err != nil:
		m.msg = "⚠ " + name + " : " + c.err.Error()
	default:
		m.msg = "✓ " + name + " fermé"
	}
	delete(m.closing, c.path)
	return m, tea.Batch(waitClose(m.closeCh), windowsCmd(m.session), ctrNowCmd(m.projectPaths(), 0),
		sessNowCmd(m.cfg, m.projectPaths()))
}

// sessionTmux : tmux de la session du projet — le hub (fenêtre du projet) ou
// celui du conteneur ; renvoie une commande « tmux args… » prête à lancer.
type sessionTmux func(args ...string) (string, error)

func (pl closePlan) tmux() sessionTmux {
	if pl.engine != nil && pl.ctr != "" {
		e, name := *pl.engine, pl.ctr
		return func(args ...string) (string, error) { return e.output(name, tmuxIn(args...)...) }
	}
	return func(args ...string) (string, error) { return tmuxOut(args...) }
}

// sessionTarget : cible tmux de la session (fenêtre du hub, ou session du conteneur).
func (pl closePlan) sessionTarget() string {
	if pl.engine != nil && pl.ctr != "" {
		return "=" + ctrSession + ":"
	}
	return pl.hubTgt
}

func runClose(pl closePlan, step func(string)) error {
	if pl.abandon {
		// la conversation reste lisible (/resume) ; elle n'est plus proposée à la reprise
		m := cleanExitPath(projectDirName(pl.node.Path))
		if err := os.MkdirAll(filepath.Dir(m), 0o755); err == nil {
			_ = os.WriteFile(m, nil, 0o644)
			now := time.Now()
			_ = os.Chtimes(m, now, now)
		}
		step("reprise abandonnée")
	}
	if pl.session {
		if pl.engine != nil {
			// dernier contrôle : quelqu'un a pu prendre la main depuis la question
			if o := pl.engine.owner(pl.ctr); o.alive() && o.id != launcherID() {
				return fmt.Errorf("piloté par %s depuis la question : fermeture annulée", o.id)
			}
		}
		if err := stopSession(pl.tmux(), pl.sessionTarget(), pl.engine != nil && pl.ctr != "", step); err != nil {
			return err
		}
	}
	if pl.window {
		// conteneur : l'attache s'est terminée avec sa session et la fenêtre avec elle
		for i := 0; i < 20 && pl.engine != nil && tmuxWindows(pl.hubSess)[pl.node.Key]; i++ {
			time.Sleep(100 * time.Millisecond)
		}
		_ = dtmux("kill-window", "-t", pl.hubTgt).Run()
	}
	if pl.engine != nil {
		step("arrêt du conteneur…")
		if err := pl.engine.stop(pl.node.Path, pl.ctr); err != nil {
			return err
		}
		if pl.engine.b.k8s() {
			step("pod arrêté (0 réplique)")
		} else {
			step("conteneur arrêté")
		}
	}
	return nil
}

// shells : commandes d'un pane revenu à son shell (claude terminé).
var shells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "fish": true, "ash": true}

// stopSession : /exit au leader, attente de sa sortie (closeWait), puis
// fermeture de la session tmux (hub : la fenêtre ; conteneur : la session).
func stopSession(t sessionTmux, target string, inCtr bool, step func(string)) error {
	leader, cmd := leaderPane(t, target, inCtr)
	if leader == "" {
		return nil // plus de session
	}
	start := time.Now()
	if !shells[cmd] {
		// Échap interrompt une réponse en cours ; /exit est ensuite tapé seul
		_, _ = t("send-keys", "-t", leader, "Escape")
		time.Sleep(400 * time.Millisecond)
		_, _ = t("send-keys", "-t", leader, "-l", "/exit")
		time.Sleep(150 * time.Millisecond)
		_, _ = t("send-keys", "-t", leader, "Enter")
		step("/exit envoyé à claude")
		clean := false
		for time.Since(start) < closeWait {
			time.Sleep(500 * time.Millisecond)
			if c := paneCommand(t, leader); c == "" || shells[c] {
				clean = true
				break
			}
		}
		if clean {
			step(fmt.Sprintf("sortie propre (%d s)", int(time.Since(start).Seconds()+0.5)))
		} else {
			step(fmt.Sprintf("pas de réponse en %d s : arrêt forcé", int(closeWait.Seconds())))
		}
	}
	if inCtr {
		_, _ = t("kill-session", "-t", "="+ctrSession)
	}
	return nil
}

// leaderPane : premier pane créé de la session (plus petit %id) et sa commande.
func leaderPane(t sessionTmux, target string, inCtr bool) (id, cmd string) {
	args := []string{"list-panes", "-t", target, "-F", "#{pane_id} #{pane_current_command}"}
	if inCtr {
		args = []string{"list-panes", "-s", "-t", "=" + ctrSession, "-F", "#{pane_id} #{pane_current_command}"}
	}
	s, err := t(args...)
	if err != nil {
		return "", ""
	}
	return leaderOf(s)
}

// leaderOf : dans des lignes « %id commande », le plus petit %id et sa commande.
func leaderOf(s string) (id, cmd string) {
	best := -1
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 1 || !strings.HasPrefix(f[0], "%") {
			continue
		}
		n, err := strconv.Atoi(f[0][1:])
		if err != nil || best >= 0 && n >= best {
			continue
		}
		best, id, cmd = n, f[0], ""
		if len(f) > 1 {
			cmd = f[1]
		}
	}
	return id, cmd
}

func paneCommand(t sessionTmux, pane string) string {
	s, err := t("display-message", "-p", "-t", pane, "#{pane_current_command}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// stop : arrêt du conteneur du projet (k8s : 0 réplique, le pod est libéré ;
// tout est sur le stockage partagé, l'ouverture suivante le remet à 1).
func (e Engine) stop(proj, name string) error {
	var c *exec.Cmd
	if e.b.k8s() {
		c = e.kcmd("scale", "statefulset", k8sName(proj), "--replicas=0")
	} else {
		c = e.cmd("stop", "-t", "5", name)
	}
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("arrêt du conteneur : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// askClose : Ctrl+clic ou Suppr sur le projet n.
func (m model) askClose(n Node) (model, tea.Cmd) {
	if !n.Project {
		return m, nil
	}
	pl, why := m.planClose(n)
	if why != "" {
		m.msg = why
		return m, nil
	}
	if !m.launch {
		m.msg = "[dry-run] fermeture de " + n.Name
		return m, nil
	}
	if pl.question == "" {
		m.msg = "⏻ " + n.Name + " : arrêt du conteneur vide"
		return m, m.startClose(pl)
	}
	m.closeAsk = &pl
	return m, nil
}
