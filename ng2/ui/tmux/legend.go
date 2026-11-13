package tmux

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"claude-launcher-ng2/core"
)

// Indicateurs d'un projet : chaque icône de sa ligne dans le navigateur, avec sa
// signification. Source unique : core/legend.go, partagée avec l'interface
// native (ui/desktop). Ici, leur rendu lipgloss : la ligne du navigateur
// (badges) et la légende de la fenêtre projet (vue « ◈ État », aperçu) en sont
// tirées, avec la même icône, la même couleur et dans le même ordre.

type indicator struct {
	icon  string
	style lipgloss.Style
	text  string // signification, pour la légende
	short string // libellé gardé sur la ligne du navigateur (rare)
}

// fromCore : rendu lipgloss d'un indicateur de core (couleur sémantique → style).
func fromCore(i core.Indicator) indicator {
	return indicator{icon: i.Icon, style: i.Tone.Style(), text: i.Text, short: i.Short}
}

func fromCoreAll(ins []core.Indicator) []indicator {
	var out []indicator
	for _, i := range ins {
		out = append(out, fromCore(i))
	}
	return out
}

// projInfo : ce qu'on sait d'un projet pour ses indicateurs (core.ProjInfo).
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

func (p projInfo) core() core.ProjInfo {
	return core.ProjInfo{Open: p.open, Git: p.git, GitOK: p.gitOK, Tpl: p.tpl, TplTag: p.tplTag,
		TplLatest: p.tplLatest, Ms: p.ms, Sess: p.sess, HasSess: p.hasSess, HasEnv: p.hasEnv,
		Backend: p.backend, Ctr: p.ctr, CtrKnown: p.ctrKnown, Problems: p.problems, BadCfg: p.badCfg,
		Dups: p.dups, Live: p.live, HasLive: p.hasLive, Dot: p.dot, DotWhy: p.dotWhy}
}

// ── Point d'état de session (#50, révisé #54) ──────────────────────────────

type sessState = core.SessState

const (
	sessNone     = core.SessNone     // ○ gris : rien ne tourne
	sessReady    = core.SessReady    // ● bleu : conteneur en marche sans claude
	sessHere     = core.SessHere     // ▲ vert : ouvert ici, ce launcher a la main
	sessReadOnly = core.SessReadOnly // ● vert : ouvert ici en lecture seule
	sessElse     = core.SessElse     // ■ orange : piloté par un autre hub
	sessOrphan   = core.SessOrphan   // ● orange : vivant, aucun launcher ne pilote
	sessAnomaly  = core.SessAnomaly  // ✕ rouge : backend ⚠, config invalide, pod en échec, reprise ↺
)

// foreign : la session du conteneur est pilotée par un autre launcher vivant.
func (p projInfo) foreign() string { return p.core().Foreign() }

// claudeLive : claude tourne dans la session du conteneur (pas seulement tmux).
func (p projInfo) claudeLive() bool { return p.core().ClaudeLive() }

// sessionOf : état du projet (conteneur, session claude, pilotage) et sa signification.
func sessionOf(p projInfo) (sessState, string) { return core.SessionOf(p.core()) }

// dotIndicator : pastille devant le nom.
func dotIndicator(p projInfo) indicator { return fromCore(core.DotIndicator(p.core())) }

// badgeIndicators : icônes après le nom, dans l'ordre de la ligne du navigateur.
func badgeIndicators(p projInfo) []indicator { return fromCoreAll(core.BadgeIndicators(p.core())) }

func tplIndicator(p projInfo) (indicator, bool) {
	i, ok := core.TplIndicator(p.core())
	return fromCore(i), ok
}

func msIndicator(st core.MsState) (indicator, bool) {
	i, ok := core.MsIndicator(st)
	return fromCore(i), ok
}

// prIndicator : « ⇄N », N pull requests ouvertes en attente (brouillons compris).
func prIndicator(st core.MsState) (indicator, bool) {
	i, ok := core.PrIndicator(st)
	return fromCore(i), ok
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
