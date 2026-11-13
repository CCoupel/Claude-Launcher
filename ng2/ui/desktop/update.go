package desktop

import (
	"os"
	"os/exec"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// ── Annonces de mise à jour (même cycle que la coque) ─────────────────────
//
// Vérification GitHub 6 s après le démarrage, puis toutes les 10 min (2 min
// tant qu'une release est attendue ou pas encore prête). NG : orange (tag sans
// release) → jaune (téléchargement et contrôle) → vert (un clic installe et
// relance l'application ; les projets restent dans tmux). Template : orange →
// vert (un clic l'adopte).

func (a *App) updateLoop() {
	d := 6 * time.Second
	for {
		select {
		case <-a.quit:
			return
		case <-time.After(d):
		}
		a.applyUpd(core.CheckUpdatesNow(false))
		a.mu.Lock()
		u := a.upd
		a.mu.Unlock()
		d = 10 * time.Minute
		if (u.newLauncher != "" && !u.launcherOK) || (u.newTpl != "" && !u.tplOK) {
			d = 2 * time.Minute
		}
	}
}

// applyUpd : résultat d'une recherche (core.CheckUpdatesNow).
func (a *App) applyUpd(msg core.UpdMsg) {
	a.mu.Lock()
	u := &a.upd
	prep := ""
	if msg.Ok {
		tag := ""
		if core.VersionNewer(core.NgVersion, msg.LauncherTag) {
			tag = msg.LauncherTag
		}
		if tag != u.newLauncher || msg.LauncherReady != u.launcherPub {
			u.hideLauncher = false
		}
		if tag != u.newLauncher {
			u.launcherOK, u.prefFile, u.prefErr = false, "", ""
		}
		u.newLauncher, u.launcherPub = tag, msg.LauncherReady
		if tag != "" && u.launcherPub && !u.launcherOK && !(u.prefBusy && u.prefTag == tag) {
			prep = tag
		}
		if msg.TplTag != u.newTpl || msg.TplReady != u.tplOK {
			u.hideTpl = false
		}
		u.newTpl, u.tplSha, u.tplOK = msg.TplTag, msg.TplSha, msg.TplReady
		a.tplVer = core.ReadTrim(core.TplVersionPath())
	}
	if msg.Manual {
		u.checking = false
		switch {
		case !msg.Ok:
			a.msg = "⚠ GitHub injoignable"
		case u.newLauncher != "":
			a.msg = ""
		default:
			a.msg = "✓ NG à jour"
		}
	}
	a.mu.Unlock()
	if msg.Ok {
		a.reloadTemplate()
	}
	if prep != "" {
		go a.prefetch(prep)
	}
	a.changed()
}

// reloadTemplate : le commit de référence a pu changer : badges template recalculés.
func (a *App) reloadTemplate() {
	latest := latestTemplate()
	a.mu.Lock()
	paths := make([]string, 0, len(a.tpl))
	for p := range a.tpl {
		paths = append(paths, p)
	}
	a.mu.Unlock()
	for _, p := range paths {
		t := core.ReadTemplate(p, latest)
		a.mu.Lock()
		a.tpl[p] = t
		a.mu.Unlock()
	}
	a.changed()
}

// prefetch : téléchargement et contrôle du nouveau binaire (annonce jaune).
func (a *App) prefetch(tag string) {
	a.mu.Lock()
	a.upd.prefTag, a.upd.prefBusy, a.upd.prefErr, a.upd.prefStep = tag, true, "", "start"
	a.mu.Unlock()
	a.changed()
	path, sum, err := core.Prefetch(tag, func(s string) {
		a.mu.Lock()
		if a.upd.prefTag == tag {
			a.upd.prefStep = s
		}
		a.mu.Unlock()
		a.changed()
	})
	a.mu.Lock()
	if a.upd.prefTag == tag {
		a.upd.prefBusy = false
		if err != nil {
			a.upd.prefErr = core.ShortReason(err)
		} else if tag == a.upd.newLauncher {
			a.upd.launcherOK, a.upd.prefFile, a.upd.prefSum, a.upd.prefErr = true, path, sum, ""
			a.upd.hideLauncher = false
		}
	}
	a.mu.Unlock()
	a.changed()
}

// clickAlert : clic sur une annonce (verte : installer / adopter ; jaune en
// échec : réessayer ; orange : sans effet).
func (a *App) clickAlert(kind string) {
	a.mu.Lock()
	u := a.upd
	a.mu.Unlock()
	switch {
	case kind == "launcher" && u.launcherOK && !u.updating:
		a.mu.Lock()
		a.upd.updating, a.msg = true, ""
		a.mu.Unlock()
		a.changed()
		go a.install(u.newLauncher, u.prefFile, u.prefSum)
	case kind == "launcher" && u.launcherPub && !u.prefBusy && u.prefErr != "":
		go a.prefetch(u.newLauncher)
	case kind == "template" && u.tplOK:
		go func() {
			r := core.AdoptTemplate(u.newTpl, u.tplSha)
			a.mu.Lock()
			if a.upd.newTpl == r.Tag {
				a.upd.newTpl, a.upd.tplSha, a.upd.tplOK = "", "", false
			}
			a.msg = "✓ template " + r.Tag + " adopté"
			if !r.Synced {
				a.msg = "⚠ template " + r.Tag + " : init-project.md non téléchargé"
			}
			a.tplVer = core.ReadTrim(core.TplVersionPath())
			a.mu.Unlock()
			a.reloadTemplate()
		}()
	}
}

// install : copie le binaire préparé à la place de l'actuel et relance
// l'application (les sessions des projets restent dans tmux).
func (a *App) install(tag, path, sum string) {
	r := core.InstallPreparedNow(tag, path, sum)
	a.mu.Lock()
	a.upd.updating = false
	switch {
	case r.Stale && a.upd.newLauncher != "":
		a.upd.launcherOK, a.upd.prefFile = false, ""
		a.msg = "⚠ mise à jour : " + r.Err.Error()
		a.mu.Unlock()
		a.changed()
		go a.prefetch(tag)
		return
	case r.Err != nil:
		a.msg = "⚠ mise à jour : " + r.Err.Error()
	case r.BinDone:
		a.msg = "✓ " + tag + " installée — redémarrage…"
	default:
		a.msg = "✓ déjà à jour"
		a.upd.hideLauncher = true
	}
	a.mu.Unlock()
	a.changed()
	if r.BinDone {
		restartApp()
	}
}

// restartApp : relance le binaire installé (nouvelle fenêtre) et quitte ; le
// mode web la remplace (même adresse, port libéré d'abord, web.go).
var restartApp = func() {
	if relaunch(os.Args[1:]...) == nil {
		time.Sleep(300 * time.Millisecond)
		quitHook()
	}
}

// restartEnv : variables ajoutées pour le binaire relancé.
var restartEnv []string

// relaunch : lance le binaire installé avec args, sans mise à jour au démarrage.
func relaunch(args ...string) error {
	exe, err := platform.SelfExe()
	if err != nil {
		return err
	}
	c := exec.Command(exe, args...)
	c.Env = append(append(os.Environ(), "CL_NO_UPDATE=1"), restartEnv...)
	return c.Start()
}

// themeHook : thème de la fenêtre native (« light », « dark », « auto ») ; nil
// en mode web (le navigateur suit la page).
var themeHook func(mode string)

// quitHook : fermeture de l'interface (Wails : runtime.Quit ; web : sortie).
var quitHook = func() { os.Exit(0) }
