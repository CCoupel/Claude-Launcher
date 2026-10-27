package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Onglet « ⌗ Teamleaders » : une vignette interactive par projet ouvert (le
// teamleader de chaque fenêtre), en grille. Les vignettes sont celles du
// launcher bash (`claude-launcher.sh --mirror WIN NOM` : affichage du pane du
// leader et relais des touches) ; ce gestionnaire remplace seulement le
// --mirror-manager bash : il crée/retire les vignettes et applique la grille.

const teamWindow = "⌗ Teamleaders"

const teamPlaceholder = `printf '\n  Aucun projet ouvert — lancez-en un depuis le navigateur.\n'; exec sleep 2147483647`

type mirrorMgr struct {
	cfg     Config
	prevSig string
	colored map[string]string // pane -> couleur déjà appliquée
	guarded string            // onglet déjà protégé par keepTeamWindow
}

// keepTeamWindow : une vignette --mirror se termine d'elle-même dès que la fenêtre
// de son projet disparaît, avant le tick suivant. Seul pane de l'onglet, elle
// l'emporterait avec elle, et avec lui claude-hub s'il ne reste aucun projet :
// le pane mort reste affiché jusqu'à ce que tick le remplace.
func keepTeamWindow(win string) {
	_ = dtmux("set-option", "-w", "-t", win, "remain-on-exit", "on").Run()
}

// runMirrorManager tourne dans le navigateur (mode coque), une fois par seconde.
func runMirrorManager(cfg Config) {
	mm := &mirrorMgr{cfg: cfg, colored: map[string]string{}}
	for {
		time.Sleep(time.Second)
		mm.tick()
	}
}

type tmuxWin struct{ id, name string }

func listWindows() []tmuxWin {
	out, err := tmuxOut("list-windows", "-t", exactS(hubSession), "-F", "#{window_id}|#{window_name}")
	if err != nil {
		return nil
	}
	var ws []tmuxWin
	for _, l := range strings.Split(out, "\n") {
		if id, name, ok := strings.Cut(l, "|"); ok && id != "" {
			ws = append(ws, tmuxWin{id, name})
		}
	}
	return ws
}

func (mm *mirrorMgr) tick() {
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		return
	}
	var teamID string
	var want []tmuxWin // projets ouverts, dans l'ordre des onglets
	for _, w := range listWindows() {
		switch w.name {
		case stateWindow, "[menu]":
		case teamWindow:
			teamID = w.id
		default:
			want = append(want, w)
		}
	}
	if teamID == "" {
		// épinglé en tête
		id, err := tmuxOut("new-window", "-d", "-b", "-P", "-F", "#{window_id}", "-n", teamWindow,
			"-t", exactS(hubSession)+":^", teamPlaceholder)
		if err != nil {
			return
		}
		teamID = id
		mm.prevSig = ""
	}
	if mm.guarded != teamID {
		keepTeamWindow(teamID)
		mm.guarded = teamID
	}

	// panes actuels : vignettes (@mirror_win renseigné) et pane d'attente
	have := map[string]string{} // window id -> pane id
	var placeholders, stale, all []string
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w.id] = true
	}
	out, _ := tmuxOut("list-panes", "-t", teamID, "-F", "#{pane_id}|#{pane_dead}|#{@mirror_win}")
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) != 3 || f[0] == "" {
			continue
		}
		pid, dead, mw := f[0], f[1] == "1", f[2]
		all = append(all, pid)
		switch {
		case dead: // vignette terminée (remain-on-exit) : remplacée ci-dessous
			stale = append(stale, pid)
		case mw == "":
			placeholders = append(placeholders, pid)
		case !wantSet[mw]:
			stale = append(stale, pid)
		default:
			have[mw] = pid
		}
	}
	if len(all) == 0 {
		return
	}

	// une fenêtre tmux sans pane se ferme : garder un pane d'attente tant qu'aucune vignette
	if len(placeholders) == 0 && len(have) == 0 {
		if pid, err := tmuxOut("split-window", "-d", "-t", all[0], "-P", "-F", "#{pane_id}", teamPlaceholder); err == nil {
			placeholders = append(placeholders, pid)
		}
	}
	for _, pid := range stale {
		_ = dtmux("kill-pane", "-t", pid).Run()
		mm.prevSig = ""
	}

	// vignettes manquantes
	script := scriptPath()
	// coupe à partir d'un pane vivant (les vignettes périmées viennent d'être fermées)
	var target string
	for _, pid := range have {
		target = pid
	}
	if len(placeholders) > 0 {
		target = placeholders[len(placeholders)-1]
	}
	if target == "" {
		return
	}
	added := false
	for _, w := range want {
		if _, ok := have[w.id]; ok {
			continue
		}
		cmd := fmt.Sprintf("bash %s --mirror %s %s", shq(script), shq(w.id), shq(w.name))
		pid, err := tmuxOut("split-window", "-d", "-t", target, "-P", "-F", "#{pane_id}", cmd)
		if err != nil {
			continue // pas la place : réessayé au prochain tick
		}
		_ = dtmux("set-option", "-p", "-t", pid, "@mirror_win", w.id).Run()
		have[w.id], target, added = pid, pid, true
		// la grille avant la coupe suivante, sinon tmux manque de place
		_ = dtmux("select-layout", "-t", teamID, "tiled").Run()
	}
	if added || len(have) > 0 {
		for _, pid := range placeholders {
			if len(have) > 0 {
				_ = dtmux("kill-pane", "-t", pid).Run()
				mm.prevSig = ""
			}
		}
	}

	// fond de chaque vignette = couleur du projet
	for _, w := range want {
		pid, ok := have[w.id]
		if !ok {
			continue
		}
		c := "bg=colour" + mm.cfg.projectColor(w.name)
		if mm.colored[pid] != c {
			_ = dtmux("set-option", "-p", "-t", pid, "window-style", c).Run()
			mm.colored[pid] = c
		}
	}

	// grille : à chaque changement de taille ou de contenu
	ids := make([]string, 0, len(have))
	for _, pid := range have {
		ids = append(ids, pid)
	}
	sort.Strings(ids)
	size, _ := tmuxOut("display-message", "-t", teamID, "-p", "#{window_width}x#{window_height}")
	sig := size + "|" + strings.Join(ids, ",")
	if sig != mm.prevSig {
		mm.prevSig = sig
		_ = dtmux("select-layout", "-t", teamID, "tiled").Run()
	}
}
