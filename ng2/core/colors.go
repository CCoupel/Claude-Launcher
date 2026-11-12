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
