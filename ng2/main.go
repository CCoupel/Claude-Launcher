// Draft Go du menu de claude-launcher : même sources de données que le bash
// (conf, arbre de projets, badges git/template, consommation Claude, fenêtres
// tmux), mais rafraîchies en parallèle sans jamais bloquer l'affichage.
package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
	"claude-launcher-ng2/ui/desktop"
	"claude-launcher-ng2/ui/tmux"
)

// windowsUnsupported : sous-commandes de la coque tmux (sans objet sous Windows).
var windowsUnsupported = map[string]bool{"shell": true, "nav": true, "state": true, "fixwidth": true,
	"mirror": true, "layout-watch": true, "relayout": true, "ctr-attach": true}

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
	platform.CleanupOldExe() // Windows : .old laissé par une mise à jour (ailleurs : rien)
	if runtime.GOOS == "windows" && cmd == "shell" {
		cmd = "desktop" // Windows : sans argument, l'interface native (#111)
	}
	if runtime.GOOS == "windows" && !(cmd == "desktop" && !slices.Contains(os.Args, "--web")) {
		// exécutable graphique : en ligne de commande, la sortie va à la console
		// de l'invite (la fenêtre, elle, n'en a pas : outils lancés sans console,
		// platform.NoWindow)
		platform.AttachParentConsole()
	}
	if runtime.GOOS == "windows" && windowsUnsupported[cmd] {
		fmt.Fprintf(os.Stderr, "« %s » : coque tmux indisponible sous Windows (interface native : #111) — disponibles : backends, configure, help, version\n", cmd)
		os.Exit(1)
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
		if startupUpdate(cmd) {
			core.SelfUpdate()
		}
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
	case "desktop": // interface native (Wails), ou web (--web) ; défaut sous Windows
		// pas de mise à jour bloquante avant la fenêtre : l'annonce du bandeau
		// (orange → jaune → vert) la prépare, un clic l'installe (startupUpdate)
		core.LinkUsageCache()
		core.ResetUsageBackoff()
		var args []string
		if len(os.Args) > 1 && os.Args[1] == "desktop" {
			args = os.Args[2:]
		} else {
			args = os.Args[1:]
		}
		err = desktop.Run(args)
	case "backends":
		err = tmux.RunBackends()
	case "configure":
		err = core.RunConfigure()
	case "help", "--help", "-h":
		err = tmux.RunHelp(len(os.Args) > 2 && os.Args[2] == "--wait")
	case "version", "--version", "-v":
		fmt.Println(core.NgName, core.NgVersion)
	default:
		err = fmt.Errorf("usage : claude-launcher-ng2 [shell|desktop|configure|relayout WIN_ID|help|version]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// startupUpdate : mise à jour au lancement, avant l'interface (coque tmux :
// oui ; fenêtre native : non, elle passe par son bandeau sans faire attendre
// une fenêtre vide, console cachée).
func startupUpdate(cmd string) bool { return cmd != "desktop" }
