package desktop

import (
	"os"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/tun"
)

// ── Relance du launcher (#219, contracts/desktop-dispatch.md « launcherInfo » / « relaunch ») ──

// LauncherSnap : champ `launcher` de l'instantané (bon marché : stat du binaire, mémorisée).
type LauncherSnap struct {
	BinChanged bool   `json:"binChanged"`
	Stale      int    `json:"stale"`
	Backup     string `json:"backup,omitempty"`
}

func (a *App) launcherMode() string {
	switch {
	case core.InCluster():
		return "pod"
	case a.web:
		return "web"
	}
	return "desktop"
}

func (a *App) launcherSnap() LauncherSnap {
	s := LauncherSnap{BinChanged: core.LauncherBinChanged()}
	if core.LauncherBackupHook != nil {
		if b := core.LauncherBackupHook(); b != nil {
			s.Backup = b.Version
		}
	}
	return s
}

// launcherInfo : état complet (exécute `<exe> version`, 3 s) ; la coque tmux ne passe pas par ici.
func (a *App) launcherInfo() core.LauncherInfo {
	li := core.LauncherState()
	li.Mode = a.launcherMode()
	if li.Mode != "shell" {
		li.Stale = []core.StaleProc{}
	}
	return li
}

// exitProcess : sortie du processus (pod : code 0, le conteneur est redémarré par Kubernetes).
var exitProcess = tun.Of(func() { os.Exit(0) })

// relaunchLauncher : sans confirm, refus (la confirmation est faite par l'interface). Fenêtre native et web :
// restartApp (binaire installé relancé, port libéré, ancien processus quitté) ; pod : sortie 0 après arrêt propre.
func (a *App) relaunchLauncher(confirm bool) (any, error) {
	if !confirm {
		return map[string]any{"ok": false, "msg": "confirmation requise"}, nil
	}
	if a.launcherMode() == "pod" {
		go func() {
			a.Stop()
			exitProcess.Get()()
		}()
		return map[string]any{"ok": true}, nil
	}
	go func() { // la réponse part avant l'arrêt : l'interface se reconnecte
		time.Sleep(200 * time.Millisecond)
		restartApp.Get()()
	}()
	return map[string]any{"ok": true}, nil
}
