package main

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed bootstrap.sh
var bootstrapScript string

// bootstrapVars : variables CL_* placées en tête du script de bootstrap.
func bootstrapVars(proj string, env EnvSpec) string {
	home, _ := os.UserHomeDir()
	name := "cl"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	byMgr := func(m string) string { return strings.Join(env.PackagesByManager[m], " ") }
	setup := ""
	if len(env.Setup) > 0 {
		setup = base64.StdEncoding.EncodeToString([]byte(strings.Join(env.Setup, "\n") + "\n"))
	}
	skip := ""
	if os.Getenv("CL_CTR_SKIP_CLAUDE") == "1" { // tests : claude factice posé par setup
		skip = "1"
	}
	vars := [][2]string{
		{"CL_UID", strconv.Itoa(os.Getuid())}, {"CL_GID", strconv.Itoa(os.Getgid())},
		{"CL_USER", name}, {"CL_HOME", home}, {"CL_PROJ", proj}, {"CL_HASH", env.hash()},
		{"CL_SKIP_CLAUDE", skip}, {"CLAUDE_CONFIG_DIR", claudeDir()},
		{"CL_PKGS", strings.Join(env.Packages, " ")},
		{"CL_PKGS_APT", byMgr("apt")}, {"CL_PKGS_DNF", byMgr("dnf")},
		{"CL_PKGS_APK", byMgr("apk")}, {"CL_PKGS_ZYPPER", byMgr("zypper")},
		{"CL_SETUP", setup},
	}
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "%s=%s\n", v[0], shq(v[1]))
	}
	return b.String()
}

// bootstrap : script exécuté en root dans le conteneur (idempotent, cf. bootstrap.sh).
func (e Engine) bootstrap(name, proj string, env EnvSpec, log io.Writer) error {
	script := bootstrapVars(proj, env) + bootstrapScript
	if err := e.exec(name, true, strings.NewReader(script), log, "sh", "-s"); err != nil {
		return fmt.Errorf("bootstrap : %w", err)
	}
	return nil
}

// tmuxIn : commande tmux du projet, dans le conteneur, en utilisateur.
func tmuxIn(args ...string) []string {
	return append([]string{"tmux", "-u", "-L", ctrSocket, "-f", "/etc/cl-tmux.conf"}, args...)
}

func (e Engine) hasSession(name string) bool {
	return e.exec(name, false, nil, io.Discard, tmuxIn("has-session", "-t", "="+ctrSession)...) == nil
}

// ── ctr-attach : contenu de la fenêtre d'un projet en conteneur ───────────
//
// Lancé par launchProject dans la fenêtre du projet (hub) :
//  1. conteneur créé/démarré, bootstrap (idempotent : rapide s'il est à jour) ;
//  2. si le tmux du projet n'a pas de session : création, puis envoi du script
//     de démarrage (le même qu'en local : questions /init-project et reprise,
//     claude, marqueur de sortie propre) ;
//  3. layout-watch lancé sur le hub pour cette fenêtre ;
//  4. remplacement du processus par l'attache au tmux du projet. Si le
//     launcher (ou le hub) meurt, la session du conteneur continue : la fenêtre
//     est recréée par reattachContainers au prochain démarrage.

func runCtrAttach(proj, startFile string) error {
	b := projectBackend(proj)
	e, ok := engineOf(b)
	if !ok {
		return fmt.Errorf("%s : backend %s sans conteneur", proj, b.Name)
	}
	env, err := loadEnv(proj)
	if err != nil {
		return err
	}
	fmt.Printf("\033[1m⬡ %s — %s (%s)\033[0m\n", baseName(proj), b.Name, env.Image)
	if ps := diagnose(b); len(ps) > 0 {
		var s strings.Builder
		for _, p := range ps {
			fmt.Fprintf(&s, "\n  ⚠ %s\n    → %s", p.what, p.fix)
		}
		return fmt.Errorf("backend %s inutilisable :%s", b.Name, s.String())
	}
	name, err := e.ensure(proj, env, os.Stdout)
	if err != nil {
		return err
	}
	if err := e.bootstrap(name, proj, env, os.Stdout); err != nil {
		return err
	}
	if !e.hasSession(name) {
		// le script de démarrage est écrit par launchProject juste après la création
		// de la fenêtre : l'attendre (bootstrap déjà à jour = quelques dixièmes de s)
		var start []byte
		for i := 0; i < 50 && len(start) == 0; i++ {
			if start, _ = os.ReadFile(startFile); len(start) == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		w, h := paneSize()
		if err := e.exec(name, false, nil, os.Stdout, tmuxIn("new-session", "-d", "-s", ctrSession,
			"-x", strconv.Itoa(w), "-y", strconv.Itoa(h), "-c", proj)...); err != nil {
			return fmt.Errorf("tmux du projet : %w", err)
		}
		if len(start) > 0 {
			_ = e.exec(name, false, nil, os.Stdout, tmuxIn("send-keys", "-t", "="+ctrSession+":", string(start), "Enter")...)
		}
	}
	_ = os.Remove(startFile)
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if win, err := tout(hub, "display-message", "-p", "-t", pane, "#{window_id}"); err == nil {
			leader, _ := e.output(name, tmuxIn("display-message", "-p", "-t", "="+ctrSession+":", "#{pane_id}")...)
			_ = hub.Run("run-shell", "-b", selfCommand("layout-watch", win, leader, proj))
		}
	}
	a := e.execArgv(name, false, true, tmuxIn("attach-session", "-t", "="+ctrSession)...)
	bin, err := lookPath(a[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, a, os.Environ())
}

var lookPath = exec.LookPath

func baseName(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// paneSize : taille du pane courant (pour la première fenêtre du tmux du projet).
func paneSize() (int, int) {
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if s, err := tout(hub, "display-message", "-p", "-t", pane, "#{pane_width} #{pane_height}"); err == nil {
			if f := strings.Fields(s); len(f) == 2 {
				w, _ := strconv.Atoi(f[0])
				h, _ := strconv.Atoi(f[1])
				if w > 0 && h > 0 {
					return w, h
				}
			}
		}
	}
	return 200, 50
}

// ctrStartFile : script de démarrage d'un projet en conteneur, passé à
// ctr-attach (trop long et trop riche en guillemets pour la ligne de commande).
func ctrStartFile(proj, script string) (string, error) {
	d := cacheDir() + "/ctr-start"
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	f := fmt.Sprintf("%s/%s-%d.sh", d, ctrName(proj), time.Now().UnixNano())
	return f, os.WriteFile(f, []byte(script), 0o600)
}
