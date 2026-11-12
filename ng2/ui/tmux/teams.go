package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-launcher-ng2/core"
)

// livePanes : panes de tous les tmux portant des projets ; ok=false si l'un
// d'eux ne répond pas (serveur absent ou erreur) — on ne conclut alors à rien.
// TODO #43 : les ids %N ne sont uniques que par serveur ; avec des conteneurs,
// une team devra être confrontée aux panes de SON tmux (pidSpace du leader).
func livePanes() (map[string]bool, bool) {
	live := map[string]bool{}
	for i, t := range targets() {
		b, err := t.Output("list-panes", "-a", "-F", "#{pane_id}")
		if err != nil {
			if i > 0 { // serveur v4 absent : rien à protéger de ce côté
				continue
			}
			return nil, false
		}
		for _, p := range strings.Fields(string(b)) {
			live[p] = true
		}
	}
	return live, len(live) > 0
}

// CleanupDeadTeams : supprime les teams mortes et leurs tâches ; renvoie leurs noms.
func CleanupDeadTeams(resumeAge time.Duration) []string {
	live, ok := livePanes()
	if !ok {
		return nil
	}
	root := core.ClaudeDir()
	dead := core.DeadTeams(root, live, resumeAge)
	for _, t := range dead {
		_ = os.RemoveAll(filepath.Join(root, "teams", t))
		_ = os.RemoveAll(filepath.Join(root, "tasks", t))
	}
	return dead
}
