package core

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

//go:embed bootstrap.sh
var bootstrapScript string

// bootstrapVars : variables CL_* placées en tête du script de bootstrap.
func bootstrapVars(proj string, env EnvSpec) string {
	// identité et chemins vus du conteneur (pathmap.go ; Linux : ceux du launcher)
	u := CtrUser()
	home, name := u.Home, u.Name
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
		{"CL_UID", strconv.Itoa(u.UID)}, {"CL_GID", strconv.Itoa(u.GID)},
		{"CL_USER", name}, {"CL_HOME", home}, {"CL_PROJ", RemotePath(proj)}, {"CL_HASH", env.hash()},
		{"CL_SKIP_CLAUDE", skip}, {"CLAUDE_CONFIG_DIR", RemotePath(ClaudeDir())},
		{"CL_PKGS", strings.Join(env.Packages, " ")},
		{"CL_PKGS_APT", byMgr("apt")}, {"CL_PKGS_DNF", byMgr("dnf")},
		{"CL_PKGS_APK", byMgr("apk")}, {"CL_PKGS_ZYPPER", byMgr("zypper")},
		{"CL_SETUP", setup}, {"CL_SETUP_HASH", env.setupHash(proj)},
		{"CL_LOCAL", strings.Join(env.localPaths(proj), "\n")},
	}
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "%s=%s\n", v[0], Shq(v[1]))
	}
	return b.String()
}

// Bootstrap : script exécuté en root dans le conteneur (idempotent, cf. bootstrap.sh).
func (e Engine) Bootstrap(name, proj string, env EnvSpec, log io.Writer) error {
	script := bootstrapVars(proj, env) + bootstrapScript
	if err := e.Exec(name, true, strings.NewReader(script), log, "sh", "-s"); err != nil {
		return fmt.Errorf("bootstrap : %w", err)
	}
	return nil
}

// OnStart : étapes on_start de env.yaml, en utilisateur dans le projet, à chaque
// démarrage d'une session (pas au rattachement à une session vivante).
func (e Engine) OnStart(name, proj string, env EnvSpec) error {
	if len(env.OnStart) == 0 {
		return nil
	}
	fmt.Printf("\n\033[1;36m▸ %s\033[0m\n", "étapes de démarrage (on_start : launcher, template, projet)")
	script := "export PATH=\"$HOME/.local/bin:$PATH\"\ncd " + Shq(RemotePath(proj)) + "\n" + strings.Join(env.OnStart, "\n") + "\n"
	if err := e.Exec(name, false, strings.NewReader(script), os.Stdout, "bash", "-e", "-s"); err != nil {
		return fmt.Errorf("on_start : %w", err)
	}
	return nil
}

// TmuxIn : commande tmux du projet, dans le conteneur, en utilisateur.
func TmuxIn(args ...string) []string {
	return append([]string{"tmux", "-u", "-L", ctrSocket, "-f", "/etc/cl-tmux.conf"}, args...)
}

// NewCtrSession : session tmux du projet dans le conteneur, avec les chemins vus
// par le navigateur (#104) dès le premier pane (claude). tmux < 3.0 n'a pas
// -e : session créée sans, seuls les panes suivants (teammates) auront la
// variable, posée par setCtrBrowserPaths.
func (e Engine) NewCtrSession(name, proj string, w, h int) error {
	ns := []string{"new-session", "-d", "-s", CtrSession, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h), "-c", RemotePath(proj)}
	withEnv := []string{"new-session", "-d"}
	for _, v := range BrowserEnvVars() {
		withEnv = append(withEnv, "-e", v.K+"="+v.V)
	}
	withEnv = append(withEnv, ns[2:]...)
	if e.Exec(name, false, nil, io.Discard, TmuxIn(withEnv...)...) == nil {
		return nil
	}
	if err := e.Exec(name, false, nil, os.Stdout, TmuxIn(ns...)...); err != nil {
		return fmt.Errorf("tmux du projet : %w", err)
	}
	return nil
}

// SetCtrBrowserPaths : à chaque ouverture ou rattachement, correspondance à
// jour dans l'environnement de la session : les panes créés ensuite
// (teammates) la prennent, sans recréer le conteneur ni le pod (#104).
func (e Engine) SetCtrBrowserPaths(name string) {
	for _, v := range BrowserEnvVars() {
		_ = e.Exec(name, false, nil, io.Discard, TmuxIn("set-environment", "-t", "="+CtrSession, v.K, v.V)...)
	}
}

func (e Engine) HasSession(name string) bool {
	return e.Exec(name, false, nil, io.Discard, TmuxIn("has-session", "-t", "="+CtrSession)...) == nil
}

// ctrSessionGone : le projet tourne sur un backend à conteneurs dont le
// conteneur n'a plus de session tmux (ou n'existe plus). Faux si on ne sait pas
// (backend hôte, moteur injoignable) : dans le doute, pas de reprise.
func ctrSessionGone(proj string) bool {
	b := ProjectBackend(proj)
	e, ok := EngineOf(b)
	if !ok || len(DiagnoseCached(b)) > 0 {
		return false
	}
	fs, err := e.List()
	if err != nil {
		return false
	}
	for _, f := range fs {
		if f.Project == proj {
			return !f.Running || !e.Of(f).HasSession(f.Name)
		}
	}
	return true // plus de conteneur pour ce projet
}
