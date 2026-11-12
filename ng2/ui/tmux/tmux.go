package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"claude-launcher-ng2/core"
)

// dtmux : commande tmux sur le serveur intérieur (TMUX retiré de l'environnement,
// sinon tmux viserait le serveur de la coque dans laquelle tourne le navigateur).
func dtmux(args ...string) *exec.Cmd {
	c := exec.Command("tmux", append([]string{"-L", core.InnerSocket()}, args...)...)
	c.Env = core.EnvWithout("TMUX", "TMUX_PANE")
	return c
}

func outerSocket() string {
	if s := os.Getenv("CL_OUTER_SOCKET"); s != "" {
		return s
	}
	return "claude-ui-ng2"
}

// otmux : commande tmux sur le serveur de la coque.
func otmux(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", outerSocket()}, args...)...)
}

func tmuxSession() string {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() == nil {
		return hubSession
	}
	return "(aucune session " + hubSession + ")"
}

// tmuxWindows : un seul appel tmux pour savoir quels projets sont ouverts.
func tmuxWindows(session string) map[string]bool {
	out := map[string]bool{}
	b, err := dtmux("list-windows", "-t", exactS(session), "-F", "#{window_name}").Output()
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[l] = true
		}
	}
	return out
}

// openProject : bascule sur la fenêtre existante ou en crée une. Sans --launch
// le navigateur n'agit pas (il décrit seulement ce qu'il ferait).
func openProject(cfg core.Config, session string, n core.Node, launch bool, latest string, mode launchMode) string {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		return "pas de session " + hubSession + " (lancer « claude-launcher-ng2 shell »)"
	}
	if !launch {
		if tmuxWindows(targetSession(session))[n.Key] {
			return "[dry-run] select-window " + n.Key
		}
		return "[dry-run] new-window " + n.Key + " → claude (couleur " + cfg.ProjectColor(n.Key) + ")"
	}
	if err := launchProject(cfg, session, n, latest, mode); err != nil {
		return fmt.Sprintf("⚠ %v", err)
	}
	return ""
}

func shorten(s string, w int) string {
	if w < 4 || utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-(w-1):])
}
