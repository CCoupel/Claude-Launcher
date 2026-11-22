package tmux

import (
	"strings"
	"sync"

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

// attnPublished : @cl_attn réellement posé, par fenêtre (cible tmux -> id de l'attente) ; une écriture qui échoue
// (fenêtre pas encore là, tmux en échec) n'est pas comptée publiée : elle est retentée au tour suivant.
// setWinOption : écriture d'une option de fenêtre du hub (remplacée dans les tests : échec simulé).
var setWinOption = func(args ...string) error { return hub.Run(args...) }

var (
	attnPubMu     sync.Mutex
	attnPublished = map[string]string{}
)

// attentionCmd : relevé des attentes des projets ouverts ici ou vivants ailleurs ; publie @cl_attn par fenêtre ouverte.
// Projet ouvert : seul le fichier du pane leader de la fenêtre compte (garde-fou si un teammate héritait de
// CL_ATTENTION_DIR) ; leader introuvable : aucune attente (pane disparu = état périmé ignoré).
func (m model) attentionCmd() tea.Cmd {
	type target struct {
		path, key string
		open      bool
	}
	var ts []target
	for _, n := range m.nodes {
		if !n.Project {
			continue
		}
		open := m.windows[n.Key]
		if p := m.projInfo(n); open || p.hasLive && p.live.Claude {
			ts = append(ts, target{n.Path, n.Key, open})
		}
	}
	session := m.session
	return func() tea.Msg {
		out := attnMsg{}
		for _, t := range ts {
			leader := ""
			if t.open {
				if leader = mirrorLeader(projectOf(exactW(targetSession(session), t.key))); leader == "" {
					continue
				}
			}
			if a, ok := core.ReadAttentionOf(t.path, leader); ok {
				out[t.path] = a
			}
		}
		for _, t := range ts { // option de fenêtre : seulement quand l'attente change
			if !t.open {
				continue
			}
			win := exactW(targetSession(session), t.key)
			a, has := out[t.path]
			attnPubMu.Lock()
			pub, wasPub := attnPublished[win]
			attnPubMu.Unlock()
			switch {
			case has && (!wasPub || pub != a.ID):
				if setWinOption("set-option", "-w", "-t", win, "@cl_attn", a.Kind+"|"+a.Text) == nil {
					attnPubMu.Lock()
					attnPublished[win] = a.ID
					attnPubMu.Unlock()
				}
			case !has && wasPub:
				if setWinOption("set-option", "-w", "-u", "-t", win, "@cl_attn") == nil {
					attnPubMu.Lock()
					delete(attnPublished, win)
					attnPubMu.Unlock()
				}
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
