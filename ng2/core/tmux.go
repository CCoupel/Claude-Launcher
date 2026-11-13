package core

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Deux serveurs tmux cohabitent :
//   - « intérieur » : serveur dédié (socket claude-ng2), où vivent claude-hub-ng2
//     et les fenêtres projet. NG2 n'utilise jamais le serveur tmux par défaut :
//     ses bindings globaux (zoom, R) n'écrasent pas ceux d'un launcher v4 ;
//   - « coque »     : serveur dédié (socket claude-ui-ng2) qui découpe le terminal
//     en navigateur (gauche) + tmux intérieur (droite).
//
// CL_INNER_SOCKET / CL_OUTER_SOCKET permettent d'isoler les deux pour les tests.

func EnvWithout(drop ...string) []string {
	var out []string
	for _, e := range os.Environ() {
		keep := true
		for _, d := range drop {
			if strings.HasPrefix(e, d+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

// InnerSocket : socket du serveur intérieur (CL_INNER_SOCKET pour les tests).
func InnerSocket() string {
	if s := os.Getenv("CL_INNER_SOCKET"); s != "" {
		return s
	}
	return "claude-ng2"
}

func PadRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
