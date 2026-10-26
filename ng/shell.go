package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	stateWindow  = "◈ État"
	defaultNavW  = 40
	outerSession = "claude-ui"
)

func cacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	d := filepath.Join(base, "claude-launcher")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// ── Sélection partagée navigateur → vue État ──────────────────────────────

type Selection struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Path string `json:"path"`
}

func selectionPath() string { return filepath.Join(cacheDir(), "selection.json") }

func writeSelection(n Node) {
	b, _ := json.Marshal(Selection{Name: n.Name, Key: n.Key, Path: n.Path})
	tmp := selectionPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, selectionPath())
	}
}

func readSelection() (Selection, bool) {
	var s Selection
	b, err := os.ReadFile(selectionPath())
	if err != nil || json.Unmarshal(b, &s) != nil || s.Path == "" {
		return s, false
	}
	return s, true
}

// ── Largeur de la zone navigation ─────────────────────────────────────────

func navWidthPath() string { return filepath.Join(cacheDir(), "navwidth") }

func loadNavWidth() int {
	if b, err := os.ReadFile(navWidthPath()); err == nil {
		if n, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && n >= 20 {
			return n
		}
	}
	return defaultNavW
}

func saveNavWidth(n int) {
	_ = os.WriteFile(navWidthPath(), []byte(strconv.Itoa(n)), 0o644)
}

// fixWidth : appelé par les hooks tmux (attache, redimensionnement du terminal)
// pour que le navigateur garde sa largeur et que la zone tmux absorbe le reste.
func fixWidth() {
	w, _ := navWidth()
	_ = otmux("resize-pane", "-t", outerSession+":0.0", "-x", strconv.Itoa(w)).Run()
}

// navWidth : largeur mémorisée, ramenée au défaut si elle ne laisserait plus
// de place à la zone tmux ; renvoie aussi la largeur de la fenêtre coque.
func navWidth() (nav, win int) {
	nav = loadNavWidth()
	if b, err := otmux("display-message", "-p", "-t", outerSession+":0", "#{window_width}").Output(); err == nil {
		if win, _ = strconv.Atoi(strings.TrimSpace(string(b))); win > 0 && nav > win-20 {
			nav = defaultNavW
		}
	}
	return nav, win
}

// ── Coque ─────────────────────────────────────────────────────────────────

func stateCommand(exe string) string {
	return "CL_HUB=" + shq(hubSession) + " CL_INNER_SOCKET=" + shq(os.Getenv("CL_INNER_SOCKET")) + " " + shq(exe) + " state"
}

func navCommand(exe string) string {
	return fmt.Sprintf("CL_SHELL=1 CL_HUB=%s CL_OUTER_SOCKET=%s CL_INNER_SOCKET=%s %s nav --launch",
		shq(hubSession), shq(outerSocket()), shq(os.Getenv("CL_INNER_SOCKET")), shq(exe))
}

// paneStale : le pane exécute-t-il une autre copie du launcher que exe (binaire
// supprimé ou remplacé par une mise à jour) ? On regarde le processus du pane
// et ses enfants directs (cas où sh -c n'a pas fait exec).
func paneStale(tm func(...string) *exec.Cmd, target, exe string) bool {
	b, err := tm("display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(b))
	pids := []string{pid}
	if c, err := os.ReadFile("/proc/" + pid + "/task/" + pid + "/children"); err == nil {
		pids = append(pids, strings.Fields(string(c))...)
	}
	for _, p := range pids {
		l, err := os.Readlink("/proc/" + p + "/exe")
		if err != nil || l == exe {
			continue
		}
		if filepath.Base(strings.TrimSuffix(l, " (deleted)")) == filepath.Base(exe) {
			return true
		}
	}
	return false
}

// ensureInner : crée claude-hub s'il manque (premier onglet : « ⌗ Teamleaders »)
// et retire l'ancien onglet « ◈ État », remplacé par le pane d'infos de la coque.
func ensureInner() error {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		if out, err := dtmux("new-session", "-d", "-s", hubSession, "-n", teamWindow, teamPlaceholder).CombinedOutput(); err != nil {
			return fmt.Errorf("new-session %s : %v (%s)", hubSession, err, strings.TrimSpace(string(out)))
		}
	}
	dropStateWindow()
	// Barre d'onglets en haut ; le style projet est posé par window à l'ouverture.
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status", "on").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-position", "top").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-justify", "left").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-left", "").Run()
	_ = dtmux("set-option", "-t", exactS(hubSession)+":", "status-right", "#[fg=colour244]%H:%M ").Run()
	_ = dtmux("select-window", "-t", exactW(hubSession, teamWindow)).Run()
	bindZoom()
	return nil
}

// tmuxDoubleClick : liaison DoubleClick1Pane par défaut de tmux (sélection d'un mot).
const tmuxDoubleClick = `select-pane -t = ; if-shell -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -H ; send-keys -X select-word ; run-shell -d 0.3 ; send-keys -X copy-pipe-and-cancel }`

// bindZoom : dans claude-hub, un double-clic sur un pane le passe en plein écran
// (comme préfixe+z) et un nouveau double-clic le remet en taille normale. Les
// liaisons sont globales au serveur : les autres sessions gardent le comportement
// par défaut.
func bindZoom() {
	cond := "#{==:#{session_name}," + hubSession + "}"
	_ = dtmux("bind-key", "-n", "DoubleClick1Pane", "if-shell", "-F", cond,
		"select-pane -t = ; resize-pane -Z -t =", tmuxDoubleClick).Run()
}

// dropStateWindow ferme l'onglet « ◈ État » des versions ≤ v3.0.8.
func dropStateWindow() {
	b, _ := dtmux("list-windows", "-t", exactS(hubSession), "-F", "#{window_name}").Output()
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == stateWindow {
			_ = dtmux("kill-window", "-t", exactW(hubSession, stateWindow)).Run()
		}
	}
}

// ── Panes de la coque ─────────────────────────────────────────────────────
//
// navigateur en haut à gauche, infos du projet sélectionné dessous, zone tmux
// (claude-hub) à droite sur toute la hauteur. Chaque pane porte son rôle dans
// l'option @cl : les index changent quand un pane est recréé.

const (
	roleNav  = "nav"
	roleInfo = "info"
	roleHub  = "hub"
)

// outerPanes : rôle -> id de pane (%N) ; untagged : panes sans rôle, par index.
func outerPanes() (roles map[string]string, untagged []string) {
	roles = map[string]string{}
	b, err := otmux("list-panes", "-t", outerSession+":0", "-F", "#{pane_id}|#{@cl}").Output()
	if err != nil {
		return roles, nil
	}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		id, role, _ := strings.Cut(l, "|")
		switch {
		case id == "":
		case role == "":
			untagged = append(untagged, id)
		default:
			roles[role] = id
		}
	}
	return roles, untagged
}

func outerPane(role string) string {
	roles, _ := outerPanes()
	return roles[role]
}

func tagPane(id, role string) { _ = otmux("set-option", "-p", "-t", id, "@cl", role).Run() }

// ensureLayout complète la coque : rôles posés (coque d'avant les rôles :
// pane 0 = navigateur, l'autre = zone tmux), zone tmux et pane d'infos recréés
// s'ils manquent, ancien onglet « ◈ État » fermé. Appelé par la coque et par
// un navigateur redémarré après mise à jour : un verrou évite les doublons.
func ensureLayout(exe string) error {
	if f, err := os.OpenFile(filepath.Join(cacheDir(), "layout.lock"), os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer f.Close()
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	roles, untagged := outerPanes()
	if len(roles) == 0 && len(untagged) > 0 {
		tagPane(untagged[0], roleNav)
		roles[roleNav] = untagged[0]
		if len(untagged) == 2 {
			tagPane(untagged[1], roleHub)
			roles[roleHub] = untagged[1]
		}
	}
	nav := roles[roleNav]
	if nav == "" {
		return fmt.Errorf("coque sans navigateur")
	}
	if roles[roleHub] == "" {
		// -f : pleine hauteur, à droite du navigateur et du pane d'infos.
		// Taille posée d'emblée : un split à 50 % serait mémorisé comme un glissement.
		args := []string{"split-window", "-h", "-f", "-d", "-P", "-F", "#{pane_id}", "-t", nav}
		if w, win := navWidth(); win > w+1 {
			args = append(args, "-l", strconv.Itoa(win-w-1))
		}
		out, err := otmux(append(args, innerCommand())...).Output()
		if err != nil {
			return fmt.Errorf("tmux split-window (zone tmux) : %v", err)
		}
		roles[roleHub] = strings.TrimSpace(string(out))
		tagPane(roles[roleHub], roleHub)
	}
	if roles[roleInfo] == "" {
		out, err := otmux("split-window", "-v", "-d", "-P", "-F", "#{pane_id}", "-l", "45%", "-t", nav, stateCommand(exe)).Output()
		if err != nil {
			return fmt.Errorf("tmux split-window (infos) : %v", err)
		}
		tagPane(strings.TrimSpace(string(out)), roleInfo)
	} else if paneStale(otmux, roles[roleInfo], exe) {
		_ = otmux("respawn-pane", "-k", "-t", roles[roleInfo], stateCommand(exe)).Run()
	}
	_ = otmux("bind-key", "-n", "M-Left", "select-pane", "-t", nav).Run()
	_ = otmux("bind-key", "-n", "M-Right", "select-pane", "-t", roles[roleHub]).Run()
	dropStateWindow()
	return nil
}

func innerCommand() string {
	innerSock := ""
	if s := os.Getenv("CL_INNER_SOCKET"); s != "" {
		innerSock = "-L " + shq(s) + " "
	}
	return fmt.Sprintf("env -u TMUX tmux %sattach-session -t %s", innerSock, shq(exactS(hubSession)))
}

func runShell() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := ensureInner(); err != nil {
		return err
	}
	nav := navCommand(exe)
	if otmux("has-session", "-t", outerSession).Run() != nil {
		steps := [][]string{
			{"-f", "/dev/null", "new-session", "-d", "-s", outerSession, "-n", "ui", nav},
			// Coque transparente : pas de barre, pas de préfixe (tout passe au tmux intérieur).
			{"set-option", "-t", outerSession, "status", "off"},
			{"set-option", "-g", "prefix", "None"},
			{"set-option", "-g", "mouse", "on"},
			{"set-option", "-g", "escape-time", "10"},
			{"set-option", "-g", "pane-border-style", "fg=colour238"},
			{"set-option", "-g", "pane-active-border-style", "fg=colour27"},
			{"set-option", "-g", "pane-border-lines", "heavy"},
			// tmux refocalise le pane sous la souris à la fin d'un double-clic, ce qui
			// annulerait le passage du focus à la zone tmux après « ouvrir » : on se
			// contente de transmettre l'événement au programme du pane.
			{"bind-key", "-n", "DoubleClick1Pane", "send-keys", "-M"},
			{"bind-key", "-n", "TripleClick1Pane", "send-keys", "-M"},
			{"set-hook", "-t", outerSession, "client-attached", "run-shell " + shq(shq(exe)+" fixwidth")},
			{"set-hook", "-t", outerSession, "client-resized", "run-shell " + shq(shq(exe)+" fixwidth")},
			{"set-option", "-p", "-t", outerSession + ":0.0", "@cl", roleNav},
		}
		for _, s := range steps {
			if out, err := otmux(s...).CombinedOutput(); err != nil {
				return fmt.Errorf("tmux %s : %v (%s)", s[0], err, strings.TrimSpace(string(out)))
			}
		}
	} else {
		// Coque survivante dont le navigateur tourne sur un binaire remplacé
		// depuis (mise à jour) : on le redémarre sur le binaire installé.
		p := outerPane(roleNav)
		if p == "" {
			p = outerSession + ":0.0" // coque d'avant les rôles
		}
		if paneStale(otmux, p, exe) {
			_ = otmux("respawn-pane", "-k", "-t", p, nav).Run()
		}
	}
	if err := ensureLayout(exe); err != nil {
		return err
	}
	_ = otmux("select-pane", "-t", outerPane(roleNav)).Run()
	// Attache le terminal courant à la coque (environnement propre : pas d'imbrication).
	c := exec.Command("tmux", "-L", outerSocket(), "attach-session", "-t", outerSession)
	c.Env = envWithout("TMUX", "TMUX_PANE")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
