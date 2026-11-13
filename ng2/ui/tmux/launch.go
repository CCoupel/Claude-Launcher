package tmux

import (
	"fmt"
	"os"
	"strings"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// Nom de la session intérieure ; CL_HUB permet une répétition sans toucher à claude-hub.
var hubSession = func() string {
	if h := os.Getenv("CL_HUB"); h != "" {
		return h
	}
	return "claude-hub-ng2"
}()

// targetSession : le launcher bash vise toujours claude-hub (les sessions
// groupées partagent ses fenêtres) ; à défaut, la session courante.
func targetSession(current string) string {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() == nil {
		return hubSession
	}
	return current
}

// exactS / exactW : cibles tmux exactes. Sans « = », tmux accepte un préfixe :
// « -t claude-hub » viserait claude-hub-test si claude-hub n'existe pas.
func exactS(s string) string    { return "=" + s }
func exactW(s, w string) string { return "=" + s + ":=" + w }

// selfCommand : ligne de commande d'une sous-commande du NG lancée par tmux
// (vignette, watcher), avec le serveur tmux et la session visés.
func selfCommand(args ...string) string {
	exe, err := platform.SelfExe()
	if err != nil {
		exe = "claude-launcher-ng2"
	}
	c := "CL_HUB=" + core.Shq(hubSession) + " CL_INNER_SOCKET=" + core.Shq(core.InnerSocket()) +
		" " + core.Shq(exe)
	for _, a := range args {
		c += " " + core.Shq(a)
	}
	return c
}

// claudeBin : commande lancée dans chaque projet (core.ClaudeBin).
func claudeBin() string { return core.ClaudeBin() }

func tmuxOut(args ...string) (string, error) {
	b, err := dtmux(args...).Output()
	return strings.TrimSpace(string(b)), err
}

// syncLauncherEnv : recopie des variables du launcher dans settings.json
// (core.SyncLauncherEnv).
func syncLauncherEnv(cfg core.Config) bool { return core.SyncLauncherEnv(cfg) }

// launchProject : fenêtre, styles, couleur du pane, démarrage de claude (avec la
// confirmation /init-project si le template est en retard ou absent), puis le
// watcher de mise en page (layout.go).
// Mode de lancement : normal (ouverture d'un projet), ou juste après sa création
// depuis le launcher — géré (/init-project d'office) ou brut (ni git ni init-project).
type launchMode = core.LaunchMode

const (
	launchNormal     = core.LaunchNormal
	launchNewManaged = core.LaunchNewManaged
	launchNewRaw     = core.LaunchNewRaw
)

func launchProject(cfg core.Config, session string, n core.Node, latest string, mode launchMode) error {
	sess := targetSession(session)
	if tmuxWindows(sess)[n.Key] {
		// fenêtre en lecture seule (un autre launcher a la main) : la rouvrir,
		// ce qui repose la question rejoindre / prendre la main (#48)
		if ro, _ := tmuxOut("display-message", "-p", "-t", exactW(sess, n.Key), "#{@cl_readonly}"); ro != "1" {
			return dtmux("select-window", "-t", exactW(sess, n.Key)).Run()
		}
		// détacher d'abord le client du conteneur (Ctrl+B d, reçu tel quel par le
		// tmux du conteneur) : tuer la fenêtre seule y laisse un client fantôme
		w := exactW(sess, n.Key)
		_ = dtmux("send-keys", "-t", w, "C-b", "d").Run()
		for i := 0; i < 20 && tmuxWindows(sess)[n.Key]; i++ {
			time.Sleep(100 * time.Millisecond)
		}
		_ = dtmux("kill-window", "-t", w).Run()
	}
	be, err := core.LaunchBackend(n.Path)
	if err != nil {
		return err
	}
	synced := syncLauncherEnv(cfg)
	if _, ok := core.EngineOf(be); !ok {
		// sur l'hôte : jamais pendant qu'un autre launcher fait tourner ce projet
		// dans un conteneur (deux claude dans le même dossier partagé)
		if who := core.ForeignOwner(n.Path); who != "" {
			return fmt.Errorf("projet en cours dans un conteneur, piloté par %s — le fermer là-bas ou F2 pour le rejoindre sur ce backend", who)
		}
	}
	_, inCtr := core.EngineOf(be)
	if !inCtr {
		exportBrowserPathsHub() // config relue à chaque ouverture (#104)
	}
	nw := []string{"new-window", "-P", "-F", "#{window_id} #{pane_id}", "-t", exactS(sess) + ":", "-n", n.Key}
	var startFile string
	if inCtr {
		// la fenêtre exécute ctr-attach (conteneur, bootstrap, attache) ; le script
		// de démarrage lui est passé par fichier
		f, err := ctrStartFile(n.Path, "")
		if err != nil {
			return err
		}
		startFile = f
		nw = append(nw, selfCommand("ctr-attach", n.Path, startFile))
	}
	ids, err := tmuxOut(nw...)
	if err != nil {
		return fmt.Errorf("new-window : %w", err)
	}
	f := strings.Fields(ids)
	if len(f) != 2 {
		return fmt.Errorf("new-window : sortie inattendue %q", ids)
	}
	winID, pane := f[0], f[1]
	target := exactW(sess, n.Key)

	_ = dtmux("set-window-option", "-t", target, "window-status-style", "fg=colour242,bg=colour236").Run()
	_ = dtmux("set-window-option", "-t", target, "window-status-current-style", "bold,fg=colour255,bg=colour27").Run()
	_ = dtmux("set-option", "-p", "-t", pane, "window-style", "bg=colour"+cfg.ProjectColor(n.Key)).Run()

	if inCtr {
		_ = dtmux("set-window-option", "-t", target, "@cl_backend", be.Name).Run()
		_ = dtmux("set-window-option", "-t", target, "@cl_ctr", core.CtrRef(be, n.Path)).Run()
		_ = dtmux("set-window-option", "-t", target, "@cl_nested", "1").Run()
	}

	// démarrage : questions /init-project et reprise, claude (core/start.go)
	start, copyInit := core.StartCommands(cfg, n.Path, latest, mode)
	if inCtr {
		script := core.CtrStartScript(cfg, n.Path, start, mode, synced)
		if err := os.WriteFile(startFile, []byte(script), 0o600); err != nil {
			return err
		}
		return dtmux("select-window", "-t", target).Run()
	}
	keys := core.HostStartKeys(cfg, n.Path, start, copyInit, synced)
	if err := dtmux("send-keys", "-t", target, keys, "Enter").Run(); err != nil {
		return fmt.Errorf("send-keys : %w", err)
	}

	_ = dtmux("run-shell", "-b", selfCommand("layout-watch", winID, pane, n.Path)).Run()
	return dtmux("select-window", "-t", target).Run()
}

// endSessionCmd / ensureEndSession : /end-session déposé dans un projet non
// géré (core.EndSessionCmd, core.EnsureEndSession).
const endSessionCmd = core.EndSessionCmd

func ensureEndSession(path string) { core.EnsureEndSession(path) }
