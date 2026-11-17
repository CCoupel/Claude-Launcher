package core

import "cmp"

// ── En-tête de la carte TEAM (#151) ───────────────────────────────────────
// Calcul unique pour la coque Linux et l'interface native (contrat :
// contracts/desktop-dispatch.md, cards.teamHead et team[].odd).

// TeamHead : ce que montre le titre de la carte TEAM.
type TeamHead struct {
	Tpl    string `json:"tpl"`    // version cible du template du projet ; "" si inconnue
	Claude string `json:"claude"` // version de claude du leader ; "" si le projet est fermé
	Odd    int    `json:"odd"`    // nombre de teammates non conformes
	// HideIdle : état du masquage des teammates inactifs (#188) : commutateur effectif et délai conservé ; renseigné par
	// l'appelant (la configuration n'est pas connue d'ApplyTeamHead)
	HideIdle HideIdleInfo `json:"hideIdle"`
}

// Odd* : sur quoi une ligne s'écarte de l'en-tête.
const (
	OddTpl    = "tpl"    // tag du teammate ≠ version cible du template
	OddClaude = "claude" // version de claude démarrée ≠ celle du leader
)

// ApplyTeamHead : en-tête de la carte et écarts de chaque ligne (TeamLine.Odd).
// tplTarget : version cible du template (TplInfo.Target) ; à défaut, celle qu'une
// ligne porte déjà (TplTarget). Une version inconnue d'un côté ne crée aucun écart.
func ApplyTeamHead(lines []TeamLine, tplTarget string) ([]TeamLine, TeamHead) {
	h := TeamHead{Tpl: tplTarget}
	for _, l := range lines {
		h.Tpl = cmp.Or(h.Tpl, l.TplTarget)
		if l.Name == "leader" && l.Started && h.Claude == "" {
			h.Claude = l.Claude
		}
	}
	for i := range lines {
		l := &lines[i]
		l.Odd = nil
		if h.Tpl != "" && l.Tag != "" && l.Tag != h.Tpl {
			l.Odd = append(l.Odd, OddTpl)
		}
		if h.Claude != "" && l.Started && l.Claude != "" && l.Claude != h.Claude {
			l.Odd = append(l.Odd, OddClaude)
		}
		if len(l.Odd) > 0 {
			h.Odd++
		}
	}
	return lines, h
}
