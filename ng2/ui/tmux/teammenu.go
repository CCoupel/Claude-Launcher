package tmux

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// Réglage du modèle et de l'effort d'un teammate depuis la carte TEAM de la coque (#250, maquette M2 A) : un clic sur
// les colonnes modèle / effort ouvre un menu tmux (display-menu) ; chaque choix lance la sous-commande team-set en
// tâche de fond (même écriture que teamAgentSet de la fenêtre native, la coque est locale : pas de refus distant).

// teamSetMsg : dernier résultat de team-set, lu par le pane des cartes (affiché 6 s sous la liste).
type teamSetMsg struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
	OK   bool   `json:"ok"`
	Warn bool   `json:"warn,omitempty"` // note à lire (teamleader : la session en cours garde son modèle)
	At   int64  `json:"at"`
}

func teamSetMsgPath() string { return filepath.Join(core.CacheDir(), "teamset-msg.json") }

func readTeamSetMsg() (teamSetMsg, bool) {
	var m teamSetMsg
	b, err := os.ReadFile(teamSetMsgPath())
	if err != nil || json.Unmarshal(b, &m) != nil {
		return m, false
	}
	return m, true
}

// RunTeamSet : sous-commande « team-set <projet> <agent> [model=<m>|default] [effort=<e>|default] ». Écrit la ligne
// de note sur out, code 0 ; refus : message sur errw, code 2.
func RunTeamSet(args []string, out, errw io.Writer) int {
	if len(args) < 3 {
		fmt.Fprintln(errw, "usage : claude-launcher-ng2 team-set <projet> <agent> [model=<m>|default] [effort=<e>|default]")
		return 2
	}
	proj, agent := args[0], args[1]
	var model, effort *string
	for _, a := range args[2:] {
		k, v, ok := strings.Cut(a, "=")
		switch {
		case ok && k == "model" && model == nil:
			model = &v
		case ok && k == "effort" && effort == nil:
			effort = &v
		default:
			fmt.Fprintf(errw, "argument invalide : %q\n", a)
			return 2
		}
	}
	res, err := core.SetAgentOverride(proj, agent, model, effort)
	if err != nil {
		note(proj, teamSetMsg{Msg: agent + " : " + res.Msg})
		fmt.Fprintln(errw, res.Msg)
		return 2
	}
	note(proj, teamSetMsg{OK: true, Warn: res.Settings != nil, Msg: agent + " : " + res.Note})
	fmt.Fprintln(out, res.Note)
	return 0
}

func note(proj string, m teamSetMsg) {
	m.Path, m.At = proj, time.Now().Unix()
	if b, err := json.Marshal(m); err == nil {
		_ = core.WriteFileAtomic(teamSetMsgPath(), b, 0o644)
	}
}

// ── menu ──────────────────────────────────────────────────────────────────

// teamMenuArgs : arguments de « tmux display-menu » pour une ligne de la carte (entrées, raccourcis, entrées
// grisées selon la table de compatibilité). x, y < 0 : position par défaut (centre). exe : binaire du launcher.
func teamMenuArgs(l core.TeamLine, proj, exe string, x, y int) []string {
	run := func(kv ...string) string { // commande tmux d'un choix : team-set en tâche de fond
		cmd := shQuote(exe) + " team-set " + shQuote(proj) + " " + shQuote(l.Agent)
		for _, a := range kv {
			cmd += " " + a
		}
		return `run-shell -b "` + tmuxDQ(cmd) + `"`
	}
	a := []string{"display-menu", "-T", " " + l.Agent + " — modèle et effort "}
	if x >= 0 && y >= 0 {
		a = append(a, "-x", strconv.Itoa(x), "-y", strconv.Itoa(y))
	}
	mark := func(on bool) string {
		if on {
			return "✓ "
		}
		return "  "
	}
	tplMark := func(v, tpl string) string {
		if v == tpl {
			return " (= défaut)"
		}
		return ""
	}
	// modèle
	a = append(a, mark(l.ModelSrc != "companion")+"Défaut (template) — "+orDash(l.TplModel), "d", run("model=default"))
	for _, m := range []struct{ v, key string }{{"opus", "o"}, {"sonnet", "s"}, {"haiku", "h"}, {"fable", "f"}} {
		on := l.ModelSrc == "companion" && strings.TrimPrefix(l.Model, "claude-") == m.v
		a = append(a, mark(on)+m.v+tplMark(m.v, l.TplModel), m.key, run("model="+m.v))
	}
	a = append(a, "", "", "")
	// effort : filtré selon le modèle résolu ; « Défaut » jamais grisé
	a = append(a, mark(l.EffortSrc != "companion")+"Défaut (template) — "+orDash(l.TplEffort), "0", run("effort=default"))
	acc, _ := core.ModelEfforts(l.Model)
	for i, e := range core.Efforts {
		label := mark(l.EffortSrc == "companion" && l.Effort == e) + e + tplMark(e, l.TplEffort)
		if !slices.Contains(acc, e) { // grisé, sans raccourci
			a = append(a, "-"+label+" (non pris en charge)", "", "")
			continue
		}
		a = append(a, label, strconv.Itoa(i+1), run("effort="+e))
	}
	a = append(a, "", "", "")
	a = append(a, "↺ Tout par défaut", "t", run("model=default", "effort=default"))
	a = append(a, "", "", "")
	if l.Agent == "teamleader" {
		a = append(a, "-teamleader.md + settings.json écrits ; /model, /effort", "", "")
	} else if l.Started {
		a = append(a, "-appliqué au prochain spawn", "", "")
	} else {
		a = append(a, "-appliqué au prochain démarrage", "", "")
	}
	return a
}

// shQuote : argument shell entre apostrophes.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// tmuxDQ : chaîne placée entre guillemets d'une commande tmux (\ " $ et # protégés : formats, variables).
func tmuxDQ(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "#", "##").Replace(s)
}

// teamMenuMsg : issue du clic (menu ouvert, ou raison du refus).
type teamMenuMsg struct{ path, msg string }

// teamMenuCmd : ouvre le menu de réglage de la ligne, à l'endroit du clic (coordonnées du pane des cartes dans la
// coque + souris) ; repli au centre si la position du pane est inconnue.
func teamMenuCmd(proj string, l core.TeamLine, mx, my int) tea.Cmd {
	return func() tea.Msg {
		exe, err := platform.SelfExe()
		if err != nil {
			return teamMenuMsg{proj, "binaire du launcher introuvable : " + err.Error()}
		}
		x, y := -1, -1
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			if b, err := otmux("display-message", "-p", "-t", pane, "#{pane_left} #{pane_top}").Output(); err == nil {
				if f := strings.Fields(string(b)); len(f) == 2 {
					px, e1 := strconv.Atoi(f[0])
					py, e2 := strconv.Atoi(f[1])
					if e1 == nil && e2 == nil {
						x, y = px+mx, py+my
					}
				}
			}
		}
		if err := otmux(teamMenuArgs(l, proj, exe, x, y)...).Run(); err != nil {
			return teamMenuMsg{proj, "menu indisponible : " + err.Error()}
		}
		return nil
	}
}

// notSettable : raison pour laquelle la ligne n'est pas réglable ("" : réglable).
func notSettable(l core.TeamLine) string {
	switch {
	case l.Agent == "":
		return "pas un teammate du registre du template"
	case !l.Editable:
		return l.Agent + " n'a ni model ni effort dans son template"
	}
	return ""
}
