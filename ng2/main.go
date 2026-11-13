// Draft Go du menu de claude-launcher : même sources de données que le bash
// (conf, arbre de projets, badges git/template, consommation Claude, fenêtres
// tmux), mais rafraîchies en parallèle sans jamais bloquer l'affichage.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
	"claude-launcher-ng2/ui/tmux"
)

func main() {
	core.ApplyClaudeConfigDir()
	cmd := "shell"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "--") {
		cmd = os.Args[1]
	}
	// options-commandes : ne jamais lancer le hub pour --version / --help
	// (avant : tout argument « --… » ouvrait le launcher)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "--help", "-h":
			cmd = os.Args[1]
		}
	}
	launch := false
	for _, a := range os.Args[1:] {
		if a == "--launch" {
			launch = true
		}
	}
	var err error
	switch cmd {
	case "shell":
		core.SelfUpdate()
		core.LinkUsageCache()
		core.ResetUsageBackoff()
		bar := core.StartProgress("Nettoyage des teams")
		core.TeamsProgress = bar.Step
		tmux.CleanupDeadTeams(core.LoadConfig().ResumeMaxAge())
		core.TeamsProgress = nil
		bar.Done()
		err = tmux.RunShell()
	case "nav":
		if os.Getenv("CL_SHELL") == "1" {
			// redémarré par une mise à jour : la coque peut dater d'avant le pane d'infos
			if exe, e := platform.SelfExe(); e == nil {
				_ = tmux.EnsureLayout(exe)
			}
		}
		_, err = tea.NewProgram(tmux.NewModel(launch), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
		if err != nil && os.Getenv("CL_SHELL") == "1" {
			tmux.RecordNavError(err) // le pane se ferme : garder la cause pour la relance
		}
	case "state":
		err = tmux.RunState()
	case "fixwidth":
		tmux.FixWidth()
	case "mirror": // vignette de l'onglet Teamleaders (lancée par tmux)
		if len(os.Args) != 4 {
			err = fmt.Errorf("usage : claude-launcher-ng2 mirror WIN_ID NOM")
		} else {
			err = tmux.RunMirror(os.Args[2], os.Args[3])
		}
	case "layout-watch": // mise en page d'une fenêtre projet (lancée par tmux)
		if len(os.Args) != 5 {
			err = fmt.Errorf("usage : claude-launcher-ng2 layout-watch WIN_ID LEADER_PANE DOSSIER")
		} else {
			tmux.RunLayoutWatch(os.Args[2], os.Args[3], os.Args[4])
		}
	case "relayout":
		if len(os.Args) != 3 {
			err = fmt.Errorf("usage : claude-launcher-ng2 relayout WIN_ID")
		} else {
			tmux.RunRelayout(os.Args[2])
		}
	case "ctr-attach": // fenêtre d'un projet en conteneur (lancée par tmux)
		err = tmux.CtrAttachMain(os.Args[2:])
	case "backends":
		err = tmux.RunBackends()
	case "configure":
		err = core.RunConfigure()
	case "help", "--help", "-h":
		err = tmux.RunHelp(len(os.Args) > 2 && os.Args[2] == "--wait")
	case "version", "--version", "-v":
		fmt.Println(core.NgName, core.NgVersion)
	default:
		err = fmt.Errorf("usage : claude-launcher-ng2 [shell|configure|relayout WIN_ID|help|version]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
