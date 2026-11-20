package tmux

import (
	"fmt"
	"strings"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// ── Relance de la coque après une mise à jour du binaire (#219) ───────────
//
// Panes de la coque (navigateur, état) et vignettes mirror : respawn-pane -k avec le binaire installé ;
// layout-watch anciens : SIGTERM puis SIGKILL, relancés par run-shell -b pour leur fenêtre. JAMAIS kill-session ni
// kill-server : les fenêtres et les panes des projets restent intacts. Seuls les processus du MÊME serveur tmux
// (CL_INNER_SOCKET) sont touchés (core.StaleProcs).

// Points d'injection (tests).
var (
	relaunchProcs     = core.StaleProcs
	relaunchTerminate = core.TerminateStale
	relaunchGrace     = func() { time.Sleep(2 * time.Second) }
)

// RelaunchResult : ce qui a été relancé.
type RelaunchResult struct {
	Panes, Mirrors, Watchers int
}

func (r RelaunchResult) String() string {
	return fmt.Sprintf("%d pane(s) de la coque, %d vignette(s), %d layout-watch relancés", r.Panes, r.Mirrors, r.Watchers)
}

// RelaunchShell relance la coque sur le binaire installé. Le navigateur (qui peut être l'appelant) est relancé EN
// DERNIER : son respawn termine le processus appelant.
func RelaunchShell() (RelaunchResult, error) {
	var res RelaunchResult
	exe, err := platform.SelfExe()
	if err != nil {
		return res, err
	}
	stale := relaunchProcs()

	// vignettes : respawn de leur pane (arguments relus de l'ancien processus : fenêtre et nom)
	paneOf := map[string]string{} // fenêtre vignetée -> pane
	if out, err := tmuxOut("list-panes", "-s", "-t", exactS(hubSession), "-F", "#{pane_id}|#{@mirror_win}"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if id, w, ok := strings.Cut(l, "|"); ok && w != "" {
				paneOf[w] = id
			}
		}
	}
	var kill []core.StaleProc
	for _, p := range stale {
		args := core.ProcArgs(p.Pid)
		switch {
		case p.Role == "mirror" && len(args) >= 4 && paneOf[args[2]] != "":
			if dtmux("respawn-pane", "-k", "-t", paneOf[args[2]], selfCommand("mirror", args[2], args[3])).Run() == nil {
				res.Mirrors++
				continue
			}
			kill = append(kill, p)
		default:
			kill = append(kill, p)
		}
	}

	// layout-watch (et vignettes sans pane) : arrêt propre puis arrêt forcé, relance pour leur fenêtre
	if len(kill) > 0 {
		args := relaunchTerminate(kill, relaunchGrace)
		for _, p := range kill {
			a := args[p.Pid]
			if p.Role == "layout-watch" && len(a) >= 3 {
				if dtmux("run-shell", "-b", selfCommand(a[1:]...)).Run() == nil {
					res.Watchers++
				}
			}
		}
	}

	// panes de la coque : état, puis navigateur en dernier
	roles, _ := outerPanes()
	if p := roles[roleInfo]; p != "" {
		if otmux("respawn-pane", "-k", "-t", p, stateCommand(exe)).Run() == nil {
			res.Panes++
		}
	}
	if p := roles[roleNav]; p != "" {
		if otmux("respawn-pane", "-k", "-t", p, navCommand(exe)).Run() == nil {
			res.Panes++
		}
	}
	return res, nil
}
