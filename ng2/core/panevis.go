package core

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Panes cachés (#136, contracts/tmux-options.md) : un teammate inactif est mis
// de côté dans une session de réserve du même serveur tmux (break-pane -d), et
// ramené par join-pane -d. Le %id et le processus sont conservés. La règle est
// pure (DecidePaneVis) ; son application (ApplyPaneVis) ne vérifie pas la main :
// l'appelant ne l'invoque que s'il est le launcher qui a la main (@cl_owner).

const (
	// HiddenSession : session de réserve, créée à la demande, sans client.
	HiddenSession = "cl-hidden"
	// HomeCtr : valeur de @cl_home dans le tmux d'un conteneur (un seul projet).
	HomeCtr = "ctr"

	optHome   = "@cl_home"    // option de pane : fenêtre d'origine d'un pane caché
	optIdleAt = "@cl_idle_at" // option de pane : début d'inactivité (heure Unix)
	optVis    = "@cl_panevis" // option de fenêtre : choix manuels « %7:h %9:s »

	visHidden = "h" // caché à la main
	visPinned = "s" // visible fixé à la main
)

// ── Choix manuels (@cl_panevis) ───────────────────────────────────────────

// ParsePaneVis : « %7:h %9:s » → pane → mode ; entrées mal formées ignorées.
func ParsePaneVis(s string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Fields(s) {
		p, mode, ok := strings.Cut(f, ":")
		if ok && strings.HasPrefix(p, "%") && (mode == visHidden || mode == visPinned) {
			m[p] = mode
		}
	}
	return m
}

// FormatPaneVis : forme canonique (triée) de ParsePaneVis.
func FormatPaneVis(m map[string]string) string {
	ids := make([]string, 0, len(m))
	for p := range m {
		ids = append(ids, p)
	}
	sort.Strings(ids)
	out := make([]string, len(ids))
	for i, p := range ids {
		out[i] = p + ":" + m[p]
	}
	return strings.Join(out, " ")
}

// ── Règle (fonction pure) ─────────────────────────────────────────────────

// VisPane : un pane de la fenêtre du projet (visible ou caché) pour la règle.
type VisPane struct {
	ID        string
	Protected bool  // leader, agent cdp, pane sans agent : toujours visible
	Active    bool  // teammate qui travaille
	Focus     bool  // pane qui a le focus clavier de la fenêtre
	IdleAt    int64 // @cl_idle_at (0 : inconnu)
	Hidden    bool  // actuellement dans la réserve
}

// VisDecision : résultat de la règle.
type VisDecision struct {
	Hide    map[string]bool   // panes qui doivent être cachés
	Choices map[string]string // choix manuels après nettoyage
	IdleAt  map[string]int64  // nouvelle @cl_idle_at (0 : à retirer) — seulement les changements
}

// DecidePaneVis applique la règle du contrat. hideIdle : TEAM_HIDE_IDLE en
// secondes (0 : jamais de masquage automatique), now : heure Unix.
func DecidePaneVis(panes []VisPane, choices map[string]string, hideIdle int, now int64) VisDecision {
	d := VisDecision{Hide: map[string]bool{}, Choices: map[string]string{}, IdleAt: map[string]int64{}}
	for _, p := range panes {
		mode := choices[p.ID]
		switch {
		case p.Protected:
			// rien : visible, jamais d'entrée utile
			if p.IdleAt != 0 {
				d.IdleAt[p.ID] = 0
			}
			continue
		case p.Active:
			// toujours visible ; ses entrées sont retirées
			if p.IdleAt != 0 {
				d.IdleAt[p.ID] = 0
			}
			continue
		}
		// inactif : début d'inactivité connu dès qu'on le voit
		idleAt := p.IdleAt
		if idleAt == 0 {
			idleAt = now
			d.IdleAt[p.ID] = now
		}
		if mode != "" {
			d.Choices[p.ID] = mode
		}
		switch {
		case mode == visPinned:
		case mode == visHidden:
			d.Hide[p.ID] = true
		case hideIdle > 0 && now-idleAt >= int64(hideIdle) && !p.Focus:
			d.Hide[p.ID] = true
		}
	}
	return d
}

// ToggleDenied : raison du refus d'une bascule manuelle sur p, "" si permise.
func ToggleDenied(p VisPane, known bool) string {
	switch {
	case !known:
		return "pane inconnu"
	case p.Protected:
		return "le leader, un agent cdp ou un pane sans agent reste toujours visible"
	case p.Active:
		return "un teammate actif reste visible"
	}
	return ""
}

// ToggleChoice : nouveau mode manuel après un double-clic sur un inactif :
// caché → visible fixé (s), visible → caché à la main (h).
func ToggleChoice(hidden bool) string {
	if hidden {
		return visPinned
	}
	return visHidden
}

// ── Recensement unique (fenêtre + réserve) ────────────────────────────────

// ProjPane : un pane du projet, caché ou non.
type ProjPane struct {
	PaneInfo
	Focus  bool
	IdleAt int64
	Hidden bool
}

// ProjectPanes : recensement des panes d'un projet — la fenêtre win et les
// panes de la réserve dont @cl_home vaut home — avec les choix manuels de la
// fenêtre. inCtr : PID d'un autre espace de noms, laissé vide. Un seul code
// pour la carte TEAM, la règle, le comptage à la fermeture et les versions.
func ProjectPanes(t SessionTmux, win, home string, inCtr bool) ([]ProjPane, map[string]string, error) {
	out, err := t("list-panes", "-t", win, "-F", paneFmt)
	if err != nil {
		return nil, nil, err
	}
	var panes []ProjPane
	var choices map[string]string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if p, vis, ok := parsePaneLine(l, inCtr); ok {
			p.Hidden = false
			panes = append(panes, p)
			if choices == nil {
				choices = ParsePaneVis(vis)
			}
		}
	}
	if choices == nil {
		choices = map[string]string{}
	}
	for _, p := range hiddenPanes(t, home, inCtr) {
		panes = append(panes, p)
	}
	return panes, choices, nil
}

// paneFmt : le « . » final protège les champs vides de fin de ligne, que certains
// appelants (tmuxOut) rognent avec TrimSpace.
const paneFmt = "#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{pane_active}\t#{@cl_idle_at}\t#{@cl_home}\t#{@cl_panevis}\t."

func parsePaneLine(l string, inCtr bool) (ProjPane, string, bool) {
	f := strings.Split(l, "\t")
	if len(f) < 8 || !strings.HasPrefix(f[0], "%") {
		return ProjPane{}, "", false
	}
	p := ProjPane{PaneInfo: PaneInfo{ID: f[0], PID: f[1], Cmd: f[2]}, Focus: f[3] == "1"}
	if inCtr {
		p.PID = ""
	}
	p.IdleAt, _ = strconv.ParseInt(f[4], 10, 64)
	p.Hidden = f[5] != ""
	return p, f[6], true
}

// hiddenPanes : panes de la réserve rattachés à home (aucun si pas de réserve).
func hiddenPanes(t SessionTmux, home string, inCtr bool) []ProjPane {
	out, err := t("list-panes", "-s", "-t", "="+HiddenSession, "-F", paneFmt)
	if err != nil {
		return nil
	}
	var ps []ProjPane
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(l, "\t")
		if p, _, ok := parsePaneLine(l, inCtr); ok && len(f) > 5 && f[5] == home {
			p.Hidden, p.Focus = true, false
			ps = append(ps, p)
		}
	}
	return ps
}

// ProjectHome : valeur de @cl_home des panes du projet dont la fenêtre est win.
func ProjectHome(t SessionTmux, win string, inCtr bool) string {
	if inCtr {
		return HomeCtr
	}
	s, err := t("display-message", "-p", "-t", win, "#{window_id}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// ── Application à un tmux ─────────────────────────────────────────────────

// PaneRole : à partir d'un pane et de la team, le pane est-il protégé ?
// leader : le plus petit %id du projet ; cdp et panes absents de la team aussi.
func visPanes(panes []ProjPane, tc *TeamConfig) []VisPane {
	leader := ""
	best := -1
	for _, p := range panes {
		if n, err := strconv.Atoi(strings.TrimPrefix(p.ID, "%")); err == nil && (best < 0 || n < best) {
			best, leader = n, p.ID
		}
	}
	member := map[string]TeamMember{}
	if tc != nil {
		for _, m := range tc.Members {
			if m.PaneID != "" {
				member[m.PaneID] = m
			}
		}
	}
	out := make([]VisPane, 0, len(panes))
	for _, p := range panes {
		m, ok := member[p.ID]
		out = append(out, VisPane{ID: p.ID, Focus: p.Focus, IdleAt: p.IdleAt, Hidden: p.Hidden,
			Protected: p.ID == leader || !ok || m.AgentType == "cdp" || m.Name == "team-lead",
			Active:    ok && m.IsActive})
	}
	return out
}

// ApplyResult : panes déplacés par ApplyPaneVis (la mise en page des panes
// visibles est à refaire par l'appelant si l'un des deux est non vide).
type ApplyResult struct{ Hid, Shown []string }

func (r ApplyResult) Changed() bool { return len(r.Hid)+len(r.Shown) > 0 }

// ApplyPaneVis lit l'état réel de la fenêtre win (et de la réserve), applique
// la règle et réalise les transitions : options @cl_idle_at / @cl_panevis,
// break-pane -d vers la réserve, join-pane -d dans la fenêtre. Le pane qui a le
// focus n'est jamais caché. Aucun effet sur les panes protégés.
func ApplyPaneVis(t SessionTmux, win, home string, tc *TeamConfig, hideIdle int, now int64, inCtr bool) (ApplyResult, error) {
	var res ApplyResult
	panes, choices, err := ProjectPanes(t, win, home, inCtr)
	if err != nil {
		return res, err
	}
	vp := visPanes(panes, tc)
	d := DecidePaneVis(vp, choices, hideIdle, now)
	for id, at := range d.IdleAt {
		if at == 0 {
			_, _ = t("set-option", "-p", "-u", "-t", id, optIdleAt)
		} else {
			_, _ = t("set-option", "-p", "-t", id, optIdleAt, strconv.FormatInt(at, 10))
		}
	}
	// choix : ne garder que ceux des panes inactifs encore présents
	if want := FormatPaneVis(d.Choices); want != FormatPaneVis(choices) {
		if want == "" {
			_, _ = t("set-option", "-w", "-u", "-t", win, optVis)
		} else {
			_, _ = t("set-option", "-w", "-t", win, optVis, want)
		}
	}
	anchor := "" // pane visible qui accueille un retour
	for _, p := range vp {
		if !p.Hidden && (anchor == "" || p.Protected) {
			anchor = p.ID
		}
	}
	for _, p := range vp {
		switch {
		case p.Hidden && !d.Hide[p.ID]:
			if anchor == "" {
				continue
			}
			if _, err := t("join-pane", "-d", "-s", p.ID, "-t", anchor); err != nil {
				return res, fmt.Errorf("retour du pane %s : %w", p.ID, err)
			}
			_, _ = t("set-option", "-p", "-u", "-t", p.ID, optHome)
			res.Shown = append(res.Shown, p.ID)
		case !p.Hidden && d.Hide[p.ID] && !p.Focus:
			if err := hidePane(t, win, p.ID, home); err != nil {
				return res, err
			}
			res.Hid = append(res.Hid, p.ID)
		}
	}
	return res, nil
}

// hidePane : met le pane de côté dans la réserve (créée à la demande, à la
// taille de la fenêtre du projet).
func hidePane(t SessionTmux, win, pane, home string) error {
	if _, err := t("has-session", "-t", "="+HiddenSession); err != nil {
		size, _ := t("display-message", "-p", "-t", win, "#{window_width} #{window_height}")
		w, h := "120", "40"
		if f := strings.Fields(size); len(f) == 2 {
			w, h = f[0], f[1]
		}
		// la fenêtre d'amorce ne porte pas @cl_home : jamais prise pour un pane caché
		if _, err := t("new-session", "-d", "-s", HiddenSession, "-x", w, "-y", h, "tail -f /dev/null"); err != nil {
			return fmt.Errorf("réserve des panes cachés : %w", err)
		}
	}
	_, _ = t("set-option", "-p", "-t", pane, optHome, home)
	if _, err := t("break-pane", "-d", "-a", "-s", pane, "-t", "="+HiddenSession+":"); err != nil {
		_, _ = t("set-option", "-p", "-u", "-t", pane, optHome)
		return fmt.Errorf("pane %s mis de côté : %w", pane, err)
	}
	return nil
}

// ToggleResult : issue d'une bascule manuelle.
type ToggleResult struct {
	OK  bool
	Msg string // raison du refus, affichable
}

// TogglePane : bascule manuelle (double-clic) du pane sur le projet win/home.
// Refus : teammate actif, leader, cdp, pane sans agent, pane inconnu. L'écriture
// ne concerne que @cl_panevis ; le déplacement est réalisé au prochain
// ApplyPaneVis. L'appelant a vérifié qu'il a la main (lecture seule : refus).
func TogglePane(t SessionTmux, win, home string, tc *TeamConfig, pane string, inCtr bool) ToggleResult {
	panes, choices, err := ProjectPanes(t, win, home, inCtr)
	if err != nil {
		return ToggleResult{Msg: "projet fermé"}
	}
	var target VisPane
	known := false
	for _, p := range visPanes(panes, tc) {
		if p.ID == pane {
			target, known = p, true
		}
	}
	if why := ToggleDenied(target, known); why != "" {
		return ToggleResult{Msg: why}
	}
	choices[pane] = ToggleChoice(target.Hidden)
	if _, err := t("set-option", "-w", "-t", win, optVis, FormatPaneVis(choices)); err != nil {
		return ToggleResult{Msg: "écriture refusée : " + err.Error()}
	}
	return ToggleResult{OK: true}
}

// ── Fermeture et nettoyage ────────────────────────────────────────────────

// KillHiddenPanes : tue les panes de la réserve rattachés à home (processus
// compris), puis la réserve s'il n'en reste aucun d'aucun projet. Rend le nombre
// de panes tués. À appeler à la fermeture d'un projet (kill-session et
// kill-window ne touchent pas la réserve).
func KillHiddenPanes(t SessionTmux, home string) int {
	if home == "" {
		return 0
	}
	n := 0
	for _, p := range hiddenPanes(t, home, false) {
		if pid := strings.TrimSpace(paneGroup(t, p.ID)); pid != "" {
			_, _ = t("run-shell", "-b", "kill -TERM -- -"+pid+" 2>/dev/null; true")
		}
		if _, err := t("kill-pane", "-t", p.ID); err == nil {
			n++
		}
	}
	dropEmptyReserve(t)
	return n
}

func paneGroup(t SessionTmux, pane string) string {
	s, _ := t("display-message", "-p", "-t", pane, "#{pane_pid}")
	return s
}

// dropEmptyReserve : tue la réserve quand aucun pane n'y porte plus @cl_home.
func dropEmptyReserve(t SessionTmux) {
	out, err := t("list-panes", "-s", "-t", "="+HiddenSession, "-F", "#{@cl_home}")
	if err != nil {
		return
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			return
		}
	}
	_, _ = t("kill-session", "-t", "="+HiddenSession)
}

// KillOrphanHidden : tue les panes de la réserve dont la fenêtre d'origine
// n'existe plus (hub tué et relancé, projet fermé par un launcher antérieur).
// Ne touche pas les panes d'un conteneur (home « ctr »). Rend le nombre tué.
func KillOrphanHidden(t SessionTmux) int {
	out, err := t("list-panes", "-s", "-t", "="+HiddenSession, "-F", "#{pane_id}\t#{@cl_home}")
	if err != nil {
		return 0
	}
	wins, _ := t("list-windows", "-a", "-F", "#{window_id}")
	alive := map[string]bool{}
	for _, w := range strings.Fields(wins) {
		alive[w] = true
	}
	n := 0
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		id, home, _ := strings.Cut(l, "\t")
		if !strings.HasPrefix(home, "@") || alive[home] {
			continue
		}
		if pid := strings.TrimSpace(paneGroup(t, id)); pid != "" {
			_, _ = t("run-shell", "-b", "kill -TERM -- -"+pid+" 2>/dev/null; true")
		}
		if _, err := t("kill-pane", "-t", id); err == nil {
			n++
		}
	}
	dropEmptyReserve(t)
	return n
}
