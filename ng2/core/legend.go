package core

import (
	"fmt"
	"strings"
	"time"
)

// Indicateurs d'un projet : chaque icône de sa ligne dans le navigateur, avec sa
// signification. Source unique, partagée par les interfaces : la coque tmux
// (ui/tmux/legend.go) les rend avec lipgloss, l'interface native (ui/desktop)
// en HTML. Une icône y est une valeur : texte, couleur sémantique (Tone) et
// signification, sans rendu.

// Tone : couleur sémantique d'une icône. Color vaut dim, green, yellow, red,
// orange, blue, cyan ou purple ; Bold : en gras. Les styles de la coque
// (colors.go) donnent leur rendu exact : Red, Blue et Cyan y sont déjà gras.
type Tone struct {
	Color string `json:"color"`
	Bold  bool   `json:"bold,omitempty"`
}

var (
	ToneDim    = Tone{Color: "dim"}
	ToneGreen  = Tone{Color: "green"}
	ToneYellow = Tone{Color: "yellow"}
	ToneRed    = Tone{Color: "red"}
	ToneOrange = Tone{Color: "orange"}
	ToneBlue   = Tone{Color: "blue"}
	ToneCyan   = Tone{Color: "cyan"}
	TonePurple = Tone{Color: "purple"}
)

// B : la même couleur, en gras.
func (t Tone) B() Tone { t.Bold = true; return t }

// Indicator : une icône de la ligne d'un projet.
type Indicator struct {
	Icon  string `json:"icon"`
	Tone  Tone   `json:"tone"`
	Text  string `json:"text"`            // signification, pour la légende
	Short string `json:"short,omitempty"` // libellé gardé sur la ligne du navigateur (rare)
}

// ProjInfo : ce qu'on sait d'un projet pour ses indicateurs.
type ProjInfo struct {
	Open      bool
	Git       GitInfo
	GitOK     bool // état git déjà lu
	Tpl       TemplateState
	TplTag    string // template installé (vide si inconnu)
	TplLatest string // dernière version du template
	Ms        MsState
	Sess      Resumable
	HasSess   bool
	HasEnv    bool      // env.yaml ou env-template.yaml présent
	Backend   string    // backend affecté (host par défaut)
	Ctr       Found     // conteneur du projet
	CtrKnown  bool      // … s'il existe
	Problems  []Problem // backend affecté inutilisable (diag.go)
	BadCfg    string    // configuration d'exécution invalide : lancement refusé
	Dups      []string  // groupe de serveurs : serveurs où le projet a un conteneur (en double)
	Live      LiveSess  // session tmux vivante dans le conteneur…
	HasLive   bool      // … s'il y en a une
	Dot       SessState // point devant le nom (SessionOf), calculé par le navigateur
	DotWhy    string    // … et sa signification
}

// ── Point d'état de session (#50, révisé #54) ──────────────────────────────

type SessState int

const (
	SessNone     SessState = iota // ○ gris : rien ne tourne
	SessReady                     // ● bleu : conteneur en marche sans claude
	SessHere                      // ▲ vert : ouvert ici, ce launcher a la main
	SessReadOnly                  // ● vert : ouvert ici en lecture seule
	SessElse                      // ■ orange : piloté par un autre hub
	SessOrphan                    // ● orange : vivant, aucun launcher ne pilote
	SessAnomaly                   // ✕ rouge : backend ⚠, config invalide, pod en échec, reprise ↺
)

// Foreign : la session du conteneur est pilotée par un autre launcher vivant.
func (p ProjInfo) Foreign() string {
	if p.HasLive && p.Live.Owner.Alive() && p.Live.Owner.Id != LauncherID() {
		return p.Live.Owner.Id
	}
	return ""
}

// ClaudeLive : claude tourne dans la session du conteneur (pas seulement tmux).
func (p ProjInfo) ClaudeLive() bool { return p.HasLive && p.Live.Claude }

// SessionOf : état du projet (conteneur, session claude, pilotage) et sa signification.
func SessionOf(p ProjInfo) (SessState, string) {
	running := p.CtrKnown && p.Ctr.Running
	switch {
	case p.Open && p.HasLive && p.Live.Owner.Id != "" && p.Live.Owner.Id != LauncherID():
		return SessReadOnly, "ouvert ici en lecture seule : " + p.Live.Owner.Id + " a la main (Entrée : rejoindre ou prendre la main)"
	case p.Open:
		return SessHere, "ouvert ici, ce launcher a la main (Ctrl+clic ou Suppr : fermer)"
	case p.BadCfg != "":
		return SessAnomaly, "configuration d'exécution invalide : lancement refusé"
	case len(p.Dups) > 1:
		return SessAnomaly, "conteneur en double sur " + strings.Join(p.Dups, " et ") + " : Entrée pour choisir lequel garder"
	case len(p.Problems) > 0:
		return SessAnomaly, "backend " + p.Backend + " inutilisable : rien ne peut démarrer"
	case p.ClaudeLive() && p.Foreign() != "":
		return SessElse, "piloté par un autre hub (" + p.Foreign() + ") : Entrée pour rejoindre ou prendre la main"
	case p.ClaudeLive():
		return SessOrphan, "claude tourne, aucun launcher ne le pilote : Entrée pour s'y rattacher"
	case p.HasSess:
		return SessAnomaly, "session interrompue (↺) : Entrée pour la reprendre, Ctrl+clic pour l'abandonner"
	case p.CtrKnown && p.Ctr.Failed:
		return SessAnomaly, "conteneur " + p.Backend + " en échec (pod pas prêt) : Ctrl+clic pour l'arrêter"
	case running:
		return SessReady, "conteneur en marche sans claude : Entrée le démarre aussitôt, Ctrl+clic arrête le conteneur"
	case p.CtrKnown:
		return SessNone, "rien ne tourne (conteneur arrêté, redémarré à l'ouverture)"
	}
	return SessNone, "rien ne tourne"
}

// DotIndicator : pastille devant le nom.
func DotIndicator(p ProjInfo) Indicator {
	st, why := p.Dot, p.DotWhy
	if why == "" { // ProjInfo sans état calculé (vue « ◈ État » d'une ancienne sélection)
		st, why = SessionOf(p)
	}
	switch st {
	case SessReady:
		return Indicator{Icon: "●", Tone: ToneBlue, Text: why}
	case SessHere:
		return Indicator{Icon: "▲", Tone: ToneGreen.B(), Text: why}
	case SessReadOnly:
		return Indicator{Icon: "●", Tone: ToneGreen.B(), Text: why}
	case SessElse:
		return Indicator{Icon: "■", Tone: ToneOrange.B(), Text: why}
	case SessOrphan:
		return Indicator{Icon: "●", Tone: ToneOrange.B(), Text: why}
	case SessAnomaly:
		return Indicator{Icon: "✕", Tone: ToneRed, Text: why}
	}
	return Indicator{Icon: "○", Tone: ToneDim, Text: why}
}

// BadgeIndicators : icônes après le nom, dans l'ordre de la ligne du navigateur.
func BadgeIndicators(p ProjInfo) []Indicator {
	var out []Indicator
	if p.GitOK {
		if p.Git.IsRepo {
			ls, lt, lst := p.Git.LocalTone()
			rs, rt, rst := p.Git.RemoteTone()
			out = append(out, Indicator{Icon: ls, Tone: lst, Text: "local : " + lt},
				Indicator{Icon: rs, Tone: rst, Text: "remote : " + rt})
		}
		if t, ok := TplIndicator(p); ok {
			out = append(out, t)
		}
	}
	if i, ok := CtrIndicator(p); ok {
		out = append(out, i)
	}
	if i, ok := MsIndicator(p.Ms); ok {
		out = append(out, i)
	}
	if i, ok := PrIndicator(p.Ms); ok {
		out = append(out, i)
	}
	if p.HasSess && !p.Open {
		ago := " (il y a " + AgeLabel(time.Since(p.Sess.Mod)) + ")"
		if p.Sess.Crashed {
			out = append(out, Indicator{Icon: "↺", Tone: ToneRed, Text: "session interrompue par un crash" + ago + " : reprise proposée à l'ouverture"})
		} else {
			out = append(out, Indicator{Icon: "↺", Tone: ToneYellow, Text: "session non close" + ago + " : reprise proposée à l'ouverture"})
		}
	}
	return out
}

// Legend : pastille puis badges, dans l'ordre de la ligne du navigateur.
func Legend(p ProjInfo) []Indicator {
	return append([]Indicator{DotIndicator(p)}, BadgeIndicators(p)...)
}

func TplIndicator(p ProjInfo) (Indicator, bool) {
	have := ""
	if p.TplTag != "" {
		have = " " + p.TplTag
	}
	switch p.Tpl {
	case TplOK:
		return Indicator{Icon: "✔", Tone: ToneGreen.B(), Text: "template" + have + " à jour"}, true
	case TplNone:
		return Indicator{Icon: "⊘", Tone: ToneDim, Text: "non géré par template"}, true
	case TplOutdated:
		next := "mise à jour"
		if p.TplLatest != "" {
			next = p.TplLatest
		}
		return Indicator{Icon: "⚠", Tone: ToneYellow, Text: "template" + have + " en retard, " + next + " disponible (/init-project au lancement)"}, true
	case TplMissing:
		return Indicator{Icon: "⚠", Tone: ToneRed, Text: "fichiers du template manquants (/init-project au lancement)", Short: "fichiers manquants"}, true
	}
	return Indicator{}, false
}

func MsIndicator(st MsState) (Indicator, bool) {
	switch st.Kind {
	case MsWorking:
		return Indicator{Icon: "⚑", Tone: ToneYellow, Text: fmt.Sprintf("milestone %s en cours (%d%%)", st.Title, int(Ratio(st.MsRow)*100+0.5))}, true
	case MsReleasing:
		return Indicator{Icon: "⚑", Tone: ToneOrange, Text: "milestone " + st.Title + " terminé, à publier"}, true
	case MsIdle:
		return Indicator{Icon: "⚑", Tone: ToneDim, Text: fmt.Sprintf("aucun milestone en cours (%d ouvert(s))", len(st.Pending))}, true
	case MsDone:
		return Indicator{Icon: "⚑", Tone: ToneGreen, Text: "milestone " + st.Title + " publié"}, true
	}
	return Indicator{}, false
}

// PrIndicator : « ⇄N », N pull requests ouvertes en attente (brouillons compris).
func PrIndicator(st MsState) (Indicator, bool) {
	if st.PRs == 0 {
		return Indicator{}, false
	}
	text := fmt.Sprintf("%d pull request(s) ouverte(s) en attente", st.PRs)
	if st.Drafts > 0 {
		text += fmt.Sprintf(" (dont %d brouillon(s))", st.Drafts)
	}
	return Indicator{Icon: fmt.Sprintf("⇄%d", st.PRs), Tone: TonePurple, Text: text}, true
}

// CtrIndicator : backend du projet sur la ligne du navigateur (projets en conteneur seulement).
func CtrIndicator(p ProjInfo) (Indicator, bool) {
	if p.BadCfg != "" {
		return Indicator{Icon: "⚠", Tone: ToneRed, Text: p.BadCfg}, true
	}
	if p.Backend == "" || p.Backend == "host" {
		if p.HasEnv {
			return Indicator{Icon: "⬡", Tone: ToneDim, Text: "environnement conteneur présent (env.yaml / env-template.yaml) : exécution locale (F2 : choisir un conteneur)"}, true
		}
		return Indicator{}, false
	}
	switch {
	case len(p.Problems) > 0:
		return Indicator{Icon: "⚠", Tone: ToneYellow, Text: "backend " + p.Backend + " inutilisable : " + p.Problems[0].String() +
			" (détails : claude-launcher-ng2 backends)"}, true
	case !p.CtrKnown:
		return Indicator{Icon: "⬡", Tone: ToneDim, Text: "conteneur " + p.Backend + " pas encore créé (bootstrap à l'ouverture)"}, true
	case p.Ctr.Running:
		return Indicator{Icon: "⬢", Tone: ToneCyan, Text: "conteneur " + p.Backend + " en marche (" + p.Ctr.Name + ")"}, true
	default:
		return Indicator{Icon: "⬢", Tone: ToneDim, Text: "conteneur " + p.Backend + " arrêté (redémarré à l'ouverture)"}, true
	}
}
