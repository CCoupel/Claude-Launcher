package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Diagnostic des backends ───────────────────────────────────────────────
//
// Quand un backend n'est pas utilisable, le launcher le dit et explique comment
// le configurer : moteur absent ou injoignable, droits sur le socket docker,
// accès ssh, podman absent de la machine distante, stockage partagé manquant.

type problem struct {
	what string // ce qui ne va pas
	fix  string // comment y remédier
}

func (p problem) String() string { return p.what + " — " + p.fix }

// underShared : p est sous l'une des racines partagées du backend.
func underShared(b BackendConf, p string) bool {
	p = filepath.Clean(p)
	for _, r := range b.Shared {
		r = filepath.Clean(r)
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// sharedProblems : prérequis du stockage partagé pour un backend distant (et,
// si proj n'est pas vide, pour ce projet).
func sharedProblems(b BackendConf, proj string) []problem {
	if !b.remote() {
		return nil
	}
	if len(b.Shared) == 0 {
		return []problem{{"backend " + b.Name + " distant sans stockage partagé déclaré",
			"ajouter « shared: [/racine/partagée] » au backend dans " + backendsPath() +
				" ; ~/.claude et les projets doivent y être, aux mêmes chemins ici et sur " + b.Host}}
	}
	var out []problem
	for _, r := range b.Shared {
		if _, err := os.Stat(r); err != nil {
			out = append(out, problem{"racine partagée " + r + " absente sur cette machine",
				"monter le stockage partagé ici au même chemin que sur " + b.Host + " (ex. MooseFS : mfsmount " + r + " -H <master>)"})
		}
	}
	if cd := claudeDir(); !underShared(b, cd) {
		out = append(out, problem{cd + " n'est pas sur le stockage partagé (" + strings.Join(b.Shared, ", ") + ")",
			"y placer la config de claude (CLAUDE_CONFIG_DIR=<racine partagée>/…/.claude) : conversations, teams et connexion doivent être vues des deux côtés"})
	}
	if proj != "" && !underShared(b, proj) {
		out = append(out, problem{"projet hors du stockage partagé (" + strings.Join(b.Shared, ", ") + ")",
			"déplacer le projet sous une racine partagée, ou choisir un backend local (F2)"})
	}
	return out
}

// checkShared : erreur explicite si le projet ne peut pas tourner sur ce backend.
func (e Engine) checkShared(proj string) error {
	ps := sharedProblems(e.b, proj)
	if e.b.Type == "ssh" && len(ps) == 0 {
		// les mêmes chemins doivent exister de l'autre côté
		for _, p := range []string{claudeDir(), proj} {
			if exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", e.b.Host, "test -d "+shq(p)).Run() != nil {
				ps = append(ps, problem{p + " absent sur " + e.b.Host,
					"monter le stockage partagé sur " + e.b.Host + " au même chemin qu'ici"})
			}
		}
	}
	if len(ps) == 0 {
		return nil
	}
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "\n  ⚠ %s\n    → %s", p.what, p.fix)
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

// diagnose : problèmes du backend (vide : utilisable) — moteur, puis stockage partagé.
func diagnose(b BackendConf) []problem {
	return append(engineProblems(b), sharedProblems(b, "")...)
}

func engineProblems(b BackendConf) []problem {
	e := Engine{b}
	switch b.Type {
	case "podman":
		if _, err := exec.LookPath("podman"); err != nil {
			return []problem{{"podman absent de cette machine", "l'installer : sudo apt install -y podman (ou dnf install podman)"}}
		}
		if out, err := runQuiet(8*time.Second, e.cli("info", "--format", "{{.Host.Security.Rootless}}")...); err != nil {
			if b.remote() {
				return []problem{{"service podman de " + b.Host + " injoignable : " + firstLine(out, err),
					"le lancer sur l'hôte (utilisateur) : podman system service --time=0 " + b.Host + " — API non authentifiée ni chiffrée : réseau de confiance seulement, sinon backend ssh"}}
			}
			return []problem{{"podman ne répond pas : " + firstLine(out, err), "vérifier « podman info » (sous-uids : /etc/subuid, /etc/subgid)"}}
		}
	case "docker":
		if _, err := exec.LookPath("docker"); err != nil {
			return []problem{{"client docker absent de cette machine",
				"installer le client seul (binaire statique : https://download.docker.com/linux/static/stable/) ou, sous WSL, activer Docker Desktop → Settings → Resources → WSL integration"}}
		}
		if p, ok := dockerProblem(b, runQuietErr(8*time.Second, e.cli("version", "--format", "{{.Server.Version}}")...), ""); ok {
			return []problem{p}
		}
	case "ssh":
		if out, err := runQuiet(12*time.Second, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", b.Host, "true"); err != nil {
			return []problem{{"accès ssh à " + b.Host + " impossible : " + firstLine(out, err),
				"configurer un accès par clé sans mot de passe : ssh-copy-id " + b.Host + " (et l'hôte dans ~/.ssh/config)"}}
		}
		eng := b.engine()
		if _, err := runQuiet(12*time.Second, "ssh", "-o", "BatchMode=yes", b.Host, "command -v "+eng); err != nil {
			return []problem{{eng + " absent de " + b.Host + " (ou hors du PATH d'une commande ssh)",
				"l'installer : ssh " + b.Host + " sudo apt install -y " + map[string]string{"podman": "podman", "docker": "docker.io"}[eng] + " — ou choisir l'autre moteur (engine: podman|docker)"}}
		}
		if eng == "docker" {
			if p, ok := dockerProblem(b, runQuietErr(12*time.Second, e.cli("version", "--format", "{{.Server.Version}}")...), " sur "+b.Host); ok {
				return []problem{p}
			}
		} else if out, err := runQuiet(12*time.Second, e.cli("info", "--format", "{{.Host.Security.Rootless}}")...); err != nil {
			return []problem{{"podman ne répond pas sur " + b.Host + " : " + firstLine(out, err), "vérifier « ssh " + b.Host + " podman info »"}}
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
func dockerProblem(b BackendConf, r qerr, where string) (problem, bool) {
	if r.err == nil {
		return problem{}, false
	}
	msg := firstLine(r.out, r.err)
	switch {
	case strings.Contains(msg, "permission denied"):
		return problem{"accès au démon docker refusé" + where + " : " + msg,
			"ajouter l'utilisateur au groupe docker" + where + " : sudo usermod -aG docker $USER, puis ouvrir une nouvelle session"}, true
	case strings.Contains(msg, "could not be found in this WSL"):
		return problem{"pas de client docker utilisable (lanceur de Docker Desktop sans intégration WSL)",
			"installer le client docker seul (binaire statique : https://download.docker.com/linux/static/stable/) ou activer Docker Desktop → Settings → Resources → WSL integration"}, true
	case b.Type == "docker" && b.remote():
		return problem{"démon docker de " + b.Host + " injoignable : " + msg,
			"l'exposer : sur l'hôte, /etc/docker/daemon.json → \"hosts\": [\"unix:///var/run/docker.sock\", \"tcp://0.0.0.0:2375\"] puis systemctl restart docker (TLS : port 2376 et --tlsverify)"}, true
	default:
		return problem{"démon docker injoignable" + where + " : " + msg,
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
	ps []problem
}

func diagnoseCached(b BackendConf) []problem {
	key := fmt.Sprintf("%+v", b)
	diagCache.Lock()
	e, ok := diagCache.m[key]
	diagCache.Unlock()
	if ok && time.Since(e.at) < time.Minute {
		return e.ps
	}
	ps := diagnose(b)
	diagCache.Lock()
	diagCache.m[key] = diagEntry{time.Now(), ps}
	diagCache.Unlock()
	return ps
}
