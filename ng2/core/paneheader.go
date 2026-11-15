package core

import (
	"strings"
)

// ── En-tête des panes : état d'activité (#152) ────────────────────────────
//
// Une seule source pour la coque Linux et l'interface native : l'état d'un pane
// (lead / active / idle) vient de la même donnée que la teinte du fond
// (TeamMember.IsActive, cf. PaneShade) et que la carte TEAM. Contrat :
// contracts/tmux-options.md (@cl_state, @cl_name, pane-border-*).

// États d'un pane (@cl_state, layout.panes[].state).
const (
	PaneLead   = "lead"   // leader ou agent cdp : en-tête neutre
	PaneActive = "active" // teammate qui travaille
	PaneIdle   = "idle"   // teammate inactif
	PaneNone   = ""       // pane sans agent
)

// PaneState : état d'un pane. leader : pane du leader (plus petit %id) ; m : son
// membre de la team, nil si le pane n'a pas d'agent.
func PaneState(m *TeamMember, leader bool) string {
	switch {
	case leader:
		return PaneLead
	case m == nil:
		return PaneNone
	case m.AgentType == "cdp" || m.Name == "team-lead":
		return PaneLead
	case m.IsActive:
		return PaneActive
	}
	return PaneIdle
}

// PaneHeader : état et nom à poser sur un pane (@cl_state, @cl_name).
type PaneHeader struct {
	Pane, State, Name string
}

// PaneHeaders : état et nom de chacun des panes ids de la fenêtre, d'après la
// team tc (nil : aucun agent connu) ; leader : le pane du leader.
func PaneHeaders(ids []string, leader string, tc *TeamConfig) []PaneHeader {
	member := map[string]TeamMember{}
	if tc != nil {
		for _, m := range tc.Members {
			if m.PaneID != "" {
				member[m.PaneID] = m
			}
		}
	}
	out := make([]PaneHeader, 0, len(ids))
	for _, id := range ids {
		var mp *TeamMember
		if m, ok := member[id]; ok {
			mp = &m
		}
		h := PaneHeader{Pane: id, State: PaneState(mp, id == leader)}
		switch {
		case mp != nil && mp.Name != "":
			h.Name = mp.Name
		case h.State == PaneLead:
			h.Name = "team-lead"
		}
		if h.State == PaneNone {
			h.Name = ""
		}
		out = append(out, h)
	}
	return out
}

// HeaderSignature : change quand un état, un nom ou la liste des panes change
// (le surveillant ne relit tmux que dans ce cas).
func HeaderSignature(hs []PaneHeader) string {
	var b strings.Builder
	for _, h := range hs {
		b.WriteString(h.Pane + "=" + h.State + ":" + h.Name + ";")
	}
	return b.String()
}

// Couleurs de l'en-tête (maquette retenue, variante A : bandeau plein par état).
const (
	headerActive = "fg=#06281a,bg=#6ee7a8" // vert, texte sombre
	headerIdle   = "fg=#aeb4be,bg=#3a404b" // gris sombre
	headerLead   = "fg=#15181d,bg=#dde1e7" // clair, neutre
)

// PaneBorderStatus : valeur de pane-border-status posée sur la fenêtre du projet.
const PaneBorderStatus = "top"

// PaneBorderFormat : pane-border-format — « nom · actif » (vert), « nom · inactif »
// (gris sombre), « nom · leader » (neutre) ; la commande du pane seule quand
// @cl_state est absent (pane sans agent).
func PaneBorderFormat() string {
	return "#{?#{==:#{@cl_state},active},#[" + headerActive + " bold] #{@cl_name} · actif #[default]," +
		"#{?#{==:#{@cl_state},idle},#[" + headerIdle + "] #{@cl_name} · inactif #[default]," +
		"#{?#{==:#{@cl_state},lead},#[" + headerLead + "] #{@cl_name} · leader #[default],#{pane_current_command}}}}"
}

// SetWindowHeaders : options de fenêtre (une ligne de titre par pane) ; ne réécrit
// que si elles diffèrent. win : cible de la fenêtre du projet.
func SetWindowHeaders(t Target, win string) {
	cur := func(opt string) string {
		b, err := t.Output("show-options", "-wqv", "-t", win, opt)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if cur("pane-border-status") != PaneBorderStatus {
		_ = t.Run("set-option", "-w", "-t", win, "pane-border-status", PaneBorderStatus)
	}
	if cur("pane-border-format") != PaneBorderFormat() {
		_ = t.Run("set-option", "-w", "-t", win, "pane-border-format", PaneBorderFormat())
	}
}

// SyncPaneHeaders : @cl_state et @cl_name des panes de la fenêtre win ; relit l'état
// réel (un seul list-panes) et ne réécrit que les écarts. Rend le nombre de panes
// modifiés. Un pane sans agent perd les deux options.
func SyncPaneHeaders(t Target, win string, want []PaneHeader) int {
	out, err := t.Output("list-panes", "-t", win, "-F", "#{pane_id}|#{@cl_state}|#{@cl_name}")
	if err != nil {
		return 0
	}
	cur := map[string][2]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) == 3 && f[0] != "" {
			cur[f[0]] = [2]string{f[1], f[2]}
		}
	}
	n := 0
	for _, h := range want {
		c, ok := cur[h.Pane]
		if !ok || c == [2]string{h.State, h.Name} {
			continue
		}
		n++
		if h.State == PaneNone {
			_ = t.Run("set-option", "-p", "-u", "-t", h.Pane, "@cl_state")
			_ = t.Run("set-option", "-p", "-u", "-t", h.Pane, "@cl_name")
			continue
		}
		if c[0] != h.State {
			_ = t.Run("set-option", "-p", "-t", h.Pane, "@cl_state", h.State)
		}
		if c[1] != h.Name {
			_ = t.Run("set-option", "-p", "-t", h.Pane, "@cl_name", h.Name)
		}
	}
	return n
}
