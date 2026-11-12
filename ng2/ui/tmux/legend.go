package tmux

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"claude-launcher-ng2/core"
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
	git       core.GitInfo
	gitOK     bool // état git déjà lu
	tpl       core.TemplateState
	tplTag    string // template installé (vide si inconnu)
	tplLatest string // dernière version du template
	ms        core.MsState
	sess      core.Resumable
	hasSess   bool
	hasEnv    bool           // env.yaml ou env-template.yaml présent
	backend   string         // backend affecté (host par défaut)
	ctr       core.Found     // conteneur du projet
	ctrKnown  bool           // … s'il existe
	problems  []core.Problem // backend affecté inutilisable (diag.go)
	badCfg    string         // configuration d'exécution invalide : lancement refusé
	dups      []string       // groupe de serveurs : serveurs où le projet a un conteneur (en double)
	live      core.LiveSess  // session tmux vivante dans le conteneur…
	hasLive   bool           // … s'il y en a une
	dot       sessState      // point devant le nom (sessionOf), calculé par le navigateur
	dotWhy    string         // … et sa signification
}

// ── Point d'état de session (#50, révisé #54) ──────────────────────────────

type sessState int

const (
	sessNone     sessState = iota // ○ gris : rien ne tourne
	sessReady                     // ● bleu : conteneur en marche sans claude
	sessHere                      // ▲ vert : ouvert ici, ce launcher a la main
	sessReadOnly                  // ● vert : ouvert ici en lecture seule
	sessElse                      // ■ orange : piloté par un autre hub
	sessOrphan                    // ● orange : vivant, aucun launcher ne pilote
	sessAnomaly                   // ✕ rouge : backend ⚠, config invalide, pod en échec, reprise ↺
)

// foreign : la session du conteneur est pilotée par un autre launcher vivant.
func (p projInfo) foreign() string {
	if p.hasLive && p.live.Owner.Alive() && p.live.Owner.Id != core.LauncherID() {
		return p.live.Owner.Id
	}
	return ""
}

// claudeLive : claude tourne dans la session du conteneur (pas seulement tmux).
func (p projInfo) claudeLive() bool { return p.hasLive && p.live.Claude }

// sessionOf : état du projet (conteneur, session claude, pilotage) et sa signification.
func sessionOf(p projInfo) (sessState, string) {
	running := p.ctrKnown && p.ctr.Running
	switch {
	case p.open && p.hasLive && p.live.Owner.Id != "" && p.live.Owner.Id != core.LauncherID():
		return sessReadOnly, "ouvert ici en lecture seule : " + p.live.Owner.Id + " a la main (Entrée : rejoindre ou prendre la main)"
	case p.open:
		return sessHere, "ouvert ici, ce launcher a la main (Ctrl+clic ou Suppr : fermer)"
	case p.badCfg != "":
		return sessAnomaly, "configuration d'exécution invalide : lancement refusé"
	case len(p.dups) > 1:
		return sessAnomaly, "conteneur en double sur " + strings.Join(p.dups, " et ") + " : Entrée pour choisir lequel garder"
	case len(p.problems) > 0:
		return sessAnomaly, "backend " + p.backend + " inutilisable : rien ne peut démarrer"
	case p.claudeLive() && p.foreign() != "":
		return sessElse, "piloté par un autre hub (" + p.foreign() + ") : Entrée pour rejoindre ou prendre la main"
	case p.claudeLive():
		return sessOrphan, "claude tourne, aucun launcher ne le pilote : Entrée pour s'y rattacher"
	case p.hasSess:
		return sessAnomaly, "session interrompue (↺) : Entrée pour la reprendre, Ctrl+clic pour l'abandonner"
	case p.ctrKnown && p.ctr.Failed:
		return sessAnomaly, "conteneur " + p.backend + " en échec (pod pas prêt) : Ctrl+clic pour l'arrêter"
	case running:
		return sessReady, "conteneur en marche sans claude : Entrée le démarre aussitôt, Ctrl+clic arrête le conteneur"
	case p.ctrKnown:
		return sessNone, "rien ne tourne (conteneur arrêté, redémarré à l'ouverture)"
	}
	return sessNone, "rien ne tourne"
}

// dotIndicator : pastille devant le nom.
func dotIndicator(p projInfo) indicator {
	st, why := p.dot, p.dotWhy
	if why == "" { // projInfo sans état calculé (vue « ◈ État » d'une ancienne sélection)
		st, why = sessionOf(p)
	}
	switch st {
	case sessReady:
		return indicator{icon: "●", style: core.Blue, text: why}
	case sessHere:
		return indicator{icon: "▲", style: core.Green.Bold(true), text: why}
	case sessReadOnly:
		return indicator{icon: "●", style: core.Green.Bold(true), text: why}
	case sessElse:
		return indicator{icon: "■", style: core.Orange.Bold(true), text: why}
	case sessOrphan:
		return indicator{icon: "●", style: core.Orange.Bold(true), text: why}
	case sessAnomaly:
		return indicator{icon: "✕", style: core.Red, text: why}
	}
	return indicator{icon: "○", style: core.Dim, text: why}
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
		ago := " (il y a " + core.AgeLabel(time.Since(p.sess.Mod)) + ")"
		if p.sess.Crashed {
			out = append(out, indicator{icon: "↺", style: core.Red, text: "session interrompue par un crash" + ago + " : reprise proposée à l'ouverture"})
		} else {
			out = append(out, indicator{icon: "↺", style: core.Yellow, text: "session non close" + ago + " : reprise proposée à l'ouverture"})
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
	case core.TplOK:
		return indicator{icon: "✔", style: core.Green.Bold(true), text: "template" + have + " à jour"}, true
	case core.TplNone:
		return indicator{icon: "⊘", style: core.Dim, text: "non géré par template"}, true
	case core.TplOutdated:
		next := "mise à jour"
		if p.tplLatest != "" {
			next = p.tplLatest
		}
		return indicator{icon: "⚠", style: core.Yellow, text: "template" + have + " en retard, " + next + " disponible (/init-project au lancement)"}, true
	case core.TplMissing:
		return indicator{icon: "⚠", style: core.Red, text: "fichiers du template manquants (/init-project au lancement)", short: "fichiers manquants"}, true
	}
	return indicator{}, false
}

func msIndicator(st core.MsState) (indicator, bool) {
	switch st.Kind {
	case core.MsWorking:
		return indicator{icon: "⚑", style: core.Yellow, text: fmt.Sprintf("milestone %s en cours (%d%%)", st.Title, int(core.Ratio(st.MsRow)*100+0.5))}, true
	case core.MsReleasing:
		return indicator{icon: "⚑", style: core.Orange, text: "milestone " + st.Title + " terminé, à publier"}, true
	case core.MsIdle:
		return indicator{icon: "⚑", style: core.Dim, text: fmt.Sprintf("aucun milestone en cours (%d ouvert(s))", len(st.Pending))}, true
	case core.MsDone:
		return indicator{icon: "⚑", style: core.Green, text: "milestone " + st.Title + " publié"}, true
	}
	return indicator{}, false
}

// prIndicator : « ⇄N », N pull requests ouvertes en attente (brouillons compris).
func prIndicator(st core.MsState) (indicator, bool) {
	if st.PRs == 0 {
		return indicator{}, false
	}
	text := fmt.Sprintf("%d pull request(s) ouverte(s) en attente", st.PRs)
	if st.Drafts > 0 {
		text += fmt.Sprintf(" (dont %d brouillon(s))", st.Drafts)
	}
	return indicator{icon: fmt.Sprintf("⇄%d", st.PRs), style: core.MsPurple, text: text}, true
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
		out = append(out, i.style.Render(core.PadRight(i.icon, 3))+" "+text)
	}
	return out
}
