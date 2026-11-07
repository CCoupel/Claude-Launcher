package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Indicateurs d'un projet : chaque icône de sa ligne dans le navigateur, avec sa
// signification. Source unique : la ligne du navigateur (badges) et la légende
// de la fenêtre projet (vue « ◈ État », aperçu) en sont tirées, avec la même
// icône, la même couleur et dans le même ordre.

type indicator struct {
	icon  string
	style lipgloss.Style
	text  string // signification, pour la légende
	short string // libellé gardé sur la ligne du navigateur (rare)
}

// projInfo : ce qu'on sait d'un projet pour ses indicateurs.
type projInfo struct {
	open      bool
	git       GitInfo
	gitOK     bool // état git déjà lu
	tpl       TemplateState
	tplTag    string // template installé (vide si inconnu)
	tplLatest string // dernière version du template
	ms        MsState
	sess      Resumable
	hasSess   bool
	hasEnv    bool      // .claude-launcher/env.yaml présent
	backend   string    // backend affecté (host par défaut)
	ctr       Found     // conteneur du projet
	ctrKnown  bool      // … s'il existe
	problems  []problem // backend affecté inutilisable (diag.go)
	badCfg    string    // configuration d'exécution invalide : lancement refusé
	live      liveSess  // session tmux vivante dans le conteneur…
	hasLive   bool      // … s'il y en a une
	dot       sessState // point devant le nom (sessionOf), calculé par le navigateur
	dotWhy    string    // … et sa signification
}

// ── Point d'état de session (#50) ─────────────────────────────────────────

type sessState int

const (
	sessNone     sessState = iota // ○ aucune session
	sessAttached                  // ● session dans une fenêtre de ce hub
	sessDetached                  // ◐ session vivante, non rattachée ici
	sessAnomaly                   // ✕ backend ⚠, pod en échec, session interrompue ↺
)

// foreign : la session du conteneur est pilotée par un autre launcher vivant.
func (p projInfo) foreign() string {
	if p.hasLive && p.live.owner.alive() && p.live.owner.id != launcherID() {
		return p.live.owner.id
	}
	return ""
}

// sessionOf : état de la session claude du projet, et sa signification.
func sessionOf(p projInfo) (sessState, string) {
	idle := p.ctrKnown && p.ctr.Running && !p.hasLive
	switch {
	case p.open:
		return sessAttached, "session ouverte dans ce hub (Ctrl+clic ou Suppr : la fermer)"
	case p.badCfg != "":
		return sessAnomaly, "configuration d'exécution invalide : lancement refusé"
	case len(p.problems) > 0:
		return sessAnomaly, "backend " + p.backend + " inutilisable : rien ne peut démarrer"
	case p.foreign() != "":
		return sessDetached, "session vivante, pilotée par " + p.foreign() + " : Entrée pour la rejoindre ou prendre la main"
	case p.hasLive:
		return sessDetached, "session vivante dans le conteneur, non rattachée : Entrée pour s'y rattacher"
	case p.hasSess:
		return sessAnomaly, "session interrompue (↺) : Entrée pour la reprendre, Ctrl+clic pour l'abandonner"
	case p.ctrKnown && p.ctr.Failed:
		return sessAnomaly, "conteneur " + p.backend + " en échec (pod pas prêt) : Ctrl+clic pour l'arrêter"
	case idle:
		return sessNone, "non démarré ; conteneur en marche sans session (Ctrl+clic : l'arrêter)"
	}
	return sessNone, "non démarré"
}

// dotIndicator : pastille devant le nom.
func dotIndicator(p projInfo) indicator {
	st, why := p.dot, p.dotWhy
	if why == "" { // projInfo sans état calculé (vue « ◈ État » d'une ancienne sélection)
		st, why = sessionOf(p)
	}
	switch st {
	case sessAttached:
		return indicator{icon: "●", style: green, text: why}
	case sessDetached:
		return indicator{icon: "◐", style: orange.Bold(true), text: why}
	case sessAnomaly:
		return indicator{icon: "✕", style: red, text: why}
	}
	return indicator{icon: "○", style: dim, text: why}
}

// badgeIndicators : icônes après le nom, dans l'ordre de la ligne du navigateur.
func badgeIndicators(p projInfo) []indicator {
	var out []indicator
	if p.gitOK {
		if p.git.IsRepo {
			ls, lt, lst := p.git.Local()
			rs, rt, rst := p.git.Remote()
			out = append(out, indicator{icon: ls, style: lst, text: "local : " + lt},
				indicator{icon: rs, style: rst, text: "remote : " + rt})
		}
		if t, ok := tplIndicator(p); ok {
			out = append(out, t)
		}
	}
	if i, ok := ctrIndicator(p); ok {
		out = append(out, i)
	}
	if i, ok := msIndicator(p.ms); ok {
		out = append(out, i)
	}
	if i, ok := prIndicator(p.ms); ok {
		out = append(out, i)
	}
	if p.hasSess && !p.open {
		ago := " (il y a " + ageLabel(time.Since(p.sess.Mod)) + ")"
		if p.sess.Crashed {
			out = append(out, indicator{icon: "↺", style: red, text: "session interrompue par un crash" + ago + " : reprise proposée à l'ouverture"})
		} else {
			out = append(out, indicator{icon: "↺", style: yellow, text: "session non close" + ago + " : reprise proposée à l'ouverture"})
		}
	}
	return out
}

func tplIndicator(p projInfo) (indicator, bool) {
	have := ""
	if p.tplTag != "" {
		have = " " + p.tplTag
	}
	switch p.tpl {
	case TplOK:
		return indicator{icon: "✔", style: green.Bold(true), text: "template" + have + " à jour"}, true
	case TplNone:
		return indicator{icon: "⊘", style: dim, text: "non géré par template"}, true
	case TplOutdated:
		next := "mise à jour"
		if p.tplLatest != "" {
			next = p.tplLatest
		}
		return indicator{icon: "⚠", style: yellow, text: "template" + have + " en retard, " + next + " disponible (/init-project au lancement)"}, true
	case TplMissing:
		return indicator{icon: "⚠", style: red, text: "fichiers du template manquants (/init-project au lancement)", short: "fichiers manquants"}, true
	}
	return indicator{}, false
}

func msIndicator(st MsState) (indicator, bool) {
	switch st.Kind {
	case MsWorking:
		return indicator{icon: "⚑", style: yellow, text: fmt.Sprintf("milestone %s en cours (%d%%)", st.Title, int(ratio(st.MsRow)*100+0.5))}, true
	case MsReleasing:
		return indicator{icon: "⚑", style: orange, text: "milestone " + st.Title + " terminé, à publier"}, true
	case MsIdle:
		return indicator{icon: "⚑", style: dim, text: fmt.Sprintf("aucun milestone en cours (%d ouvert(s))", len(st.Pending))}, true
	case MsDone:
		return indicator{icon: "⚑", style: green, text: "milestone " + st.Title + " publié"}, true
	}
	return indicator{}, false
}

// prIndicator : « ⇄N », N pull requests ouvertes en attente (brouillons compris).
func prIndicator(st MsState) (indicator, bool) {
	if st.PRs == 0 {
		return indicator{}, false
	}
	text := fmt.Sprintf("%d pull request(s) ouverte(s) en attente", st.PRs)
	if st.Drafts > 0 {
		text += fmt.Sprintf(" (dont %d brouillon(s))", st.Drafts)
	}
	return indicator{icon: fmt.Sprintf("⇄%d", st.PRs), style: msPurple, text: text}, true
}

// badgeLine : rendu des badges sur la ligne du navigateur.
func badgeLine(ins []indicator) string {
	var b strings.Builder
	for _, i := range ins {
		s := i.icon
		if i.short != "" {
			s += " " + i.short
		}
		b.WriteString(" " + i.style.Render(s))
	}
	return b.String()
}

// legendLines : « icône  signification », une ligne par indicateur ; si w > 0,
// le texte est replié à w colonnes, la suite alignée sous le texte.
func legendLines(p projInfo, w int) []string {
	ins := append([]indicator{dotIndicator(p)}, badgeIndicators(p)...)
	out := make([]string, 0, len(ins))
	for _, i := range ins {
		text := i.text
		if w > 8 {
			text = strings.ReplaceAll(lipgloss.NewStyle().Width(w-4).Render(text), "\n", "\n    ")
		}
		out = append(out, i.style.Render(padRight(i.icon, 3))+" "+text)
	}
	return out
}
