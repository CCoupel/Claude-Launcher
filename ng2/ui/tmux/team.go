package tmux

import (
	"sync"
	"time"

	"claude-launcher-ng2/core"
)

// ctrPaneVersions : panes d'un projet en conteneur. Leurs PID sont ceux du
// conteneur : ne jamais les chercher dans le /proc de l'hôte (PID = ""). Les
// teammates portent déjà leur version (nom du processus) ; le pane du leader
// reçoit celle de claude installé DANS le conteneur (cible de ~/.local/bin/claude).
func ctrPaneVersions(pw projWin, panes []core.PaneInfo) {
	v := ""
	for i := range panes {
		panes[i].PID = ""
		if core.ClaudeVerRe.FindString(panes[i].Cmd) != "" {
			continue
		}
		if v == "" {
			v = ctrClaudeVersion(pw)
		}
		if v != "" && i == 0 { // le leader : premier pane (doLayout le place en tête)
			panes[i].Cmd = v
		}
	}
}

// ctrVerCache : version de claude par conteneur, une minute.
var ctrVerCache = struct {
	sync.Mutex
	m map[string]verEntry
}{m: map[string]verEntry{}}

type verEntry struct {
	at time.Time
	v  string
}

func ctrClaudeVersion(pw projWin) string {
	ctrVerCache.Lock()
	e, ok := ctrVerCache.m[pw.hubWin]
	ctrVerCache.Unlock()
	if ok && time.Since(e.at) < time.Minute {
		return e.v
	}
	out, _ := pw.ctrExec("sh", "-c", `basename "$(readlink -f "$HOME/.local/bin/claude" 2>/dev/null)"`)
	v := core.ClaudeVerRe.FindString(out)
	ctrVerCache.Lock()
	ctrVerCache.m[pw.hubWin] = verEntry{time.Now(), v}
	ctrVerCache.Unlock()
	return v
}
