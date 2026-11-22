package tmux

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// État du binaire (#219) relu toutes les 10 s : annonce « relancer » de la coque.
type launcherStateMsg core.LauncherInfo

type relaunchDoneMsg struct{ text string }

func launcherStateCmd() tea.Cmd {
	return func() tea.Msg { return launcherStateMsg(core.LauncherState()) }
}

func launcherTickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return launcherStateMsg(core.LauncherState()) })
}

func (m model) relaunchWanted() bool {
	return m.shell && (m.launcher.BinChanged || len(m.launcher.Stale) > 0)
}

// relaunchCmd : relance la coque ; le navigateur étant relancé en dernier, cette commande ne revient en général pas.
func relaunchCmd() tea.Cmd {
	return func() tea.Msg {
		res, err := RelaunchShell()
		if err != nil {
			return relaunchDoneMsg{"⚠ relance : " + err.Error()}
		}
		return relaunchDoneMsg{"↻ " + res.String()}
	}
}
