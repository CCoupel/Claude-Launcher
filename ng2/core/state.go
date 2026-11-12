package core

type PaneInfo struct{ ID, PID, Cmd string }

func OrNone(s string) string {
	if s == "" {
		return Dim.Render("aucune")
	}
	return s
}
