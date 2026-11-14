package core

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Backends refusés sous Windows (#110) ──────────────────────────────────
//
// Sous Windows, seuls les backends distants sont admis : docker/podman tcp://
// ou ssh:// (ou contexte docker), type ssh, k8s. host (claude sur le poste) et
// les moteurs locaux (socket unix://, npipe://, socket par défaut) sont refusés
// avec un message explicite, au diagnostic comme à l'ouverture d'un projet.
// Ailleurs : rien n'est refusé.

// engineEnv : ce qui désigne le démon d'un docker/podman sans « host » : les
// variables DOCKER_HOST / CONTAINER_HOST et le contexte docker actif.
type engineEnv struct {
	DockerHost, ContainerHost string
	DockerContextHost         func() string // adresse du contexte docker actif ("" : inconnue)
}

// effectiveHost : adresse du démon réellement visée par un client sans host.
func (ev engineEnv) effectiveHost(engine string) string {
	switch engine {
	case "docker":
		if ev.DockerHost != "" {
			return ev.DockerHost
		}
		if ev.DockerContextHost != nil {
			return ev.DockerContextHost()
		}
	case "podman":
		return ev.ContainerHost
	}
	return ""
}

// remoteEngineAddr : adresse d'un démon distant (tcp://, ssh://).
func remoteEngineAddr(h string) bool {
	h = strings.TrimSpace(h)
	return strings.HasPrefix(h, "tcp://") || strings.HasPrefix(h, "ssh://")
}

// localRefusal : logique pure ; ok vrai si b est refusé sur goos.
func localRefusal(goos string, b BackendConf, ev engineEnv) (Problem, bool) {
	if goos != "windows" {
		return Problem{}, false
	}
	fix := "sous Windows, les projets tournent dans un conteneur distant : backend docker/podman avec host: tcp://… ou ssh://… (ou DOCKER_HOST / CONTAINER_HOST / contexte docker distants), type ssh, ou k8s (" + BackendsPath() + ")"
	switch b.Type {
	case "host":
		return Problem{"backend host refusé sous Windows (claude ne tourne pas sur le poste)", fix}, true
	case "docker", "podman":
		if b.Type == "docker" && b.Context != "" {
			return Problem{}, false // contexte docker nommé : sa cible est résolue par docker
		}
		hosts := b.Hosts
		if len(hosts) == 0 {
			hosts = []string{b.Host}
		}
		for _, h := range hosts {
			if h == "" {
				// client sans adresse : celle de l'environnement ou du contexte actif
				if eh := ev.effectiveHost(b.Type); remoteEngineAddr(eh) {
					continue
				}
			}
			if localEngineAddr(h) {
				what := "socket par défaut"
				if h != "" {
					what = h
				}
				return Problem{b.Type + " local refusé sous Windows (" + what + ")", fix}, true
			}
		}
	}
	return Problem{}, false
}

// localEngineAddr : adresse de démon locale (vide : socket par défaut).
func localEngineAddr(h string) bool {
	h = strings.TrimSpace(h)
	return h == "" || strings.HasPrefix(h, "unix://") || strings.HasPrefix(h, "npipe://")
}

// dockerContextHost : adresse du contexte docker actif, lue une fois (Windows
// seulement : localRefusal ne l'interroge pas ailleurs).
var dockerContextHost = sync.OnceValue(func() string {
	if _, err := exec.LookPath("docker"); err != nil {
		return ""
	}
	out, err := runQuiet(4*time.Second, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
})

// WarmEngineEnv : lit d'avance le contexte docker actif (Windows seulement :
// ailleurs rien ne l'interroge), pour que la première validation de
// backends.yaml ne paie pas « docker context inspect ».
func WarmEngineEnv() {
	if runtime.GOOS == "windows" {
		_ = dockerContextHost()
	}
}

func runtimeEngineEnv() engineEnv {
	return engineEnv{DockerHost: os.Getenv("DOCKER_HOST"), ContainerHost: os.Getenv("CONTAINER_HOST"), DockerContextHost: dockerContextHost}
}

// LocalRefusal : b est-il refusé sur ce système (et pourquoi) ?
func LocalRefusal(b BackendConf) (Problem, bool) {
	return localRefusal(runtime.GOOS, b, runtimeEngineEnv())
}

// refusalErr : erreur d'ouverture d'un projet sur un backend refusé.
func refusalErr(b BackendConf) error {
	if p, ok := LocalRefusal(b); ok {
		return fmt.Errorf("%s — %s", p.What, p.Fix)
	}
	return nil
}

// ── Outils externes requis (#110) ─────────────────────────────────────────
//
// Le launcher pilote les backends par leurs clients en ligne de commande, non
// embarqués : ssh, git, docker/podman, kubectl, gh. Le diagnostic liste ceux
// qu'exigent les backends RÉELLEMENT déclarés, et sous Windows la commande
// winget qui installe chacun de ceux qui manquent.

// ToolStatus : un outil externe et son état sur ce poste.
type ToolStatus struct {
	Tool       string   // commande (ssh, git, docker, podman, kubectl, gh)
	RequiredBy []string // « launcher » et/ou noms des backends qui l'utilisent
	Optional   bool     // le launcher fonctionne sans (repli, fonction en moins)
	Found      bool
	Path       string // chemin trouvé dans le PATH
	Install    string // commande d'installation (winget sous Windows)
}

// installCmds : commande d'installation par système.
var installCmds = map[string]map[string]string{
	"windows": {
		"ssh":     "Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0",
		"git":     "winget install --id Git.Git -e",
		"docker":  "winget install --id Docker.DockerCLI -e",
		"podman":  "winget install --id RedHat.Podman -e",
		"kubectl": "winget install --id Kubernetes.kubectl -e",
		"gh":      "winget install --id GitHub.cli -e",
	},
	"linux": {
		"ssh":     "sudo apt install -y openssh-client",
		"git":     "sudo apt install -y git",
		"docker":  "client seul : https://download.docker.com/linux/static/stable/",
		"podman":  "sudo apt install -y podman",
		"kubectl": "https://kubernetes.io/docs/tasks/tools/",
		"gh":      "https://cli.github.com",
	},
	"darwin": {
		"ssh":     "intégré à macOS",
		"git":     "xcode-select --install",
		"docker":  "brew install docker",
		"podman":  "brew install podman",
		"kubectl": "brew install kubectl",
		"gh":      "brew install gh",
	},
}

// InstallCmd : commande d'installation de tool sur goos ("" : inconnue).
func InstallCmd(goos, tool string) string { return installCmds[goos][tool] }

// toolsFor : outil → ce qui l'exige, pour ces backends (logique pure).
func toolsFor(goos string, bs []BackendConf, ev engineEnv) map[string][]string {
	need := map[string][]string{}
	add := func(t, by string) {
		for _, x := range need[t] {
			if x == by {
				return
			}
		}
		need[t] = append(need[t], by)
	}
	add("git", "launcher")
	add("gh", "launcher")
	for _, b := range bs {
		if _, refused := localRefusal(goos, b, ev); refused || b.Type == "host" {
			continue
		}
		hosts := b.Hosts
		if len(hosts) == 0 {
			hosts = []string{b.Host}
		}
		switch b.Type {
		case "k8s":
			add("kubectl", b.Name)
		case "ssh":
			add("ssh", b.Name) // le moteur est celui de la machine distante
		case "docker":
			add("docker", b.Name)
			for _, h := range hosts {
				if strings.HasPrefix(h, "ssh://") {
					add("ssh", b.Name) // docker -H ssh:// passe par le client ssh
				}
			}
		case "podman":
			add("podman", b.Name) // podman --url ssh:// a son propre client ssh
		}
	}
	return need
}

// toolsStatus : état de chaque outil requis (logique pure ; look : exec.LookPath).
func toolsStatus(goos string, bs []BackendConf, ev engineEnv, look func(string) (string, error)) []ToolStatus {
	need := toolsFor(goos, bs, ev)
	order := map[string]int{"ssh": 0, "git": 1, "docker": 2, "podman": 3, "kubectl": 4, "gh": 5}
	names := make([]string, 0, len(need))
	for t := range need {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool { return order[names[i]] < order[names[j]] })
	var out []ToolStatus
	for _, t := range names {
		s := ToolStatus{Tool: t, RequiredBy: need[t], Optional: t == "gh", Install: InstallCmd(goos, t)}
		if p, err := look(t); err == nil {
			s.Found, s.Path = true, p
		}
		out = append(out, s)
	}
	return out
}

// CheckTools : outils requis par les backends déclarés, et leur présence ici.
// Réutilisable par l'interface (écran de premier lancement, #111).
func CheckTools() []ToolStatus {
	bs, _ := LoadBackends()
	return toolsStatus(runtime.GOOS, bs, runtimeEngineEnv(), exec.LookPath)
}

// MissingTools : outils requis absents (optionnels compris).
func MissingTools(ts []ToolStatus) []ToolStatus {
	var out []ToolStatus
	for _, t := range ts {
		if !t.Found {
			out = append(out, t)
		}
	}
	return out
}

// PrintTools : section de « claude-launcher-ng2 backends ».
func PrintTools() {
	fmt.Println("\nOutils externes (selon les backends déclarés) :")
	for _, t := range CheckTools() {
		by := strings.Join(t.RequiredBy, ", ")
		if t.Found {
			fmt.Printf("  ✓ %-8s %s  (%s)\n", t.Tool, t.Path, by)
			continue
		}
		opt := ""
		if t.Optional {
			opt = ", facultatif"
		}
		fmt.Printf("  ⚠ %-8s absent  (%s%s)\n", t.Tool, by, opt)
		if t.Install != "" {
			fmt.Printf("      → %s\n", t.Install)
		}
	}
}
