package tmux

import (
	"claude-launcher-ng2/core"
)

// exportBrowserPathsHub : projets sur l'hôte — environnement global du tmux du
// hub, hérité par les fenêtres créées ensuite (teamleader et teammates) :
// CL_BROWSER_PATH_MAP et CL_BROWSER_HINT (#106).
func exportBrowserPathsHub() {
	for _, v := range core.BrowserEnvVars() {
		_ = dtmux("set-environment", "-g", v.K, v.V).Run()
	}
}
