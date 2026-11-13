package tmux

import (
	"bufio"
	"fmt"
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
	// conteneur, bootstrap, session et propriétaire : core/ctropen.go
	op, err := core.OpenCtr(proj, core.CtrOpenIO{
		Out:   os.Stdout,
		In:    bufio.NewReader(os.Stdin),
		Start: func() []byte { b, _ := os.ReadFile(startFile); return b },
		Size:  paneSize,
		Done:  func() { _ = os.Remove(startFile) },
		Owner: core.OwnerChoice,
	})
	if err != nil {
		return err
	}
	if op.Canceled {
		return nil
	}
	e, name, readOnly := op.E, op.Name, op.ReadOnly
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
