package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Nom de la session intérieure ; CL_HUB permet une répétition sans toucher à claude-hub.
var hubSession = func() string {
	if h := os.Getenv("CL_HUB"); h != "" {
		return h
	}
	return "claude-hub"
}()

// targetSession : le launcher bash vise toujours claude-hub (les sessions
// groupées partagent ses fenêtres) ; à défaut, la session courante.
func targetSession(current string) string {
	if dtmux("has-session", "-t", hubSession).Run() == nil {
		return hubSession
	}
	return current
}

func scriptPath() string {
	if p := os.Getenv("CL_SCRIPT"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "claude-launcher.sh")
}

// claudeBin : commande lancée dans chaque projet (CL_CLAUDE_BIN : remplaçant de test).
func claudeBin() string {
	if b := os.Getenv("CL_CLAUDE_BIN"); b != "" {
		return shq(b)
	}
	return "claude"
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func tmuxOut(args ...string) (string, error) {
	b, err := dtmux(args...).Output()
	return strings.TrimSpace(string(b)), err
}

// exports : équivalent de build_claude_exports.
func (c Config) exports() string {
	var e strings.Builder
	if c.GithubToken != "" {
		fmt.Fprintf(&e, "\nexport GITHUB_PERSONAL_ACCESS_TOKEN=%q\nexport GH_TOKEN=%q", c.GithubToken, c.GithubToken)
	}
	if c.DisableMouse {
		e.WriteString("\nexport CLAUDE_CODE_DISABLE_MOUSE=1")
	}
	if c.ExpTeams {
		e.WriteString("\nexport CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1")
	}
	for _, v := range c.ExtraEnvs {
		if v != "" {
			e.WriteString("\nexport " + v)
		}
	}
	return e.String()
}

// launchProject reproduit la branche « ouvrir un projet » du menu bash :
// fenêtre, styles, couleur du pane, démarrage de claude (avec la confirmation
// /init-project si le template est en retard ou absent), puis le watcher de
// layout — qui reste le script bash, pas encore migré.
func launchProject(cfg Config, session string, n Node, latest string) error {
	sess := targetSession(session)
	if tmuxWindows(sess)[n.Key] {
		return dtmux("select-window", "-t", sess+":"+n.Key).Run()
	}
	ids, err := tmuxOut("new-window", "-P", "-F", "#{window_id} #{pane_id}", "-t", sess, "-n", n.Key)
	if err != nil {
		return fmt.Errorf("new-window : %w", err)
	}
	f := strings.Fields(ids)
	if len(f) != 2 {
		return fmt.Errorf("new-window : sortie inattendue %q", ids)
	}
	winID, pane := f[0], f[1]
	target := sess + ":" + n.Key

	_ = dtmux("set-window-option", "-t", target, "window-status-style", "fg=colour242,bg=colour236").Run()
	_ = dtmux("set-window-option", "-t", target, "window-status-current-style", "bold,fg=colour255,bg=colour27").Run()
	_ = dtmux("set-option", "-p", "-t", pane, "window-style", "bg=colour"+cfg.projectColor(n.Key)).Run()

	script := scriptPath()
	initCache := filepath.Join(filepath.Dir(script), "init-project.md")

	var initPre, initArg string
	switch readTemplate(n.Path, latest) {
	case TplOutdated:
		initPre = `_ip_arg=''; read -rp '↑ template en retard — lancer /init-project ? [o/N] ' _r; [[ "$_r" == [oOyY]* ]] && _ip_arg='/init-project'; `
		initArg = " $_ip_arg"
	case TplMissing:
		initPre = `_ip_arg='/init-project'; read -rp '🗑 fichiers template manquants (clone frais) — lancer /init-project pour restaurer ? [O/n] ' _r; [[ "$_r" == [nN]* ]] && _ip_arg=''; `
		initArg = " $_ip_arg"
	}

	keys := fmt.Sprintf("cd %s%s\nmkdir -p .claude/commands\n_ip=.claude/commands/init-project.md\n"+
		"[[ -s %s ]] && cp %s \"$_ip\" || echo '⚠  init-project.md non disponible — relancer le launcher connecté'\n"+
		"%s%s%s",
		shq(n.Path), cfg.exports(), shq(initCache), shq(initCache), initPre, claudeBin()+" "+cfg.ClaudeOptions, initArg)
	if err := dtmux("send-keys", "-t", target, keys, "Enter").Run(); err != nil {
		return fmt.Errorf("send-keys : %w", err)
	}

	watch := fmt.Sprintf("bash %s --layout-watch %s %s %s %s %s",
		shq(script), shq(sess), shq(winID), shq(pane), shq(n.Path), shq(script))
	_ = dtmux("run-shell", "-b", watch).Run()
	return dtmux("select-window", "-t", target).Run()
}
