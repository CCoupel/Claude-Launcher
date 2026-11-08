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
	"strconv"
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
	Watch             []string            `yaml:"watch"`    // fichiers dont le contenu change → setup rejoué
	OnStart           []string            `yaml:"on_start"` // étapes rejouées à chaque démarrage du projet
	Local             []string            `yaml:"local"`    // dossiers du projet privés au conteneur (masqués)
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
	for i, l := range e.Local {
		c, err := cleanRel(l)
		if err != nil {
			return e, fmt.Errorf("%s : local « %s » : %w", envFile, l, err)
		}
		e.Local[i] = c
	}
	for i, w := range e.Watch {
		c, err := cleanRel(w)
		if err != nil {
			return e, fmt.Errorf("%s : watch « %s » : %w", envFile, w, err)
		}
		e.Watch[i] = c
	}
	e.raw = b
	return e, nil
}

// ── Bootstrap du launcher (#57) ───────────────────────────────────────────
//
// <config>/env.yaml, à côté de backends.yaml : l'outillage de l'utilisateur
// (statusline, hooks…) installé dans TOUS ses conteneurs, en plus du env.yaml de
// chaque projet. Clés : packages, packages_by_manager, setup, on_start — celles
// qui ne concernent pas un projet en particulier.

func launcherEnvPath() string { return filepath.Join(ng2ConfigDir(), "env.yaml") }

var launcherEnvKeys = map[string]bool{"packages": true, "packages_by_manager": true, "setup": true, "on_start": true}

// loadLauncherEnv : env.yaml du launcher ; absent = vide.
func loadLauncherEnv() (EnvSpec, error) {
	var e EnvSpec
	b, err := os.ReadFile(launcherEnvPath())
	if os.IsNotExist(err) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	var keys map[string]any
	if err := yaml.Unmarshal(b, &keys); err != nil {
		return e, fmt.Errorf("%s : %w", launcherEnvPath(), err)
	}
	for k := range keys {
		if !launcherEnvKeys[k] {
			return e, fmt.Errorf("%s : clé « %s » réservée au env.yaml d'un projet (ici : packages, packages_by_manager, setup, on_start)", launcherEnvPath(), k)
		}
	}
	if err := yaml.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("%s : %w", launcherEnvPath(), err)
	}
	e.raw = b
	return e, nil
}

// withLauncher : environnement du projet complété par celui du launcher —
// paquets réunis, étapes du launcher d'abord ; ses changements entrent dans les
// empreintes (bootstrap ou setup rejoués).
func (e EnvSpec) withLauncher(l EnvSpec) EnvSpec {
	if len(l.raw) == 0 {
		return e
	}
	union := func(a, b []string) []string {
		seen := map[string]bool{}
		var out []string
		for _, x := range append(append([]string(nil), a...), b...) {
			if !seen[x] {
				seen[x], out = true, append(out, x)
			}
		}
		return out
	}
	e.Packages = union(l.Packages, e.Packages)
	if len(l.PackagesByManager) > 0 {
		m := map[string][]string{}
		for k, v := range e.PackagesByManager {
			m[k] = v
		}
		for k, v := range l.PackagesByManager {
			m[k] = union(v, m[k])
		}
		e.PackagesByManager = m
	}
	e.Setup = append(append([]string(nil), l.Setup...), e.Setup...)
	e.OnStart = append(append([]string(nil), l.OnStart...), e.OnStart...)
	e.raw = append(append(append([]byte(nil), e.raw...), "\x00launcher\x00"...), l.raw...)
	return e
}

// loadEnvFull : env.yaml du projet complété par celui du launcher (bootstrap).
func loadEnvFull(proj string) (EnvSpec, error) {
	e, err := loadEnv(proj)
	if err != nil {
		return e, err
	}
	l, err := loadLauncherEnv()
	if err != nil {
		return e, err
	}
	return e.withLauncher(l), nil
}

// cleanRel : chemin relatif au projet, sans sortir du projet (ni « : », séparateur de -v).
func cleanRel(p string) (string, error) {
	c := filepath.Clean(strings.TrimSpace(p))
	switch {
	case c == "." || c == "":
		return "", fmt.Errorf("chemin vide")
	case filepath.IsAbs(c):
		return "", fmt.Errorf("chemin relatif au projet attendu")
	case c == ".." || strings.HasPrefix(c, "../"):
		return "", fmt.Errorf("hors du projet")
	case strings.ContainsAny(c, ":,"):
		return "", fmt.Errorf("« : » et « , » interdits")
	}
	return c, nil
}

// localPaths : chemins absolus des dossiers « local » du projet.
func (e EnvSpec) localPaths(proj string) []string {
	out := make([]string, len(e.Local))
	for i, l := range e.Local {
		out[i] = filepath.Join(proj, l)
	}
	return out
}

// localKey : empreinte des dossiers « local » (label cl.local du conteneur) ; ils
// sont posés à la création : une liste changée impose de recréer le conteneur.
func (e EnvSpec) localKey() string {
	if len(e.Local) == 0 {
		return ""
	}
	l := append([]string(nil), e.Local...)
	sort.Strings(l)
	h := sha256.Sum256([]byte(strings.Join(l, "\n")))
	return hex.EncodeToString(h[:])[:12]
}

// setupHash : empreinte des étapes setup et du contenu des fichiers watch ; si
// elle change (verrou de dépendances modifié…), seules les étapes setup sont
// rejouées au prochain démarrage, sans refaire tout le bootstrap.
func (e EnvSpec) setupHash(proj string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(e.Setup, "\n")))
	for _, w := range e.Watch {
		fmt.Fprintf(h, "\x00%s\x00", w)
		if b, err := os.ReadFile(filepath.Join(proj, w)); err == nil {
			h.Write(b)
		} else {
			h.Write([]byte("(absent)"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
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
	Name      string            `yaml:"-"`
	Type      string            `yaml:"type"`        // host | podman | docker | ssh
	Context   string            `yaml:"context"`     // docker : --context
	Host      string            `yaml:"host"`        // docker / podman : adresse du démon ou du service (tcp://…) ; ssh : destination ssh
	Hosts     []string          `yaml:"hosts"`       // groupe de serveurs (#61, pool.go) : à la place de host
	Pool      string            `yaml:"pool"`        // pool de machines nommé (#64) : host sert alors de modèle ({} = machine)
	Machines  map[string]string `yaml:"-"`           // pool : adresse du serveur → machine
	Drain     map[string]bool   `yaml:"-"`           // pool : serveurs (adresses) sans nouveaux projets
	Mates     []BackendConf     `yaml:"-"`           // pool : autres backends du même pool (charge commune)
	Server    string            `yaml:"-"`           // membre d'un groupe : son serveur (at)
	Engine    string            `yaml:"engine"`      // ssh : moteur sur la machine distante, podman (défaut) ou docker
	EngHost   string            `yaml:"engine_host"` // ssh : adresse du démon côté machine distante (docker rootless, service podman…)
	Shared    []string          `yaml:"shared"`      // distant : racines du stockage partagé (mêmes chemins des deux côtés)
	Namespace string            `yaml:"namespace"`   // k8s
	Volumes   map[string]string `yaml:"volumes"`     // k8s : racine partagée → PVC ReadWriteMany monté à ce chemin
	Spread    *bool             `yaml:"spread"`      // k8s : répartir les pods NG2 sur les nodes (défaut : oui, #60)
	Pod       map[string]any    `yaml:"pod"`         // k8s : ajouté au spec du pod (nodeSelector, affinity, tolerations, resources…)
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
	if b.pool() {
		return b.at(b.Hosts[0]).remote() // groupe : distant comme ses serveurs
	}
	return b.Type == "ssh" || b.Type == "k8s" || (b.Type == "docker" || b.Type == "podman") && b.Host != "" && !strings.HasPrefix(b.Host, "unix://")
}

type backendsFile struct {
	Pools    map[string]PoolConf    `yaml:"pools"` // pools de machines nommés (#64, pool.go)
	Backends map[string]BackendConf `yaml:"backends"`
	Images   []string               `yaml:"images"` // socles proposés à la création (défaut : defaultImages)
	Defaults struct {
		Backend string `yaml:"backend"` // mode présélectionné à la création : host ou un backend conteneur
		Image   string `yaml:"image"`   // socle présélectionné
	} `yaml:"defaults"`
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
	if err := checkPools(f.Pools); err != nil {
		return nil, err
	}
	var out []BackendConf
	for n, c := range f.Backends {
		c.Name = n
		if c.Pool != "" {
			if err := c.usePool(f.Pools); err != nil {
				return nil, fmt.Errorf("backends.yaml : backend %q : %w", n, err)
			}
		}
		if len(c.Hosts) > 0 {
			if c.Type != "podman" && c.Type != "docker" && c.Type != "ssh" {
				return nil, fmt.Errorf("backends.yaml : backend %q : « hosts » (groupe de serveurs) réservé à podman, docker et ssh", n)
			}
			if c.Host != "" {
				return nil, fmt.Errorf("backends.yaml : backend %q : « host » ou « hosts », pas les deux", n)
			}
			seen := map[string]bool{}
			for _, h := range c.Hosts {
				if strings.TrimSpace(h) == "" || seen[h] {
					return nil, fmt.Errorf("backends.yaml : backend %q : serveur vide ou en double dans « hosts »", n)
				}
				seen[h] = true
			}
		}
		switch c.Type {
		case "host", "podman", "docker":
		case "k8s":
			c.Shared = nil // les racines partagées sont celles des volumes
			for r := range c.Volumes {
				c.Shared = append(c.Shared, r)
			}
			sort.Strings(c.Shared)
			if err := validatePod(c.Pod); err != nil {
				return nil, fmt.Errorf("backends.yaml : backend %q : %w", n, err)
			}
		case "ssh":
			if c.Host == "" && len(c.Hosts) == 0 {
				return nil, fmt.Errorf("backends.yaml : backend ssh %q sans « host » (destination ssh)", n)
			}
			if c.Engine != "" && c.Engine != "podman" && c.Engine != "docker" {
				return nil, fmt.Errorf("backends.yaml : backend ssh %q : engine %q (podman ou docker)", n, c.Engine)
			}
		default:
			return nil, fmt.Errorf("backends.yaml : backend %q de type %q non géré (host, podman, docker, ssh, k8s)", n, c.Type)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == "host") != (out[j].Name == "host") {
			return out[i].Name == "host"
		}
		return out[i].Name < out[j].Name
	})
	linkMates(out)
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

// defaultImages : socles proposés par défaut (bootstrap vérifié sur chacun).
var defaultImages = []string{
	"docker.io/library/debian:12",
	"docker.io/library/ubuntu:24.04",
	"docker.io/library/rockylinux:9",
	"docker.io/library/alpine:3.20",
}

// baseImages : socles proposés (images: de backends.yaml, sinon defaultImages).
func baseImages() []string {
	var f backendsFile
	if b, err := os.ReadFile(backendsPath()); err == nil && yaml.Unmarshal(b, &f) == nil && len(f.Images) > 0 {
		return f.Images
	}
	return defaultImages
}

// imageLabel : « debian:12 » pour docker.io/library/debian:12.
func imageLabel(img string) string {
	return strings.TrimPrefix(strings.TrimPrefix(img, "docker.io/"), "library/")
}

// execDefaults : defaults: de backends.yaml (mode et socle présélectionnés).
func execDefaults() (backend, image string) {
	var f backendsFile
	if b, err := os.ReadFile(backendsPath()); err == nil {
		_ = yaml.Unmarshal(b, &f)
	}
	return f.Defaults.Backend, f.Defaults.Image
}

// ctrBackends : backends à conteneurs (podman, docker, ssh, k8s), dans l'ordre.
func ctrBackends() []BackendConf {
	bs, _ := loadBackends()
	var out []BackendConf
	for _, b := range bs {
		if _, ok := engineOf(b); ok {
			out = append(out, b)
		}
	}
	return out
}

// defaultCtrBackend : backend conteneur présélectionné (defaults.backend s'il
// désigne un conteneur, sinon le premier).
func defaultCtrBackend() (BackendConf, bool) {
	cs := ctrBackends()
	if len(cs) == 0 {
		return BackendConf{}, false
	}
	def, _ := execDefaults()
	for _, b := range cs {
		if b.Name == def {
			return b, true
		}
	}
	return cs[0], true
}

// writeEnv : env.yaml minimal et commenté pour le socle img (sans écraser un existant).
func writeEnv(proj, img string) error {
	p := envPath(proj)
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("# Exécution du projet en conteneur (Claude-Launcher-NG2) — versionné avec le projet\n"+
		"image: "+img+"\n"+
		"# packages: []              # paquets en plus (tmux, git, bash, sudo, curl : implicites)\n"+
		"# packages_by_manager: {}   # noms propres à une distribution, ex. { dnf: [libpq-devel] }\n"+
		"# setup: []                 # étapes exécutées une fois, en utilisateur, dans le projet\n"+
		"# mounts: []                # dossiers en plus, ex. [{ path: ../autre, ro: true }]\n"), 0o644)
}

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
	if backend == "" {
		delete(m, proj)
	} else {
		m[proj] = backend // « host » compris : le projet est configuré (plus de question à l'ouverture)
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

// assigned : le projet a déjà été configuré (backend choisi, host compris).
func assigned(proj string) bool {
	_, ok := readAssignments()[proj]
	return ok
}

// setEnvImage : remplace la ligne « image: » d'env.yaml (le reste est gardé).
func setEnvImage(proj, img string) error {
	p := envPath(proj)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	done := false
	for i, l := range lines {
		if strings.HasPrefix(l, "image:") {
			lines[i], done = "image: "+img, true
			break
		}
	}
	if !done {
		lines = append([]string{"image: " + img}, lines...)
	}
	return os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
}

// projectBackend : backend affecté au projet ; host sans env.yaml ou sans
// affectation valide. Une affectation absente reprend « prefer » d'env.yaml.
// launchBackend : backend sur lequel le projet DOIT être lancé. Un projet
// configuré ne part jamais ailleurs : backend disparu de backends.yaml, ou
// env.yaml absent / illisible pour un backend conteneur → erreur (F2 pour
// reconfigurer), jamais un repli sur l'hôte.
func launchBackend(proj string) (BackendConf, error) {
	name, ok := readAssignments()[proj]
	if !ok {
		return projectBackend(proj), nil // jamais configuré (aucun backend conteneur : hôte)
	}
	if name == "host" {
		return BackendConf{Name: "host", Type: "host"}, nil
	}
	b, found := findBackend(name)
	if !found {
		return b, fmt.Errorf("backend « %s » du projet absent de backends.yaml — F2 pour reconfigurer", name)
	}
	if _, ok := engineOf(b); ok {
		if !hasEnv(proj) {
			return b, fmt.Errorf("projet configuré sur « %s » mais %s absent — F2 pour reconfigurer", name, envFile)
		}
		if _, err := loadEnv(proj); err != nil {
			return b, fmt.Errorf("%v — corriger le fichier ou F2 pour reconfigurer", err)
		}
	}
	return b, nil
}

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
	case "podman", "docker", "ssh", "k8s":
		return Engine{b}, true
	}
	return Engine{}, false
}

// podman : moteur podman (direct ou par ssh) — utilisateur gardé (keep-id).
func (e Engine) podman() bool { return e.b.engine() == "podman" }

// engineArgv : client du moteur et adresse de son démon / service.
// sshMux : connexion ssh persistante, partagée par les commandes vers un même
// serveur (le scan des groupes ne rouvre pas une session toutes les 10 s).
func sshMux() []string {
	d := filepath.Join(os.TempDir(), "cl-ssh-"+strconv.Itoa(os.Getuid()))
	if os.MkdirAll(d, 0o700) != nil {
		return nil
	}
	return []string{"-o", "ControlMaster=auto", "-o", "ControlPath=" + d + "/%C", "-o", "ControlPersist=120"}
}

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
		out := append([]string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, sshMux()...)
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

// listTimeout : délai d'une commande de listing.
var listTimeout = 8 * time.Second

// quick : sortie d'une commande du moteur, abandonnée au-delà de listTimeout.
func (e Engine) quick(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	a := e.cli(args...)
	out, err := exec.CommandContext(ctx, a[0], a[1:]...).Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s : pas de réponse en %s", e.b.Host, listTimeout)
	}
	return out, err
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

// stateLocal : "running", "exited", "created"… ou "" si le conteneur n'existe
// pas ; son image et l'empreinte des dossiers « local » posés à sa création.
func (e Engine) stateLocal(name string) (status, image, local string) {
	b, err := e.cmd("inspect", "-f", `{{.State.Status}}|{{index .Config.Labels "cl.image"}}|{{index .Config.Labels "cl.local"}}`, name).Output()
	if err != nil {
		return "", "", ""
	}
	f := strings.SplitN(strings.TrimSpace(string(b)), "|", 3)
	for len(f) < 3 {
		f = append(f, "")
	}
	if f[2] == "<no value>" {
		f[2] = ""
	}
	return f[0], f[1], f[2]
}

// pid1 : processus principal qui ignore tout sauf SIGTERM (arrêt propre, sans --init).
const pid1 = `trap 'exit 0' TERM INT; while :; do sleep 3600 & wait $!; done`

// ensure : conteneur du projet créé (ou recréé si l'image a changé) et démarré.
func (e Engine) ensure(proj string, env EnvSpec, log io.Writer) (string, error) {
	if e.b.k8s() {
		return e.k8sEnsure(proj, env, log)
	}
	name := ctrName(proj)
	status, image, local := e.stateLocal(name)
	if status != "" && (image != env.Image || local != env.localKey()) {
		if image != env.Image {
			fmt.Fprintf(log, "  image changée (%s → %s) : conteneur recréé\n", image, env.Image)
		} else {
			fmt.Fprintf(log, "  dossiers « local » changés : conteneur recréé\n")
		}
		// -v : ses volumes anonymes (dossiers « local ») partent avec lui
		_ = e.cmd("rm", "-f", "-v", name).Run()
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
		"--label", "cl.local=" + env.localKey(),
		"--mount", bind(cd, false), "--mount", bind(proj, false), "-w", proj,
		"-e", "HOME=" + home, "-e", "CLAUDE_CONFIG_DIR=" + cd, "-e", "LANG=C.UTF-8",
		"-e", "DISABLE_AUTOUPDATER=1"}
	if e.podman() {
		args = append(args, "--userns=keep-id", "--user", "0")
	}
	// cache de consommation : plus de montage, un lien vers la config claude
	// (montée partout) est posé à chaque ouverture (usage.go, linkUsageScript)
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
	// dossiers « local » : volume anonyme du conteneur, monté par-dessus le
	// dossier du projet (masqué) ; supprimé avec le conteneur (rm -v)
	for _, p := range env.localPaths(proj) {
		args = append(args, "-v", p)
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
	if e.b.k8s() {
		return e.k8sExecArgv(name, root, tty, argv...)
	}
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
	Server  string // groupe de serveurs : serveur où le conteneur a été trouvé
	Failed  bool   // devrait tourner mais n'y arrive pas (pod pas prêt, conteneur dead/restarting)
}

// list : conteneurs NG2 du backend — de tous ses serveurs pour un groupe (#61) ;
// erreur seulement si aucun ne répond (les injoignables : listPool).
func (e Engine) list() ([]Found, error) {
	if e.b.k8s() {
		return e.k8sList()
	}
	fs, down := e.listPool()
	if len(down) == len(e.b.servers()) {
		for _, err := range down {
			return nil, err
		}
	}
	return fs, nil
}

// listOne : conteneurs NG2 d'un serveur (par label ; labels relus par inspect,
// même format pour podman et docker).
func (e Engine) listOne() ([]Found, error) {
	// filtre aussi par backend : deux backends peuvent viser le même moteur
	// délai borné : un serveur éteint ne doit pas bloquer le scan (groupes, #61)
	b, err := e.quick("ps", "-a", "-q", "--filter", "label=cl.launcher=ng2", "--filter", "label=cl.backend="+e.b.Name)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return nil, nil
	}
	b, err = e.quick(append([]string{"inspect", "-f",
		`{{.Name}}|{{.State.Status}}|{{index .Config.Labels "cl.project"}}`}, ids...)...)
	if err != nil {
		return nil, err
	}
	var out []Found
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) != 3 || f[2] == "" {
			continue
		}
		out = append(out, Found{Backend: e.b.Name, Name: strings.TrimPrefix(f[0], "/"), Project: f[2], Running: f[1] == "running",
			Failed: f[1] == "dead" || f[1] == "restarting"})
	}
	return out, nil
}
