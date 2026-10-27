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
	if dtmux("has-session", "-t", exactS(hubSession)).Run() == nil {
		return hubSession
	}
	return current
}

// exactS / exactW : cibles tmux exactes. Sans « = », tmux accepte un préfixe :
// « -t claude-hub » viserait claude-hub-test si claude-hub n'existe pas.
func exactS(s string) string    { return "=" + s }
func exactW(s, w string) string { return "=" + s + ":=" + w }

// scriptPath : ~/claude-launcher.sh, réduit depuis la v4 à une redirection de
// l'ancienne commande vers le NG (tenue à jour par la mise à jour automatique).
func scriptPath() string {
	if p := os.Getenv("CL_SCRIPT"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "claude-launcher.sh")
}

// selfCommand : ligne de commande d'une sous-commande du NG lancée par tmux
// (vignette, watcher), avec le serveur tmux et la session visés.
func selfCommand(args ...string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "claude-launcher-ng"
	}
	c := "CL_HUB=" + shq(hubSession) + " CL_INNER_SOCKET=" + shq(os.Getenv("CL_INNER_SOCKET")) +
		" " + shq(strings.TrimSuffix(exe, " (deleted)"))
	for _, a := range args {
		c += " " + shq(a)
	}
	return c
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

// launchProject : fenêtre, styles, couleur du pane, démarrage de claude (avec la
// confirmation /init-project si le template est en retard ou absent), puis le
// watcher de mise en page (layout.go).
// Mode de lancement : normal (ouverture d'un projet), ou juste après sa création
// depuis le launcher — géré (/init-project d'office) ou brut (ni git ni init-project).
type launchMode int

const (
	launchNormal launchMode = iota
	launchNewManaged
	launchNewRaw
)

func launchProject(cfg Config, session string, n Node, latest string, mode launchMode) error {
	sess := targetSession(session)
	if tmuxWindows(sess)[n.Key] {
		return dtmux("select-window", "-t", exactW(sess, n.Key)).Run()
	}
	ids, err := tmuxOut("new-window", "-P", "-F", "#{window_id} #{pane_id}", "-t", exactS(sess)+":", "-n", n.Key)
	if err != nil {
		return fmt.Errorf("new-window : %w", err)
	}
	f := strings.Fields(ids)
	if len(f) != 2 {
		return fmt.Errorf("new-window : sortie inattendue %q", ids)
	}
	winID, pane := f[0], f[1]
	target := exactW(sess, n.Key)

	_ = dtmux("set-window-option", "-t", target, "window-status-style", "fg=colour242,bg=colour236").Run()
	_ = dtmux("set-window-option", "-t", target, "window-status-current-style", "bold,fg=colour255,bg=colour27").Run()
	_ = dtmux("set-option", "-p", "-t", pane, "window-style", "bg=colour"+cfg.projectColor(n.Key)).Run()

	initCache := initProjectPath()

	var initPre, initArg string
	switch tpl := readTemplate(n.Path, latest); {
	case mode == launchNewManaged:
		initArg = " /init-project" // projet tout juste créé : pas de question
	case mode == launchNewRaw: // ni question ni /init-project
	case tpl == TplOutdated:
		initPre = `_ip_arg=''; read -rp '↑ template en retard — lancer /init-project ? [o/N] ' _r; [[ "$_r" == [oOyY]* ]] && _ip_arg='/init-project'; `
		initArg = " $_ip_arg"
	case tpl == TplMissing:
		initPre = `_ip_arg='/init-project'; read -rp '🗑 fichiers template manquants (clone frais) — lancer /init-project pour restaurer ? [O/n] ' _r; [[ "$_r" == [nN]* ]] && _ip_arg=''; `
		initArg = " $_ip_arg"
	}

	// init-project.md est déposé dans le projet, sauf pour un projet brut tout juste créé
	copyInit := fmt.Sprintf("mkdir -p .claude/commands\n_ip=.claude/commands/init-project.md\n"+
		"[[ -s %s ]] && cp %s \"$_ip\" || echo '⚠  init-project.md non disponible — relancer le launcher connecté'\n",
		shq(initCache), shq(initCache))
	if mode == launchNewRaw {
		copyInit = ""
	}
	keys := fmt.Sprintf("cd %s%s\n%s%s%s%s",
		shq(n.Path), cfg.exports(), copyInit, initPre, claudeBin()+" "+cfg.ClaudeOptions, initArg)
	if err := dtmux("send-keys", "-t", target, keys, "Enter").Run(); err != nil {
		return fmt.Errorf("send-keys : %w", err)
	}

	_ = dtmux("run-shell", "-b", selfCommand("layout-watch", winID, pane, n.Path)).Run()
	return dtmux("select-window", "-t", target).Run()
}
