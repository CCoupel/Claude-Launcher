package desktop

import (
	"os"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/tun"
)

// ── Relance du launcher (#219, contracts/desktop-dispatch.md « launcherInfo » / « relaunch ») ──

// LauncherSnap : champ `launcher` de l'instantané (bon marché : stat du binaire mémorisée, version lue en tâche de fond).
type LauncherSnap struct {
	Running    string `json:"running"`
	Installed  string `json:"installed"`
	BinChanged bool   `json:"binChanged"`
}

// LauncherInfoOut : résultat de `launcherInfo` (contrat : sans processus anciens ni sauvegarde, propres à la coque).
type LauncherInfoOut struct {
	Running    string `json:"running"`
	Installed  string `json:"installed"`
	BinChanged bool   `json:"binChanged"`
	Mode       string `json:"mode"`
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
	return LauncherSnap{Running: core.Version(), Installed: core.LauncherInstalledCached(), BinChanged: core.LauncherBinChanged()}
}

// launcherInfo : état (exécute `<exe> version`, 3 s) ; la coque tmux lit ses processus anciens directement dans core.
func (a *App) launcherInfo() LauncherInfoOut {
	li := core.LauncherStateFor(a.launcherMode())
	return LauncherInfoOut{Running: li.Running, Installed: li.Installed, BinChanged: li.BinChanged, Mode: a.launcherMode()}
}

// exitProcess : sortie du processus (pod : code 0, le conteneur est redémarré par Kubernetes).
var exitProcess = tun.Of(func() { os.Exit(0) })

// relaunchLauncher : sans confirm, refus (la confirmation est faite par l'interface). Fenêtre native et web :
// restartApp (binaire installé relancé, port libéré, ancien processus quitté) ; pod : sortie 0 après arrêt propre.
func (a *App) relaunchLauncher(confirm bool) (any, error) {
	if !confirm {
		return map[string]any{"ok": false, "msg": "confirmation requise"}, nil
	}
	_ = a.confNow() // config.conf à jour avant la relance (la copie en mémoire ne doit pas précéder le fichier, #225)
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
