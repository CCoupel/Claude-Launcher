package main

import (
	"bufio"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
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
		{"CL_SETUP", setup}, {"CL_SETUP_HASH", env.setupHash(proj)},
		{"CL_LOCAL", strings.Join(env.localPaths(proj), "\n")},
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

// onStart : étapes on_start de env.yaml, en utilisateur dans le projet, à chaque
// démarrage d'une session (pas au rattachement à une session vivante).
func (e Engine) onStart(name, proj string, env EnvSpec) error {
	if len(env.OnStart) == 0 {
		return nil
	}
	fmt.Printf("\n\033[1;36m▸ %s\033[0m\n", "étapes de démarrage (env.yaml : on_start)")
	script := "export PATH=\"$HOME/.local/bin:$PATH\"\ncd " + shq(proj) + "\n" + strings.Join(env.OnStart, "\n") + "\n"
	if err := e.exec(name, false, strings.NewReader(script), os.Stdout, "bash", "-e", "-s"); err != nil {
		return fmt.Errorf("on_start : %w", err)
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
	env, err := loadEnvFull(proj)
	if err != nil {
		return err
	}
	fmt.Printf("\033[1m⬡ %s — %s (%s)\033[0m\n", baseName(proj), b.Name, env.Image)
	if _, err := os.Stat(launcherEnvPath()); err == nil {
		fmt.Printf("  + bootstrap du launcher : %s\n", launcherEnvPath())
	}
	if len(env.Local) > 0 {
		fmt.Printf("  dossiers privés au conteneur : %s\n", strings.Join(env.Local, ", "))
	}
	if ps := diagnose(b); len(ps) > 0 {
		var s strings.Builder
		for _, p := range ps {
			fmt.Fprintf(&s, "\n  ⚠ %s\n    → %s", p.what, p.fix)
		}
		return fmt.Errorf("backend %s inutilisable :%s", b.Name, s.String())
	}
	// groupe de serveurs (#61) : serveur du projet, choisi sous verrou s'il est nouveau
	e, unlock, err := e.place(proj, asker{os.Stdout, bufio.NewReader(os.Stdin)})
	if err != nil {
		return err
	}
	name, err := e.ensure(proj, env, os.Stdout)
	unlock()
	if err != nil {
		return err
	}
	if err := e.bootstrap(name, proj, env, os.Stdout); err != nil {
		return err
	}
	if l, ok := e.readLive(name); ok && !l.claude {
		// tmux resté seul après un /exit (point bleu) : la session est recréée
		// pour que claude redémarre, au lieu de se rattacher à son shell
		fmt.Println("  claude n'est plus en marche dans la session : redémarrage")
		_ = e.exec(name, false, nil, io.Discard, tmuxIn("kill-session", "-t", "="+ctrSession)...)
	}
	if !e.hasSession(name) {
		if err := e.onStart(name, proj, env); err != nil {
			return err
		}
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
	// propriétaire (#48) : un seul launcher pilote la session
	readOnly := false
	if o := e.owner(name); o.alive() && o.id != launcherID() {
		switch ownerChoice(o) {
		case "l":
			readOnly = true
		case "p":
			e.takeOver(name)
		default:
			return nil
		}
	} else {
		e.claimFree(name)
	}
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		if win, err := tout(hub, "display-message", "-p", "-t", pane, "#{window_id}"); err == nil {
			_ = hub.Run("set-window-option", "-t", win, "@cl_server", e.b.Server)
			if readOnly {
				_ = hub.Run("set-window-option", "-t", win, "@cl_readonly", "1")
			} else {
				leader, _ := e.output(name, tmuxIn("display-message", "-p", "-t", "="+ctrSession+":", "#{pane_id}")...)
				_ = hub.Run("run-shell", "-b", selfCommand("layout-watch", win, leader, proj))
			}
		}
	}
	att := []string{"attach-session", "-t", "=" + ctrSession}
	if readOnly {
		att = append(att, "-r")
	}
	a := e.execArgv(name, false, true, tmuxIn(att...)...)
	bin, err := lookPath(a[0])
	if err != nil {
		return err
	}
	if !loadConfig().CtrStopOnExit || readOnly {
		return syscall.Exec(bin, a, os.Environ())
	}
	// CTR_STOP_ON_EXIT : l'attache reste un enfant ; quand elle rend la main et que
	// la session n'existe plus (claude sorti proprement, ou fermeture), le
	// conteneur est arrêté. Simple détachement : la session vit, rien n'est arrêté.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
	c := exec.Command(bin, a[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
	if !e.hasSession(name) {
		fmt.Println("\n⏻ claude terminé : arrêt du conteneur (CTR_STOP_ON_EXIT)")
		return e.stop(proj, name)
	}
	return nil
}

var lookPath = exec.LookPath

// ctrSessionGone : le projet tourne sur un backend à conteneurs dont le
// conteneur n'a plus de session tmux (ou n'existe plus). Faux si on ne sait pas
// (backend hôte, moteur injoignable) : dans le doute, pas de reprise.
func ctrSessionGone(proj string) bool {
	b := projectBackend(proj)
	e, ok := engineOf(b)
	if !ok || len(diagnoseCached(b)) > 0 {
		return false
	}
	fs, err := e.list()
	if err != nil {
		return false
	}
	for _, f := range fs {
		if f.Project == proj {
			return !f.Running || !e.of(f).hasSession(f.Name)
		}
	}
	return true // plus de conteneur pour ce projet
}

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
