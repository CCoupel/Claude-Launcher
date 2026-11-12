package tmux

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"claude-launcher-ng2/core"
)

// ── ctr-attach : contenu de la fenêtre d'un projet en conteneur ───────────
//
// Lancé par launchProject dans la fenêtre du projet (hub) :
//  1. conteneur créé/démarré, bootstrap (idempotent : rapide s'il est à jour) ;
//  2. si le tmux du projet n'a pas de session : création, puis envoi du script
//     de démarrage (le même qu'en local : questions /init-project et reprise,
//     claude, marqueur de sortie propre) ;
//  3. layout-watch lancé sur le hub pour cette fenêtre ;
//  4. remplacement du processus par l'attache au tmux du projet. Si le
//     launcher (ou le hub) meurt, la session du conteneur continue : la fenêtre
//     est recréée par reattachContainers au prochain démarrage.

func runCtrAttach(proj, startFile string) error {
	b := core.ProjectBackend(proj)
	e, ok := core.EngineOf(b)
	if !ok {
		return fmt.Errorf("%s : backend %s sans conteneur", proj, b.Name)
	}
	env, err := core.LoadEnvFull(proj)
	if err != nil {
		return err
	}
	fmt.Printf("\033[1m⬡ %s — %s (%s)\033[0m\n", baseName(proj), b.Name, env.Image)
	if _, err := os.Stat(core.LauncherEnvPath()); err == nil {
		fmt.Printf("  + bootstrap du launcher : %s\n", core.LauncherEnvPath())
	}
	if len(env.Local) > 0 {
		fmt.Printf("  dossiers privés au conteneur : %s\n", strings.Join(env.Local, ", "))
	}
	if ps := core.Diagnose(b); len(ps) > 0 {
		var s strings.Builder
		for _, p := range ps {
			fmt.Fprintf(&s, "\n  ⚠ %s\n    → %s", p.What, p.Fix)
		}
		return fmt.Errorf("backend %s inutilisable :%s", b.Name, s.String())
	}
	// groupe de serveurs (#61) : serveur du projet, choisi sous verrou s'il est nouveau
	e, unlock, err := e.Place(proj, core.Asker{Out: os.Stdout, In: bufio.NewReader(os.Stdin)})
	if err != nil {
		return err
	}
	name, err := e.Ensure(proj, env, os.Stdout)
	unlock()
	if err != nil {
		return err
	}
	if err := e.Bootstrap(name, proj, env, os.Stdout); err != nil {
		return err
	}
	// cache de consommation commun (panneau milestone) : voir linkUsageCache
	_ = e.Exec(name, false, strings.NewReader(core.LinkUsageScript()), io.Discard, "sh", "-s")
	if l, ok := e.ReadLive(name); ok && !l.Claude {
		// tmux resté seul après un /exit (point bleu) : la session est recréée
		// pour que claude redémarre, au lieu de se rattacher à son shell
		fmt.Println("  claude n'est plus en marche dans la session : redémarrage")
		_ = e.Exec(name, false, nil, io.Discard, core.TmuxIn("kill-session", "-t", "="+core.CtrSession)...)
	}
	if !e.HasSession(name) {
		if err := e.OnStart(name, proj, env); err != nil {
			return err
		}
		// le script de démarrage est écrit par launchProject juste après la création
		// de la fenêtre : l'attendre (bootstrap déjà à jour = quelques dixièmes de s)
		var start []byte
		for i := 0; i < 50 && len(start) == 0; i++ {
			if start, _ = os.ReadFile(startFile); len(start) == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		w, h := paneSize()
		if err := e.NewCtrSession(name, proj, w, h); err != nil {
			return err
		}
		if len(start) > 0 {
			_ = e.Exec(name, false, nil, os.Stdout, core.TmuxIn("send-keys", "-t", "="+core.CtrSession+":", string(start), "Enter")...)
		}
	}
	_ = os.Remove(startFile)
	e.SetCtrBrowserPaths(name)
	// propriétaire (#48) : un seul launcher pilote la session
	readOnly := false
	if o := e.Owner(name); o.Alive() && o.Id != core.LauncherID() {
		switch core.OwnerChoice(o) {
		case "l":
			readOnly = true
		case "p":
			e.TakeOver(name)
		default:
			return nil
		}
	} else {
		e.ClaimFree(name)
	}
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if win, err := tout(hub, "display-message", "-p", "-t", pane, "#{window_id}"); err == nil {
			_ = hub.Run("set-window-option", "-t", win, "@cl_server", e.B.Server)
			if readOnly {
				_ = hub.Run("set-window-option", "-t", win, "@cl_readonly", "1")
			} else {
				// premier pane créé, pas le pane actif (un teammate peut avoir le focus)
				panes, _ := e.Output(name, core.TmuxIn("list-panes", "-s", "-t", "="+core.CtrSession, "-F", "#{pane_id}")...)
				leader, _ := core.LeaderOf(panes)
				_ = hub.Run("run-shell", "-b", selfCommand("layout-watch", win, leader, proj))
			}
		}
	}
	att := []string{"attach-session", "-t", "=" + core.CtrSession}
	if readOnly {
		att = append(att, "-r")
	}
	a := e.ExecArgv(name, false, true, core.TmuxIn(att...)...)
	bin, err := lookPath(a[0])
	if err != nil {
		return err
	}
	if !core.LoadConfig().CtrStopOnExit || readOnly {
		return syscall.Exec(bin, a, os.Environ())
	}
	// CTR_STOP_ON_EXIT : l'attache reste un enfant ; quand elle rend la main et que
	// la session n'existe plus (claude sorti proprement, ou fermeture), le
	// conteneur est arrêté. Simple détachement : la session vit, rien n'est arrêté.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
	c := exec.Command(bin, a[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
	if !e.HasSession(name) {
		fmt.Println("\n⏻ claude terminé : arrêt du conteneur (CTR_STOP_ON_EXIT)")
		return e.Stop(proj, name)
	}
	return nil
}

var lookPath = exec.LookPath

func baseName(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// paneSize : taille du pane courant (pour la première fenêtre du tmux du projet).
func paneSize() (int, int) {
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if s, err := tout(hub, "display-message", "-p", "-t", pane, "#{pane_width} #{pane_height}"); err == nil {
			if f := strings.Fields(s); len(f) == 2 {
				w, _ := strconv.Atoi(f[0])
				h, _ := strconv.Atoi(f[1])
				if w > 0 && h > 0 {
					return w, h
				}
			}
		}
	}
	return 200, 50
}

// ctrStartFile : script de démarrage d'un projet en conteneur, passé à
// ctr-attach (trop long et trop riche en guillemets pour la ligne de commande).
func ctrStartFile(proj, script string) (string, error) {
	d := core.CacheDir() + "/ctr-start"
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	f := fmt.Sprintf("%s/%s-%d.sh", d, core.CtrName(proj), time.Now().UnixNano())
	return f, os.WriteFile(f, []byte(script), 0o600)
}
