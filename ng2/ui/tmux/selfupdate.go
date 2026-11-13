package tmux

import (
	"claude-launcher-ng2/platform"
)

// restartOnNewBinary : après une mise à jour depuis le navigateur, relance le
// pane d'infos puis le navigateur lui-même sur le binaire installé (respawn-pane
// tue ce processus). Les projets et la zone tmux ne sont pas touchés.
func restartOnNewBinary() {
	exe, err := platform.SelfExe() // chemin du fichier, même remplacé ou après un remontage
	if err != nil {
		return
	}
	if p := outerPane(roleInfo); p != "" {
		_ = otmux("respawn-pane", "-k", "-t", p, stateCommand(exe)).Run()
	}
	_ = otmux("respawn-pane", "-k", "-t", outerPane(roleNav), navCommand(exe)).Run()
}
