package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Nettoyage des teams mortes. Une session Claude coupée (crash, terminal fermé)
// laisse sa team dans ~/.claude/teams ; la reprise en crée une autre pour le
// même projet. Critère volontairement strict, pour ne jamais toucher une team
// vivante :
//   - la team a au moins un teammate avec un pane tmux (%N) ;
//   - aucun de ces panes n'existe sur le serveur tmux intérieur.
// Les teams sans teammate (le leader n'a pas de pane %N) ne prouvent rien :
// elles sont laissées en place, de même que celles dont la session du leader
// peut encore être reprise (resume.go) : la reprise retrouve team et tâches. L'ancien cleanup_orphan_teams du bash, fondé sur
// le nom de la fenêtre, avait déjà supprimé des teams vivantes.

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// livePanes : panes du serveur intérieur ; ok=false si tmux ne répond pas
// (serveur absent ou erreur) — on ne conclut alors à rien.
func livePanes() (map[string]bool, bool) {
	b, err := dtmux("list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		return nil, false
	}
	live := map[string]bool{}
	for _, p := range strings.Fields(string(b)) {
		live[p] = true
	}
	return live, len(live) > 0
}

// deadTeams : noms des teams de root/teams dont tous les panes teammates ont disparu.
func deadTeams(root string, live map[string]bool, resumeAge time.Duration) []string {
	entries, err := os.ReadDir(filepath.Join(root, "teams"))
	if err != nil {
		return nil
	}
	var dead []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, "teams", e.Name(), "config.json"))
		if err != nil {
			continue // config absente ou en cours d'écriture
		}
		var cfg struct {
			LeadSessionID string `json:"leadSessionId"`
			Members       []struct {
				TmuxPaneID string `json:"tmuxPaneId"`
			} `json:"members"`
		}
		if json.Unmarshal(b, &cfg) != nil || resumableSession(root, cfg.LeadSessionID, resumeAge) {
			continue
		}
		panes, alive := 0, false
		for _, m := range cfg.Members {
			if !strings.HasPrefix(m.TmuxPaneID, "%") {
				continue
			}
			panes++
			if live[m.TmuxPaneID] {
				alive = true
			}
		}
		if panes > 0 && !alive {
			dead = append(dead, e.Name())
		}
	}
	return dead
}

// cleanupDeadTeams : supprime les teams mortes et leurs tâches ; renvoie leurs noms.
func cleanupDeadTeams(resumeAge time.Duration) []string {
	live, ok := livePanes()
	if !ok {
		return nil
	}
	root := claudeDir()
	dead := deadTeams(root, live, resumeAge)
	for _, t := range dead {
		_ = os.RemoveAll(filepath.Join(root, "teams", t))
		_ = os.RemoveAll(filepath.Join(root, "tasks", t))
	}
	return dead
}
