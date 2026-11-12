package tmux

import (
	"fmt"
	"os"
	"path/filepath"
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

// claudeBin : commande lancée dans chaque projet (CL_CLAUDE_BIN : remplaçant de test).
func claudeBin() string {
	if b := os.Getenv("CL_CLAUDE_BIN"); b != "" {
		return core.Shq(b)
	}
	return "claude"
}

func tmuxOut(args ...string) (string, error) {
	b, err := dtmux(args...).Output()
	return strings.TrimSpace(string(b)), err
}

// syncLauncherEnv : recopie des variables du launcher dans settings.json ;
// false si elle a échoué (les secrets sont alors tapés comme avant).
func syncLauncherEnv(cfg core.Config) bool {
	_ = core.SyncBrowserHook(core.ClaudeDir()) // consigne navigateur à chaque session (#106)
	return core.SyncSettingsEnv(core.ClaudeDir(), cfg.EnvVars()) == nil
}

// launchProject : fenêtre, styles, couleur du pane, démarrage de claude (avec la
// confirmation /init-project si le template est en retard ou absent), puis le
// watcher de mise en page (layout.go).
// Mode de lancement : normal (ouverture d'un projet), ou juste après sa création
// depuis le launcher — géré (/init-project d'office) ou brut (ni git ni init-project).
type launchMode int

const (
	launchNormal launchMode = iota
	launchNewManaged
	launchNewRaw
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

	initCache := core.InitProjectPath()
	tpl := core.ReadTemplate(n.Path, latest)

	var initPre, initArg string
	switch {
	case mode == launchNewManaged:
		initArg = " /init-project" // projet tout juste créé : pas de question
	case mode == launchNewRaw: // ni question ni /init-project
	case tpl == core.TplOutdated:
		q := "↑ template en retard : projet " + core.ProjectTemplateRef(n.Path) + " → disponible " + core.TemplateRef(core.ReadTrim(core.TplVersionPath()), latest) +
			" — lancer /init-project ? [o/N] "
		initPre = `_ip_arg=''; read -rp ` + core.Shq(q) + ` _r; [[ "$_r" == [oOyY]* ]] && _ip_arg='/init-project'; `
		initArg = " $_ip_arg"
	case tpl == core.TplMissing:
		q := "🗑 fichiers template manquants (clone frais) : projet " + core.ProjectTemplateRef(n.Path) + ", disponible " + core.TemplateRef(core.ReadTrim(core.TplVersionPath()), latest) +
			" — lancer /init-project pour restaurer ? [O/n] "
		initPre = `_ip_arg='/init-project'; read -rp ` + core.Shq(q) + ` _r; [[ "$_r" == [nN]* ]] && _ip_arg=''; `
		initArg = " $_ip_arg"
	}

	// init-project.md est déposé dans le projet, sauf pour un projet brut tout juste créé
	copyInit := fmt.Sprintf("mkdir -p .claude/commands\n_ip=.claude/commands/init-project.md\n"+
		"[[ -s %s ]] && cp %s \"$_ip\" || echo '⚠  init-project.md non disponible — relancer le launcher connecté'\n",
		core.Shq(initCache), core.Shq(initCache))
	if mode == launchNewRaw {
		copyInit = ""
	}
	start := initPre + claudeBin() + " " + cfg.ClaudeOptions + initArg

	// Reprise de la dernière conversation (resume.go) : si elle est choisie, ni
	// question ni /init-project, qui viendraient perturber la conversation reprise.
	if mode == launchNormal {
		if pre, r, ok := cfg.ResumeShell(n.Path); ok {
			resume := claudeBin() + " " + cfg.ClaudeOptions + " --resume " + core.Shq(r.ID)
			if p := core.ResumePrompt(r); p != "" {
				resume += " " + core.Shq(p)
			}
			start = pre + "if [[ -n $_rs ]]; then " + resume + "; else " + start + "; fi"
		}
	}
	// sortie propre : marqueur posé seulement si claude rend la main au shell
	start += "; " + core.CleanExitShell(n.Path)
	// projet non géré : /end-session minimal, pour pouvoir le clore comme les autres
	if mode == launchNewRaw || mode == launchNormal && tpl == core.TplNone {
		ensureEndSession(n.Path)
	}
	if inCtr {
		// même démarrage qu'en local, exécuté par le bash du tmux du conteneur ;
		// init-project.md est copié depuis l'hôte (le cache n'est pas monté)
		if mode != launchNewRaw {
			if b, err := os.ReadFile(initCache); err == nil && len(b) > 0 {
				d := filepath.Join(n.Path, ".claude", "commands")
				if os.MkdirAll(d, 0o755) == nil {
					_ = os.WriteFile(filepath.Join(d, "init-project.md"), b, 0o644)
				}
			}
		}
		ctrStart := strings.ReplaceAll(start, claudeBin()+" ", "claude ")
		if cfg.CtrStopOnExit {
			// sortie propre : la session du conteneur se termine, et ctr-attach
			// (resté parent de l'attache) arrête le conteneur
			ctrStart += " && tmux kill-session"
		}
		script := fmt.Sprintf("cd %s; export PATH=\"$HOME/.local/bin:$PATH\"%s\n%s", core.Shq(n.Path), cfg.Exports(!synced), ctrStart)
		if err := os.WriteFile(startFile, []byte(script), 0o600); err != nil {
			return err
		}
		return dtmux("select-window", "-t", target).Run()
	}
	keys := fmt.Sprintf("cd %s%s\n%s%s", core.Shq(n.Path), cfg.Exports(!synced), copyInit, start)
	if err := dtmux("send-keys", "-t", target, keys, "Enter").Run(); err != nil {
		return fmt.Errorf("send-keys : %w", err)
	}

	_ = dtmux("run-shell", "-b", selfCommand("layout-watch", winID, pane, n.Path)).Run()
	return dtmux("select-window", "-t", target).Run()
}

// endSessionCmd : /end-session déposé dans un projet non géré (le template, lui,
// apporte le sien). Le taper laisse dans la conversation la marque que le
// navigateur reconnaît (resume.go) : la session n'est plus proposée à la reprise.
const endSessionCmd = `---
description: Clore la session (déposé par Claude-Launcher dans un projet non géré)
---

Fin de session. Sans modifier aucun fichier ni lancer de commande, résume en
quelques lignes ce qui a été fait pendant la session et ce qui reste à faire,
puis indique que la session peut être quittée (/exit).
`

// ensureEndSession : dépose .claude/commands/end-session.md s'il n'existe pas.
func ensureEndSession(path string) {
	f := filepath.Join(path, ".claude", "commands", "end-session.md")
	if _, err := os.Stat(f); err == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(f), 0o755) == nil {
		_ = os.WriteFile(f, []byte(endSessionCmd), 0o644)
	}
}
