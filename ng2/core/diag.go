package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ── Diagnostic des backends ───────────────────────────────────────────────
//
// Quand un backend n'est pas utilisable, le launcher le dit et explique comment
// le configurer : moteur absent ou injoignable, droits sur le socket docker,
// accès ssh, podman absent de la machine distante, stockage partagé manquant.

type Problem struct {
	What string // ce qui ne va pas
	Fix  string // comment y remédier
}

func (p Problem) String() string { return p.What + " — " + p.Fix }

// underShared : p (chemin vu des backends, Linux) est sous l'une des racines
// partagées du backend.
func underShared(b BackendConf, p string) bool {
	p = path.Clean(p)
	for _, r := range b.Shared {
		r = path.Clean(r)
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// sharedProblems : prérequis du stockage partagé pour un backend distant (et,
// si proj n'est pas vide, pour ce projet).
func sharedProblems(b BackendConf, proj string) []Problem {
	if !b.remote() {
		return nil
	}
	if len(b.Shared) == 0 {
		return []Problem{{"backend " + b.Name + " distant sans stockage partagé déclaré",
			"ajouter « shared: [/racine/partagée] » au backend dans " + BackendsPath() +
				" ; ~/.claude et les projets doivent y être, aux mêmes chemins ici et sur " + b.Host}}
	}
	var out []Problem
	for _, r := range b.Shared {
		// racine Linux : cherchée ici à son chemin local (pathmap.go)
		if _, err := os.Stat(LocalPath(r)); err != nil {
			out = append(out, Problem{"racine partagée " + r + " absente sur cette machine",
				sharedRootFix(r, b.Host)})
		}
	}
	// chemins vus des backends ; sous Windows, un chemin sans correspondance
	// Linux est signalé tel quel
	for _, p := range []string{ClaudeDir(), proj} {
		if p == "" {
			continue
		}
		if _, err := RemotePathChecked(p); err != nil {
			out = append(out, Problem{err.Error(), "ajouter sa correspondance dans hubs.<machine>.paths (" + BackendsPath() + ")"})
		}
	}
	if cd := RemotePath(ClaudeDir()); !underShared(b, cd) {
		out = append(out, Problem{cd + " n'est pas sur le stockage partagé (" + strings.Join(b.Shared, ", ") + ")",
			"y placer la config de claude (CLAUDE_CONFIG_DIR=<racine partagée>/…/.claude) : conversations, teams et connexion doivent être vues des deux côtés"})
	}
	if proj != "" && !underShared(b, RemotePath(proj)) {
		out = append(out, Problem{"projet hors du stockage partagé (" + strings.Join(b.Shared, ", ") + ")",
			"déplacer le projet sous une racine partagée, ou choisir un backend local (F2)"})
	}
	return out
}

// sharedRootFix : comment rendre visible ici la racine partagée r.
func sharedRootFix(r, host string) string { return sharedRootFixFor(runtime.GOOS, r, host) }

func sharedRootFixFor(goos, r, host string) string {
	if goos == "windows" {
		return "rendre le partage visible sur ce poste (ex. \\\\wsl.localhost\\<distribution>" + strings.ReplaceAll(r, "/", `\`) +
			") et déclarer sa correspondance : hubs.<machine>.paths: [{ local: '…', remote: " + r + " }] dans " + BackendsPath()
	}
	return "monter le stockage partagé ici au même chemin que sur " + host + " (ex. MooseFS : mfsmount " + r + " -H <master>)"
}

// checkShared : erreur explicite si le projet ne peut pas tourner sur ce backend.
func (e Engine) checkShared(proj string) error {
	ps := sharedProblems(e.B, proj)
	if e.B.Type == "ssh" && len(ps) == 0 {
		// les mêmes chemins doivent exister de l'autre côté
		for _, p := range []string{RemotePath(ClaudeDir()), RemotePath(proj)} {
			if exec.Command("ssh", append(append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, sshMux()...), e.B.Host, "test -d "+Shq(p))...).Run() != nil {
				ps = append(ps, Problem{p + " absent sur " + e.B.Host,
					"monter le stockage partagé sur " + e.B.Host + " au même chemin qu'ici"})
			}
		}
	}
	if len(ps) == 0 {
		return nil
	}
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "\n  ⚠ %s\n    → %s", p.What, p.Fix)
	}
	return fmt.Errorf("stockage partagé :%s", b.String())
}

func runQuiet(timeout time.Duration, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("délai dépassé (%s)", timeout)
	}
	return strings.TrimSpace(string(out)), err
}

// Diagnose : problèmes du backend (vide : utilisable) — moteur, puis stockage partagé.
func Diagnose(b BackendConf) []Problem {
	if p, refused := LocalRefusal(b); refused {
		return []Problem{p} // backend local sous Windows (oslimits.go)
	}
	if b.IsPool() {
		// groupe (#61) : utilisable tant qu'un serveur l'est ; sinon les problèmes de chacun
		var all []Problem
		for _, h := range b.Hosts {
			ps := Diagnose(b.At(h))
			if len(ps) == 0 {
				return nil
			}
			for _, p := range ps {
				all = append(all, Problem{h + " : " + p.What, p.Fix})
			}
		}
		return all
	}
	return append(engineProblems(b), sharedProblems(b, "")...)
}

// PoolProblems : problèmes de chaque serveur d'un groupe (pour « backends »).
func PoolProblems(b BackendConf) map[string][]Problem {
	out := map[string][]Problem{}
	for _, h := range b.Servers() {
		if ps := Diagnose(b.At(h)); len(ps) > 0 {
			out[h] = ps
		}
	}
	return out
}

func engineProblems(b BackendConf) []Problem {
	e := Engine{b}
	switch b.Type {
	case "k8s":
		return k8sProblems(b)
	case "podman":
		if _, err := exec.LookPath("podman"); err != nil {
			return []Problem{{"podman absent de cette machine", "l'installer : sudo apt install -y podman (ou dnf install podman)"}}
		}
		if out, err := runQuiet(8*time.Second, e.cli("info", "--format", "{{.Host.Security.Rootless}}")...); err != nil {
			if b.remote() {
				return []Problem{{"service podman de " + b.Host + " injoignable : " + firstLine(out, err),
					"le lancer sur l'hôte (utilisateur) : podman system service --time=0 " + b.Host + " — API non authentifiée ni chiffrée : réseau de confiance seulement, sinon backend ssh"}}
			}
			return []Problem{{"podman ne répond pas : " + firstLine(out, err), "vérifier « podman info » (sous-uids : /etc/subuid, /etc/subgid)"}}
		}
	case "docker":
		if _, err := exec.LookPath("docker"); err != nil {
			return []Problem{{"client docker absent de cette machine",
				"installer le client seul (binaire statique : https://download.docker.com/linux/static/stable/) ou, sous WSL, activer Docker Desktop → Settings → Resources → WSL integration"}}
		}
		if p, ok := dockerProblem(b, runQuietErr(8*time.Second, e.cli("version", "--format", "{{.Server.Version}}")...), ""); ok {
			return []Problem{p}
		}
	case "ssh":
		if out, err := runQuiet(12*time.Second, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", b.Host, "true"); err != nil {
			return []Problem{{"accès ssh à " + b.Host + " impossible : " + firstLine(out, err),
				"configurer un accès par clé sans mot de passe : ssh-copy-id " + b.Host + " (et l'hôte dans ~/.ssh/config)"}}
		}
		eng := b.engine()
		if _, err := runQuiet(12*time.Second, "ssh", "-o", "BatchMode=yes", b.Host, "command -v "+eng); err != nil {
			return []Problem{{eng + " absent de " + b.Host + " (ou hors du PATH d'une commande ssh)",
				"l'installer : ssh " + b.Host + " sudo apt install -y " + map[string]string{"podman": "podman", "docker": "docker.io"}[eng] + " — ou choisir l'autre moteur (engine: podman|docker)"}}
		}
		if eng == "docker" {
			if p, ok := dockerProblem(b, runQuietErr(12*time.Second, e.cli("version", "--format", "{{.Server.Version}}")...), " sur "+b.Host); ok {
				return []Problem{p}
			}
		} else if out, err := runQuiet(12*time.Second, e.cli("info", "--format", "{{.Host.Security.Rootless}}")...); err != nil {
			return []Problem{{"podman ne répond pas sur " + b.Host + " : " + firstLine(out, err), "vérifier « ssh " + b.Host + " podman info »"}}
		}
	}
	return nil
}

type qerr struct {
	out string
	err error
}

func runQuietErr(timeout time.Duration, argv ...string) qerr {
	out, err := runQuiet(timeout, argv...)
	return qerr{out, err}
}

// dockerProblem : lecture de l'échec d'un « docker version » (local, direct ou par ssh).
func dockerProblem(b BackendConf, r qerr, where string) (Problem, bool) {
	if r.err == nil {
		return Problem{}, false
	}
	msg := firstLine(r.out, r.err)
	switch {
	case strings.Contains(msg, "permission denied"):
		return Problem{"accès au démon docker refusé" + where + " : " + msg,
			"ajouter l'utilisateur au groupe docker" + where + " : sudo usermod -aG docker $USER, puis ouvrir une nouvelle session"}, true
	case strings.Contains(msg, "could not be found in this WSL"):
		return Problem{"pas de client docker utilisable (lanceur de Docker Desktop sans intégration WSL)",
			"installer le client docker seul (binaire statique : https://download.docker.com/linux/static/stable/) ou activer Docker Desktop → Settings → Resources → WSL integration"}, true
	case b.Type == "docker" && b.remote():
		return Problem{"démon docker de " + b.Host + " injoignable : " + msg,
			"l'exposer : sur l'hôte, /etc/docker/daemon.json → \"hosts\": [\"unix:///var/run/docker.sock\", \"tcp://0.0.0.0:2375\"] puis systemctl restart docker (TLS : port 2376 et --tlsverify)"}, true
	default:
		return Problem{"démon docker injoignable" + where + " : " + msg,
			"le démarrer (sudo systemctl start docker), ou indiquer son adresse (host / engine_host : unix:///run/user/<uid>/docker.sock pour un docker rootless)"}, true
	}
}

func firstLine(out string, err error) string {
	if l, _, _ := strings.Cut(strings.TrimSpace(out), "\n"); l != "" {
		return l
	}
	return err.Error()
}

// diagCache : diagnostic par backend, gardé une minute (le navigateur le relit
// toutes les 10 s ; un ssh ou un démon distant coûte jusqu'à plusieurs secondes).
var diagCache = struct {
	sync.Mutex
	m map[string]diagEntry
}{m: map[string]diagEntry{}}

type diagEntry struct {
	at time.Time
	ps []Problem
}

func DiagnoseCached(b BackendConf) []Problem {
	key := fmt.Sprintf("%+v", b)
	diagCache.Lock()
	e, ok := diagCache.m[key]
	diagCache.Unlock()
	if ok && time.Since(e.at) < time.Minute {
		return e.ps
	}
	ps := Diagnose(b)
	diagCache.Lock()
	diagCache.m[key] = diagEntry{time.Now(), ps}
	diagCache.Unlock()
	return ps
}
