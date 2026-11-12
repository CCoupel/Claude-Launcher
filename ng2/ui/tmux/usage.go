package tmux

// openLogin ouvre dans la session des projets une fenêtre « login » qui lance
// claude /login ; elle se ferme quand on quitte claude.
func openLogin(session string) error {
	sess := targetSession(session)
	if tmuxWindows(sess)["login"] {
		return dtmux("select-window", "-t", exactW(sess, "login")).Run()
	}
	return dtmux("new-window", "-t", exactS(sess)+":", "-n", "login", claudeBin()+" /login").Run()
}
