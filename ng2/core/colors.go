package core

import (
	"github.com/charmbracelet/lipgloss"
)

// ── Rendu ────────────────────────────────────────────────────────────────

var (
	Dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	Bold   = lipgloss.NewStyle().Bold(true)
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	Orange = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	Blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	Cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

// Style : rendu lipgloss d'une couleur sémantique (legend.go), identique aux
// styles ci-dessus (Red, Blue et Cyan y sont déjà gras).
func (t Tone) Style() lipgloss.Style {
	var s lipgloss.Style
	switch t.Color {
	case "green":
		s = Green
	case "yellow":
		s = Yellow
	case "red":
		s = Red
	case "orange":
		s = Orange
	case "blue":
		s = Blue
	case "cyan":
		s = Cyan
	case "purple":
		s = MsPurple
	default:
		s = Dim
	}
	if t.Bold {
		s = s.Bold(true)
	}
	return s
}
