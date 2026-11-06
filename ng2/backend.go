package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Backends d'exécution des projets (#43) ────────────────────────────────
//
// Trois niveaux :
//   - l'environnement du projet : <projet>/.claude-launcher/env.yaml, versionné
//     (image, paquets, étapes de setup, montages en plus) ;
//   - les backends de la machine : ~/.config/claude-launcher-ng2/backends.yaml
//     (host toujours présent ; podman / docker détectés s'il n'y a pas de fichier) ;
//   - l'affectation projet → backend : état du launcher, choisie dans le
//     navigateur (F2), ~/.local/state/claude-launcher-ng2/projects.json.
//
// Un projet en conteneur vit dans SON conteneur (un projet = un conteneur), dans
// un tmux à lui (socket ctrSocket, session ctrSession), attaché dans sa fenêtre
// du hub ; le launcher pilote ce tmux par son canal de contrôle (ctl.go).

const (
	ctrSocket  = "cl" // socket tmux dans le conteneur
	ctrSession = "cl" // session du projet dans ce tmux
	envFile    = ".claude-launcher/env.yaml"
)

// EnvSpec : <projet>/.claude-launcher/env.yaml.
type EnvSpec struct {
	Image             string              `yaml:"image"`
	Packages          []string            `yaml:"packages"`
	PackagesByManager map[string][]string `yaml:"packages_by_manager"`
	Setup             []string            `yaml:"setup"`
	Mounts            []EnvMount          `yaml:"mounts"`
	Prefer            string              `yaml:"prefer"`
	raw               []byte
}

type EnvMount struct {
	Path string `yaml:"path"`
	RO   *bool  `yaml:"ro"` // lecture seule par défaut
}

func envPath(proj string) string { return filepath.Join(proj, envFile) }

func hasEnv(proj string) bool {
	_, err := os.Stat(envPath(proj))
	return err == nil
}

func loadEnv(proj string) (EnvSpec, error) {
	var e EnvSpec
	b, err := os.ReadFile(envPath(proj))
	if err != nil {
		return e, err
	}
	if err := yaml.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("%s : %w", envFile, err)
	}
	if e.Image == "" {
		return e, fmt.Errorf("%s : « image » manquante", envFile)
	}
	e.raw = b
	return e, nil
}

// hash : empreinte de l'environnement + du script de bootstrap (rejoué si elle change).
func (e EnvSpec) hash() string {
	h := sha256.New()
	h.Write(e.raw)
	h.Write([]byte(bootstrapScript))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ── Config des backends ───────────────────────────────────────────────────

type BackendConf struct {
	Name    string   `yaml:"-"`
	Type    string   `yaml:"type"`        // host | podman | docker | ssh
	Context string   `yaml:"context"`     // docker : --context
	Host    string   `yaml:"host"`        // docker / podman : adresse du démon ou du service (tcp://…) ; ssh : destination ssh
	Engine  string   `yaml:"engine"`      // ssh : moteur sur la machine distante, podman (défaut) ou docker
	EngHost string   `yaml:"engine_host"` // ssh : adresse du démon côté machine distante (docker rootless, service podman…)
	Shared  []string `yaml:"shared"`      // distant : racines du stockage partagé (mêmes chemins des deux côtés)
}

// engine : moteur réellement piloté (podman ou docker).
func (b BackendConf) engine() string {
	if b.Type == "ssh" {
		if b.Engine == "" {
			return "podman"
		}
		return b.Engine
	}
	return b.Type
}

// remote : le conteneur tourne sur une autre machine (démon docker distant, ou
// podman joint par ssh). Ses montages y sont résolus : ~/.claude et le projet
// doivent être sur le stockage partagé, aux mêmes chemins.
func (b BackendConf) remote() bool {
	return b.Type == "ssh" || (b.Type == "docker" || b.Type == "podman") && b.Host != "" && !strings.HasPrefix(b.Host, "unix://")
}

type backendsFile struct {
	Backends map[string]BackendConf `yaml:"backends"`
	Defaults struct {
		Backend string `yaml:"backend"`
	} `yaml:"defaults"`
}

func ng2ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-launcher-ng2")
}

func backendsPath() string { return filepath.Join(ng2ConfigDir(), "backends.yaml") }

// loadBackends : backends déclarés, triés (host en premier). Sans fichier :
// host + les moteurs de conteneurs trouvés dans le PATH.
func loadBackends() ([]BackendConf, error) {
	var f backendsFile
	b, err := os.ReadFile(backendsPath())
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("backends.yaml : %w", err)
		}
	case os.IsNotExist(err):
		f.Backends = map[string]BackendConf{}
		for _, t := range detectedEngines() {
			f.Backends[t] = BackendConf{Type: t}
		}
	default:
		return nil, err
	}
	if f.Backends == nil {
		f.Backends = map[string]BackendConf{}
	}
	f.Backends["host"] = BackendConf{Type: "host"}
	var out []BackendConf
	for n, c := range f.Backends {
		c.Name = n
		switch c.Type {
		case "host", "podman", "docker":
		case "ssh":
			if c.Host == "" {
				return nil, fmt.Errorf("backends.yaml : backend ssh %q sans « host » (destination ssh)", n)
			}
			if c.Engine != "" && c.Engine != "podman" && c.Engine != "docker" {
				return nil, fmt.Errorf("backends.yaml : backend ssh %q : engine %q (podman ou docker)", n, c.Engine)
			}
		default:
			return nil, fmt.Errorf("backends.yaml : backend %q de type %q non géré (host, podman, docker, ssh)", n, c.Type)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == "host") != (out[j].Name == "host") {
			return out[i].Name == "host"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// detectedEngines : moteurs du PATH qui répondent (un « docker » de Docker
// Desktop sans intégration WSL est présent mais inutilisable). Une fois par processus.
var detectedEngines = sync.OnceValue(func() []string {
	var out []string
	for _, t := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(t); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		err := exec.CommandContext(ctx, t, "version", "--format", "{{.Client.Version}}").Run()
		cancel()
		if err == nil {
			out = append(out, t)
		}
	}
	return out
})

func findBackend(name string) (BackendConf, bool) {
	bs, err := loadBackends()
	if err != nil {
		return BackendConf{}, false
	}
	for _, b := range bs {
		if b.Name == name {
			return b, true
		}
	}
	return BackendConf{}, false
}

// ── Affectation projet → backend ──────────────────────────────────────────

func ng2StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claude-launcher-ng2")
}

func assignPath() string { return filepath.Join(ng2StateDir(), "projects.json") }

func readAssignments() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(assignPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func writeAssignment(proj, backend string) error {
	m := readAssignments()
	if backend == "" || backend == "host" {
		delete(m, proj)
	} else {
		m[proj] = backend
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.MkdirAll(ng2StateDir(), 0o755); err != nil {
		return err
	}
	tmp := assignPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, assignPath())
}

// projectBackend : backend affecté au projet ; host sans env.yaml ou sans
// affectation valide. Une affectation absente reprend « prefer » d'env.yaml.
func projectBackend(proj string) BackendConf {
	host := BackendConf{Name: "host", Type: "host"}
	if !hasEnv(proj) {
		return host
	}
	name, ok := readAssignments()[proj]
	if !ok {
		if e, err := loadEnv(proj); err == nil && e.Prefer != "" {
			name = e.Prefer
		}
	}
	if b, ok := findBackend(name); ok {
		return b
	}
	return host
}

// ── Moteur de conteneurs (podman / docker) ────────────────────────────────

// Engine : un moteur de conteneurs : podman local, docker (socket local ou démon
// distant en accès direct), ou podman d'une autre machine joint par ssh. Le conteneur porte les labels
// cl.launcher=ng2, cl.project, cl.backend, cl.image : c'est par eux que le
// launcher retrouve ses projets au redémarrage (List).
type Engine struct{ b BackendConf }

func engineOf(b BackendConf) (Engine, bool) {
	switch b.Type {
	case "podman", "docker", "ssh":
		return Engine{b}, true
	}
	return Engine{}, false
}

// podman : moteur podman (direct ou par ssh) — utilisateur gardé (keep-id).
func (e Engine) podman() bool { return e.b.engine() == "podman" }

// engineArgv : client du moteur et adresse de son démon / service.
func engineArgv(engine, context, host string) []string {
	out := []string{engine}
	switch {
	case engine == "docker" && context != "":
		out = append(out, "--context", context)
	case engine == "docker" && host != "":
		out = append(out, "-H", host)
	case engine == "podman" && host != "":
		out = append(out, "--url", host)
	}
	return out
}

func (e Engine) cli(args ...string) []string { return e.cliT(false, args...) }

// cliT : ligne de commande du moteur ; tty : terminal demandé à ssh (attache).
// Par ssh, la commande distante est une seule chaîne passée au shell distant :
// chaque argument y est protégé.
func (e Engine) cliT(tty bool, args ...string) []string {
	switch e.b.Type {
	case "ssh":
		out := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
		if tty {
			out = append(out, "-t")
		}
		rem := append(engineArgv(e.b.engine(), "", e.b.EngHost), args...)
		q := make([]string, len(rem))
		for i, a := range rem {
			q[i] = shq(a)
		}
		return append(out, e.b.Host, strings.Join(q, " "))
	}
	return append(engineArgv(e.b.Type, e.b.Context, e.b.Host), args...)
}

func (e Engine) cmd(args ...string) *exec.Cmd {
	a := e.cli(args...)
	return exec.Command(a[0], a[1:]...)
}

// ctrName : nom (et hostname) du conteneur d'un projet : lisible + empreinte du chemin.
func ctrName(proj string) string {
	base := strings.ToLower(filepath.Base(proj))
	var b strings.Builder
	for _, r := range base {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	n := strings.Trim(b.String(), "-")
	if len(n) > 30 {
		n = n[:30]
	}
	h := sha256.Sum256([]byte(proj))
	return "cl-" + n + "-" + hex.EncodeToString(h[:])[:8]
}

// state : "running", "exited", "created"… ou "" si le conteneur n'existe pas.
func (e Engine) state(name string) (status, image string) {
	b, err := e.cmd("inspect", "-f", `{{.State.Status}}|{{index .Config.Labels "cl.image"}}`, name).Output()
	if err != nil {
		return "", ""
	}
	status, image, _ = strings.Cut(strings.TrimSpace(string(b)), "|")
	return status, image
}

// pid1 : processus principal qui ignore tout sauf SIGTERM (arrêt propre, sans --init).
const pid1 = `trap 'exit 0' TERM INT; while :; do sleep 3600 & wait $!; done`

// ensure : conteneur du projet créé (ou recréé si l'image a changé) et démarré.
func (e Engine) ensure(proj string, env EnvSpec, log io.Writer) (string, error) {
	name := ctrName(proj)
	status, image := e.state(name)
	if status != "" && image != env.Image {
		fmt.Fprintf(log, "  image changée (%s → %s) : conteneur recréé\n", image, env.Image)
		_ = e.cmd("rm", "-f", name).Run()
		status = ""
	}
	switch status {
	case "running":
		return name, nil
	case "":
		args, err := e.runArgs(name, proj, env)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(log, "  création du conteneur %s (%s)…\n", name, env.Image)
		var out bytes.Buffer
		c := e.cmd(args...)
		c.Stdout, c.Stderr = io.MultiWriter(log, &out), io.MultiWriter(log, &out)
		if err := c.Run(); err != nil {
			if s := out.String(); strings.Contains(s, "bind source path does not exist") || strings.Contains(s, "no such file or directory") {
				return "", fmt.Errorf("création du conteneur : un dossier monté manque sur la machine du moteur — stockage partagé non monté au même chemin (%s, %s)", claudeDir(), proj)
			}
			return "", fmt.Errorf("création du conteneur : %w", err)
		}
	default:
		fmt.Fprintf(log, "  démarrage du conteneur %s…\n", name)
		if out, err := e.cmd("start", name).CombinedOutput(); err != nil {
			return "", fmt.Errorf("démarrage : %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return name, nil
}

func (e Engine) runArgs(name, proj string, env EnvSpec) ([]string, error) {
	home, _ := os.UserHomeDir()
	cd := claudeDir()
	if err := ensureSharedConfig(); err != nil {
		return nil, err
	}
	if err := e.checkShared(proj); err != nil {
		return nil, err
	}
	// hostname : « cl- » conteneur local, « clx- » distant — pidSpace.go en déduit
	// si l'absence du namespace prouve la mort du leader (local) ou rien (distant)
	host := name
	if e.b.remote() {
		host = "clx-" + strings.TrimPrefix(name, "cl-")
	}
	// --mount (et non -v) : échoue si le dossier manque sur la machine du moteur,
	// au lieu d'y créer un dossier vide (stockage partagé non monté)
	bind := func(p string, ro bool) string {
		m := "type=bind,src=" + p + ",dst=" + p
		if ro {
			m += ",readonly"
		}
		return m
	}
	args := []string{"run", "-d", "--name", name, "--hostname", host,
		"--label", "cl.launcher=ng2", "--label", "cl.project=" + proj,
		"--label", "cl.backend=" + e.b.Name, "--label", "cl.image=" + env.Image,
		"--mount", bind(cd, false), "--mount", bind(proj, false), "-w", proj,
		"-e", "HOME=" + home, "-e", "CLAUDE_CONFIG_DIR=" + cd, "-e", "LANG=C.UTF-8",
		"-e", "DISABLE_AUTOUPDATER=1"}
	if e.podman() {
		args = append(args, "--userns=keep-id", "--user", "0")
	}
	for _, m := range env.Mounts {
		p := m.Path
		if !filepath.IsAbs(p) {
			p = filepath.Clean(filepath.Join(proj, p))
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("montage %s : %w", m.Path, err)
		}
		if e.b.remote() && !underShared(e.b, p) {
			return nil, fmt.Errorf("montage %s : hors du stockage partagé du backend %s (%s)", p, e.b.Name, strings.Join(e.b.Shared, ", "))
		}
		args = append(args, "--mount", bind(p, m.RO == nil || *m.RO))
	}
	return append(args, env.Image, "sh", "-c", pid1), nil
}

// ensureSharedConfig : claude lancé avec CLAUDE_CONFIG_DIR=~/.claude range sa
// config dans ~/.claude/.claude.json (un fichier monté seul se détache : il est
// réécrit par remplacement). Première fois : COPIE de ~/.claude.json, jamais
// déplacée — les sessions de l'hôte (launcher v4 compris) gardent la leur.
func ensureSharedConfig() error {
	dst := filepath.Join(claudeDir(), ".claude.json")
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return nil // pas de config à reprendre : claude en créera une
	}
	if err := os.MkdirAll(claudeDir(), 0o755); err != nil {
		return err
	}
	tmp := dst + ".cl-tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// execArgv : commande exécutée dans le conteneur ; root : en root, sinon en
// utilisateur de l'hôte (uid:gid) ; tty : terminal interactif (attach).
func (e Engine) execArgv(name string, root, tty bool, argv ...string) []string {
	if tty {
		// attache : TERM et LANG passés au conteneur, terminal demandé à ssh
		a := []string{"exec", "-i", "-t", "-e", "TERM=tmux-256color", "-e", "LANG=C.UTF-8"}
		if root {
			a = append(a, "-u", "0")
		} else {
			a = append(a, "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
		}
		return e.cliT(true, append(append(a, name), argv...)...)
	}
	a := []string{"exec", "-i"}
	if tty {
		a = append(a, "-t", "-e", "TERM=tmux-256color")
	}
	if root {
		a = append(a, "-u", "0")
	} else {
		a = append(a, "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	return e.cli(append(append(a, name), argv...)...)
}

func (e Engine) exec(name string, root bool, stdin io.Reader, out io.Writer, argv ...string) error {
	a := e.execArgv(name, root, false, argv...)
	c := exec.Command(a[0], a[1:]...)
	c.Stdin, c.Stdout, c.Stderr = stdin, out, out
	return c.Run()
}

func (e Engine) output(name string, argv ...string) (string, error) {
	var b bytes.Buffer
	a := e.execArgv(name, false, false, argv...)
	c := exec.Command(a[0], a[1:]...)
	c.Stdout = &b
	err := c.Run()
	return strings.TrimSpace(b.String()), err
}

// Found : conteneur d'un projet trouvé sur un backend.
type Found struct {
	Backend string
	Name    string
	Project string
	Running bool
}

// list : conteneurs NG2 du moteur (par label ; labels relus par inspect, même
// format pour podman et docker).
func (e Engine) list() ([]Found, error) {
	// filtre aussi par backend : deux backends peuvent viser le même moteur
	b, err := e.cmd("ps", "-a", "-q", "--filter", "label=cl.launcher=ng2", "--filter", "label=cl.backend="+e.b.Name).Output()
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return nil, nil
	}
	b, err = e.cmd(append([]string{"inspect", "-f",
		`{{.Name}}|{{.State.Status}}|{{index .Config.Labels "cl.project"}}`}, ids...)...).Output()
	if err != nil {
		return nil, err
	}
	var out []Found
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) != 3 || f[2] == "" {
			continue
		}
		out = append(out, Found{Backend: e.b.Name, Name: strings.TrimPrefix(f[0], "/"), Project: f[2], Running: f[1] == "running"})
	}
	return out, nil
}
