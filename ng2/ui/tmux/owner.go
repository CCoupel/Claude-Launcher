package tmux

import (
	"strconv"
	"time"

	"claude-launcher-ng2/core"
)

// heartbeat : appelé par layout-watch ; false si un autre launcher a pris la
// main — la fenêtre est alors marquée en lecture seule et le message affiché.
func heartbeat(pw projWin) bool {
	s, err := tout(pw.t, "display-message", "-p", "-t", pw.win, core.OwnerFormat)
	if err != nil {
		return true // canal momentanément indisponible : on ne conclut rien
	}
	if o := core.ParseOwner(s); o.Id != "" && o.Id != core.LauncherID() {
		_ = hub.Run("set-window-option", "-t", pw.hubWin, "@cl_readonly", "1")
		_ = hub.Run("display-message", "-d", "8000", "-t", pw.hubWin,
			"⬢ "+o.Id+" a pris la main : lecture seule — Entrée sur le projet pour la reprendre")
		return false
	}
	_ = pw.t.Run("set-option", "-t", "="+core.CtrSession+":", "@cl_owner_at", strconv.FormatInt(time.Now().Unix(), 10))
	return true
}
