package core

import (
	"os"
	"reflect"
	"sort"
	"strings"
)

// ── Effet de chaque option de config.conf (#226) ──────────────────────────
//
// Table UNIQUE (OptionEffect) : quand une option modifiée prend effet, et que faire pour qu'elle prenne effet. Tirée du
// diagnostic dev-frontend-diag-config-a-chaud-20261121.md, corrigée par #225 (config.conf relu à chaud, recopie dans
// settings.json à l'enregistrement). Interface : IfaceDesktop (fenêtre native, web, exe, pod) ou IfaceShell (coque tmux).

const (
	EffectImmediate = "immediate" // appliquée dès l'enregistrement
	EffectProject   = "project"   // à la prochaine ouverture d'un projet
	EffectClaude    = "claude"    // les sessions claude déjà ouvertes doivent être relancées (les nouvelles l'ont)
	EffectLauncher  = "launcher"  // relance du launcher (#219)

	IfaceDesktop = "desktop"
	IfaceShell   = "shell"

	ActionRefresh  = "refresh"  // « Rafraîchir » : refreshProject des projets ouverts concernés (#158)
	ActionRelaunch = "relaunch" // « Relancer le launcher » (#219)
	ActionReopen   = "reopen"   // texte seul : rouvrir le projet
)

// EffectHow : contenu de la bulle d'une option — pourquoi cet effet, quoi faire, comment dans la coque, lien d'action.
type EffectHow struct {
	Why    string `json:"why"`
	Do     string `json:"do"`
	Shell  string `json:"shell"`
	Action string `json:"action"`
}

// effectOf : effet par clé ; une seule valeur sauf exception par interface (effectShell).
var effectOf = map[string]string{
	"GITHUB_DIR": EffectLauncher, "GITHUB_DIRS": EffectLauncher, "CLAUDE_CONFIG_DIR": EffectLauncher,
	"CLAUDE_OPTIONS": EffectProject, "RESUME_MODE": EffectProject, "RESUME_MAX_AGE_H": EffectProject,
	"CLAUDE_DISABLE_MOUSE": EffectClaude, "CLAUDE_EXPERIMENTAL_TEAMS": EffectClaude, "EXTRA_ENVS": EffectClaude, "GITHUB_TOKEN": EffectClaude,
	"TEAM_HIDE_IDLE_ENABLED": EffectImmediate, "TEAM_HIDE_IDLE": EffectImmediate,
	"LAYOUT_MODE": EffectImmediate, "LAYOUT_LEFT_LEADER_PCT": EffectImmediate, "LAYOUT_2ROW_LEADER_PCT": EffectImmediate,
	"LAYOUT_3ROW_LEADER_NUM": EffectImmediate, "LAYOUT_3ROW_MID_NUM": EffectImmediate, "LAYOUT_3ROW_DEN": EffectImmediate,
	"PANE_ACTIVE_BRIGHTEN_STEP": EffectImmediate, "PANE_INACTIVE_DARKEN_STEP": EffectImmediate,
	"COLOR_PALETTE": EffectImmediate, "CTR_STOP_ON_EXIT": EffectImmediate, "USAGE_REFRESH_SEC": EffectImmediate,
}

// effectShell : exceptions de la coque tmux (la palette n'y est lue qu'au lancement d'un projet).
var effectShell = map[string]string{"COLOR_PALETTE": EffectProject}

// howOf : texte de la bulle par effet.
var howOf = map[string]EffectHow{
	EffectImmediate: {Why: "appliquée dès l'enregistrement : config.conf est relu à chaud", Do: "", Shell: "", Action: ""},
	EffectProject: {Why: "lue à l'ouverture d'un projet : les projets déjà ouverts gardent l'ancienne valeur",
		Do: "fermer puis rouvrir le projet concerné", Shell: "fermer la fenêtre du projet puis le rouvrir depuis le navigateur", Action: ActionReopen},
	EffectClaude: {Why: "recopiée dans settings.json dès l'enregistrement ; lue par claude à son démarrage",
		Do: "Rafraîchir chaque projet ouvert ; les nouvelles sessions l'ont déjà", Shell: "F5 sur le projet", Action: ActionRefresh},
	EffectLauncher: {Why: "lue au démarrage du launcher : elle ne peut pas être changée à chaud",
		Do: "Relancer le launcher", Shell: "ctrl+r quand l'annonce de relance est affichée, sinon Échap puis l puis relancer claude-launcher-ng2", Action: ActionRelaunch},
}

// OptionEffect : effet d'une option pour l'interface iface et contenu de sa bulle ; ok faux pour une clé inconnue (pas de
// pastille). Les textes sont en français, produits ici : l'interface ne les invente pas.
func OptionEffect(key, iface string) (effect string, how EffectHow, ok bool) {
	effect, ok = effectOf[key]
	if !ok {
		return "", EffectHow{}, false
	}
	if iface == IfaceShell {
		if e, has := effectShell[key]; has {
			effect = e
		}
	}
	return effect, howOf[effect], true
}

// OptionKeys : clés connues de la table (couverture : toutes celles du modèle de config.conf y figurent).
func OptionKeys() []string {
	out := make([]string, 0, len(effectOf))
	for k := range effectOf {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ChangedOption : une option dont la valeur EFFECTIVE a changé, avec son effet.
type ChangedOption struct {
	Key    string `json:"key"`
	Effect string `json:"effect"`
}

// ChangedOptions : options de config.conf dont la valeur effective diffère entre old et new (texte du fichier). Un
// commentaire, un espace, un ordre ou une valeur explicitement égale au défaut ne comptent pas ; le jeton se compare sur sa
// valeur (jamais exposée ici).
func ChangedOptions(oldSrc, newSrc, iface string) []ChangedOption {
	om, nm := ReadConfModel(oldSrc), ReadConfModel(newSrc)
	oldTok, newTok := ConfTokenValue(oldSrc), ConfTokenValue(newSrc)
	by := map[string]ConfSettingView{}
	for _, s := range om.Settings {
		by[s.Key] = s
	}
	var out []ChangedOption
	for _, n := range nm.Settings {
		o := by[n.Key]
		changed := false
		if n.Type == "secret" {
			changed = oldTok != newTok
		} else {
			changed = !reflect.DeepEqual(effectiveValue(o), effectiveValue(n))
		}
		if !changed {
			continue
		}
		if eff, _, ok := OptionEffect(n.Key, iface); ok {
			out = append(out, ChangedOption{Key: n.Key, Effect: eff})
		}
	}
	return out
}

func effectiveValue(s ConfSettingView) any {
	if v, ok := s.Value.([]string); ok && len(v) == 0 {
		return []string{}
	}
	return s.Value
}

// NeedsRelaunch : une option « launcher » a changé.
func NeedsRelaunch(ch []ChangedOption) bool {
	for _, c := range ch {
		if c.Effect == EffectLauncher {
			return true
		}
	}
	return false
}

// ApplyNote : ligne d'état de la coque après relecture — options modifiées et ce qu'il reste à faire (texte seul).
func ApplyNote(ch []ChangedOption, iface string) string {
	if len(ch) == 0 {
		return ""
	}
	var keys []string
	todo := map[string]bool{}
	for _, c := range ch {
		keys = append(keys, c.Key)
		if c.Effect != EffectImmediate {
			_, how, _ := OptionEffect(c.Key, iface)
			t := how.Do
			if iface == IfaceShell {
				t = how.Shell
			}
			if t != "" {
				todo[t] = true
			}
		}
	}
	msg := "config.conf relu : " + strings.Join(keys, ", ")
	if len(todo) == 0 {
		return msg + " (appliqué)"
	}
	var ts []string
	for t := range todo {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	return msg + " — à faire : " + strings.Join(ts, " ; ")
}

// ReadConfSrc : contenu actuel de config.conf ("" : absent ou illisible).
func ReadConfSrc() string {
	b, _ := os.ReadFile(ConfigPath())
	return string(b)
}
