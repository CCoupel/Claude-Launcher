package desktop

import (
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #167 : CL_UPDATE_MODE=notify — annonce sans bouton d'installation, ni téléchargement.
func TestUpdateNotifyModeAnnouncesWithoutInstall(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	t.Setenv("CL_UPDATE_MODE", "notify")
	a.mu.Lock()
	a.upd.newLauncher, a.upd.launcherPub, a.upd.launcherOK = "v9.9.9", true, true // même une version prête : rien à installer
	a.mu.Unlock()
	a.mu.Lock()
	al := a.alerts()
	a.mu.Unlock()
	var got *Alert
	for i := range al {
		if al[i].Kind == "launcher" {
			got = &al[i]
		}
	}
	if got == nil || !strings.Contains(got.Text, "v9.9.9 disponible · mise à jour par l'image") || got.Click || !got.Notify {
		t.Fatalf("annonce en mode notify : %+v", got)
	}
	// un clic n'installe rien
	a.clickAlert("launcher")
	a.mu.Lock()
	updating := a.upd.updating
	a.mu.Unlock()
	if updating {
		t.Error("un clic sur l'annonce a lancé une installation en mode notify")
	}
	if !core.UpdateNotifyOnly() {
		t.Error("UpdateNotifyOnly() faux avec CL_UPDATE_MODE=notify")
	}
	t.Setenv("CL_UPDATE_MODE", "")
	if core.UpdateNotifyOnly() {
		t.Error("UpdateNotifyOnly() vrai sans la variable")
	}
}
