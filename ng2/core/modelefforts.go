package core

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Table de compatibilité modèle → efforts (#250, P2/P6) : une seule table, utilisée par teamAgentSet, la
// sous-commande team-set, la coque tmux et (via teamHead.modelEfforts) l'interface native et le navigateur.
//
// Source : référence `claude-api` de Claude Code, tableau « Thinking & Effort » (mis en cache le 2026-11-03).
// À METTRE À JOUR à chaque nouvelle génération de modèles : un alias accepte toujours les 5 efforts, un modèle
// inconnu aussi (au pire un effort est proposé sans effet).

// Efforts : niveaux d'effort, du plus bas au plus haut.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// Models : alias de modèle réglables depuis la carte.
var Models = []string{"opus", "sonnet", "haiku", "fable"}

var modelIDRe = regexp.MustCompile(`^(?:claude-)?(opus|sonnet|haiku|fable)-(\d+)(?:-(\d{1,2})(?:$|[^\d]))?`)
var oldModelIDRe = regexp.MustCompile(`^(?:claude-)?(\d+)(?:-(\d+))?-(opus|sonnet|haiku)`)

// ModelEfforts : efforts acceptés par un modèle (valeur du frontmatter, avec ou sans « claude- »). known faux :
// modèle inconnu de la table (tous les efforts, l'effort courant est conservé). Liste vide et known vrai : le modèle
// ne prend aucun effort.
func ModelEfforts(model string) (efforts []string, known bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if slices.Contains(Models, m) {
		return slices.Clone(Efforts), true
	}
	if g := oldModelIDRe.FindStringSubmatch(m); g != nil { // claude-3-5-sonnet… : génération < 4
		if n, _ := strconv.Atoi(g[1]); n < 4 {
			return nil, true
		}
	}
	if g := modelIDRe.FindStringSubmatch(m); g != nil {
		maj, _ := strconv.Atoi(g[2])
		min, _ := strconv.Atoi(g[3]) // absent : 0
		fam := g[1]
		switch {
		case fam != "fable" && (maj < 4 || (maj == 4 && min < 5)):
			return nil, true
		case maj == 4 && min == 5:
			if fam == "opus" {
				return []string{"low", "medium", "high"}, true
			}
			if fam == "sonnet" || fam == "haiku" {
				return nil, true
			}
		case maj == 4 && min == 6:
			if fam == "opus" || fam == "sonnet" {
				return []string{"low", "medium", "high", "max"}, true
			}
		}
	}
	return slices.Clone(Efforts), false
}

// EffortAccepted : le modèle accepte-t-il cet effort ?
func EffortAccepted(model, effort string) bool {
	e, _ := ModelEfforts(model)
	return slices.Contains(e, effort)
}

// ModelEffortsTable : la table exposée aux clients (teamHead.modelEfforts) ; « * » : repli d'un modèle inconnu.
func ModelEffortsTable() map[string][]string {
	t := map[string][]string{}
	for _, m := range []string{"opus", "sonnet", "haiku", "fable",
		"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-5", "claude-haiku-4-5", "claude-sonnet-4-5",
		"opus-4-6", "sonnet-4-6", "opus-4-5", "haiku-4-5", "sonnet-4-5"} {
		t[m], _ = ModelEfforts(m)
		if t[m] == nil {
			t[m] = []string{}
		}
	}
	t["*"] = slices.Clone(Efforts)
	return t
}

// highestEffort : le plus haut effort de la liste (vide si aucun).
func highestEffort(list []string) string {
	best, bi := "", -1
	for _, e := range list {
		if i := slices.Index(Efforts, e); i > bi {
			best, bi = e, i
		}
	}
	return best
}
