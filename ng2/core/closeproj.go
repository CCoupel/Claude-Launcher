package core

import (
	"fmt"
	"strings"
	"time"
)

// ── Fermeture de la session d'un projet (#50) ─────────────────────────────
// Commune à la coque tmux (ui/tmux/close.go) et à l'interface native.

// SessionTmux : tmux de la session du projet — le hub (fenêtre du projet) ou
// celui du conteneur ; lance « tmux args… » et rend sa sortie.
type SessionTmux func(args ...string) (string, error)

// StopSession : /exit au leader, attente de sa sortie (closeWait), puis
// fermeture de la session tmux (hub : la fenêtre ; conteneur : la session).
func StopSession(t SessionTmux, target string, inCtr bool, closeWait time.Duration, step func(string)) error {
	leader, cmd := LeaderPane(t, target, inCtr)
	if leader == "" {
		return nil // plus de session
	}
	home := ProjectHome(t, target, inCtr) // avant la fermeture : retrouver les panes cachés (#136)
	start := time.Now()
	if !Shells[cmd] {
		// Échap interrompt une réponse en cours ; /exit est ensuite tapé seul
		_, _ = t("send-keys", "-t", leader, "Escape")
		time.Sleep(400 * time.Millisecond)
		_, _ = t("send-keys", "-t", leader, "-l", "/exit")
		time.Sleep(150 * time.Millisecond)
		_, _ = t("send-keys", "-t", leader, "Enter")
		step("/exit envoyé à claude")
		clean := false
		for time.Since(start) < closeWait {
			time.Sleep(500 * time.Millisecond)
			if c := PaneCommand(t, leader); c == "" || Shells[c] {
				clean = true
				break
			}
		}
		if clean {
			step(fmt.Sprintf("sortie propre (%d s)", int(time.Since(start).Seconds()+0.5)))
		} else {
			step(fmt.Sprintf("pas de réponse en %d s : arrêt forcé", int(closeWait.Seconds())))
		}
	}
	// les panes cachés vivent dans la réserve, hors de la session : fermés aussi
	if n := KillHiddenPanes(t, home); n > 0 {
		step(fmt.Sprintf("%d pane(s) caché(s) fermé(s)", n))
	}
	if inCtr {
		_, _ = t("kill-session", "-t", "="+CtrSession)
	}
	return nil
}

// LeaderPane : premier pane créé de la session (plus petit %id) et sa commande.
func LeaderPane(t SessionTmux, target string, inCtr bool) (id, cmd string) {
	args := []string{"list-panes", "-t", target, "-F", "#{pane_id} #{pane_current_command}"}
	if inCtr {
		args = []string{"list-panes", "-s", "-t", "=" + CtrSession, "-F", "#{pane_id} #{pane_current_command}"}
	}
	s, err := t(args...)
	if err != nil {
		return "", ""
	}
	return LeaderOf(s)
}

func PaneCommand(t SessionTmux, pane string) string {
	s, err := t("display-message", "-p", "-t", pane, "#{pane_current_command}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}
