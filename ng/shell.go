package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	_ = otmux("resize-pane", "-t", outerSession+":0.0", "-x", strconv.Itoa(loadNavWidth())).Run()
}

// ── Coque ─────────────────────────────────────────────────────────────────

// ensureInner : crée claude-hub s'il manque, et épingle l'onglet « ◈ État »
// en première position.
func ensureInner(exe string) error {
	stateCmd := "CL_HUB=" + shq(hubSession) + " CL_INNER_SOCKET=" + shq(os.Getenv("CL_INNER_SOCKET")) + " " + shq(exe) + " state"
	if dtmux("has-session", "-t", hubSession).Run() != nil {
		if out, err := dtmux("new-session", "-d", "-s", hubSession, "-n", stateWindow, stateCmd).CombinedOutput(); err != nil {
			return fmt.Errorf("new-session %s : %v (%s)", hubSession, err, strings.TrimSpace(string(out)))
		}
	} else {
		b, _ := dtmux("list-windows", "-t", hubSession, "-F", "#{window_name}").Output()
		has := false
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) == stateWindow {
				has = true
			}
		}
		if !has {
			if out, err := dtmux("new-window", "-d", "-b", "-n", stateWindow, "-t", hubSession+":^", stateCmd).CombinedOutput(); err != nil {
				return fmt.Errorf("new-window %s : %v (%s)", stateWindow, err, strings.TrimSpace(string(out)))
			}
		}
	}
	// Barre d'onglets en haut ; le style projet est posé par window à l'ouverture.
	_ = dtmux("set-option", "-t", hubSession, "status", "on").Run()
	_ = dtmux("set-option", "-t", hubSession, "status-position", "top").Run()
	_ = dtmux("set-option", "-t", hubSession, "status-justify", "left").Run()
	_ = dtmux("set-option", "-t", hubSession, "status-left", "").Run()
	_ = dtmux("set-option", "-t", hubSession, "status-right", "#[fg=colour244]%H:%M ").Run()
	_ = dtmux("select-window", "-t", hubSession+":"+stateWindow).Run()
	return nil
}

func runShell() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := ensureInner(exe); err != nil {
		return err
	}
	if otmux("has-session", "-t", outerSession).Run() != nil {
		innerSock := ""
		if s := os.Getenv("CL_INNER_SOCKET"); s != "" {
			innerSock = "-L " + shq(s) + " "
		}
		nav := fmt.Sprintf("CL_SHELL=1 CL_HUB=%s CL_OUTER_SOCKET=%s CL_INNER_SOCKET=%s %s nav --launch",
			shq(hubSession), shq(outerSocket()), shq(os.Getenv("CL_INNER_SOCKET")), shq(exe))
		inner := fmt.Sprintf("env -u TMUX tmux %sattach-session -t %s", innerSock, hubSession)
		steps := [][]string{
			{"-f", "/dev/null", "new-session", "-d", "-s", outerSession, "-n", "ui", nav},
			{"split-window", "-h", "-t", outerSession + ":0.0", inner},
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
			{"bind-key", "-n", "M-Left", "select-pane", "-t", outerSession + ":0.0"},
			{"bind-key", "-n", "M-Right", "select-pane", "-t", outerSession + ":0.1"},
			{"set-hook", "-t", outerSession, "client-attached", "run-shell " + shq(shq(exe)+" fixwidth")},
			{"set-hook", "-t", outerSession, "client-resized", "run-shell " + shq(shq(exe)+" fixwidth")},
			{"select-pane", "-t", outerSession + ":0.0"},
		}
		for _, s := range steps {
			if out, err := otmux(s...).CombinedOutput(); err != nil {
				return fmt.Errorf("tmux %s : %v (%s)", s[0], err, strings.TrimSpace(string(out)))
			}
		}
	}
	// Attache le terminal courant à la coque (environnement propre : pas d'imbrication).
	c := exec.Command("tmux", "-L", outerSocket(), "attach-session", "-t", outerSession)
	c.Env = envWithout("TMUX", "TMUX_PANE")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
