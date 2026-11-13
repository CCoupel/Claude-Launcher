package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-launcher-ng2/platform"
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

func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// TeamsProgress : avancement de deadTeams (barre de progression au démarrage).
var TeamsProgress func(done, total int)

// DeadTeams : noms des teams de root/teams dont tous les panes teammates ont disparu.
func DeadTeams(root string, live map[string]bool, resumeAge time.Duration) []string {
	entries, err := os.ReadDir(filepath.Join(root, "teams"))
	if err != nil {
		return nil
	}
	var dead []string
	for i, e := range entries {
		if TeamsProgress != nil {
			TeamsProgress(i, len(entries))
		}
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, "teams", e.Name(), "config.json"))
		if err != nil {
			continue // config absente ou en cours d'écriture
		}
		var cfg struct {
			LeadSessionID string `json:"leadSessionId"`
			LeadProcess   *struct {
				PID      int    `json:"pid"`
				PIDSpace string `json:"pidSpace"`
			} `json:"leadProcess"`
			Members []struct {
				TmuxPaneID string `json:"tmuxPaneId"`
			} `json:"members"`
		}
		if json.Unmarshal(b, &cfg) != nil {
			continue
		}
		// team d'un conteneur ou d'une autre machine : ses panes %N sont ceux de
		// SON tmux, invisibles d'ici — jamais supprimée par l'hôte
		if p := cfg.LeadProcess; p != nil && p.PIDSpace != "" && p.PIDSpace != platform.HostPidSpace() {
			if h, _, _ := strings.Cut(p.PIDSpace, "#"); h != platform.HostName() {
				continue
			}
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
		// session reprenable : cherchée en dernier (balaye tous les projets,
		// coûteux sur un stockage réseau), seulement pour une team condamnée
		if panes > 0 && !alive && !resumableSession(root, cfg.LeadSessionID, resumeAge) {
			dead = append(dead, e.Name())
		}
	}
	return dead
}
