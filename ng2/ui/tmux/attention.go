package tmux

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"claude-launcher-ng2/core"
)

// Teamleader en attente de réponse (#223, contracts/attention.md § 8) : le navigateur lit les dossiers d'état (un tour
// toutes les 2 s, avec les fenêtres) des projets ouverts ou dont la session vit ailleurs, affiche la pastille
// « ❓ en attente » sur la ligne du projet et publie l'attente dans l'option de fenêtre @cl_attn — lue par les vignettes
// de « ⌗ Teamleaders » (bordure orange, titre, ligne de la question). Pas de notification système.

// attnBadge : pastille de la ligne du projet (maquette A : fond orange).
var attnBadge = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Bold(true).Render(" ❓ en attente ")

type attnMsg map[string]core.Attention

// attentionCmd : relevé des attentes des projets ouverts ici ou vivants ailleurs ; publie @cl_attn par fenêtre ouverte.
func (m model) attentionCmd() tea.Cmd {
	type target struct{ path, key string }
	var ts []target
	for _, n := range m.nodes {
		if !n.Project {
			continue
		}
		open := m.windows[n.Key]
		if p := m.projInfo(n); open || p.hasLive && p.live.Claude {
			ts = append(ts, target{n.Path, n.Key})
		}
	}
	prev := m.attn
	session := m.session
	return func() tea.Msg {
		out := attnMsg{}
		for _, t := range ts {
			if a, ok := core.ReadAttention(t.path); ok {
				out[t.path] = a
			}
		}
		for _, t := range ts { // option de fenêtre : seulement quand l'attente change
			a, has := out[t.path]
			old, hadOld := prev[t.path]
			if has == hadOld && (!has || a.ID == old.ID) {
				continue
			}
			win := exactW(targetSession(session), t.key)
			if has {
				_ = hub.Run("set-option", "-w", "-t", win, "@cl_attn", a.Kind+"|"+a.Text)
			} else {
				_ = hub.Run("set-option", "-w", "-u", "-t", win, "@cl_attn")
			}
		}
		return out
	}
}

// parseAttnOption : « kind|texte » de @cl_attn (vide : pas d'attente).
func parseAttnOption(s string) (kind, text string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	kind, text, _ = strings.Cut(s, "|")
	return kind, text, true
}

// attnTitle : étiquette de la question dans la vignette (« ❓ question : … » / « ❓ permission : … »).
func attnTitle(kind, text string) string {
	if text == "" {
		return "❓ " + kind
	}
	return "❓ " + kind + " : " + text
}
