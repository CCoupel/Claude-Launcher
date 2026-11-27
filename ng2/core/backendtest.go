package core

import (
	"claude-launcher-ng2/internal/tun"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ── Test d'un backend (#118, bouton « Tester ») ───────────────────────────
//
// Étapes lisibles d'un diagnostic sur un backend, enregistré ou non (le
// formulaire de l'interface n'a pas encore écrit le fichier) : outil requis,
// moteur joignable, stockage partagé ; k8s : kubectl, namespace, droits, PVC.
// Mêmes contrôles que le diagnostic existant (engineProblems, sharedProblems,
// k8sSteps), un seul code ; chaque contrôle a son délai, et le test entier est
// borné (TestBackendTimeout).

// TestStep : une étape du test. OK faux : Detail dit ce qui ne va pas, Fix
// comment y remédier.
type TestStep struct {
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Warn   bool   `json:"warn,omitempty"` // avertissement : jamais un problème bloquant du backend (#269)
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
	Cat    string `json:"-"` // catégorie d'un refus d'hôte (audit serveur)
}

// TestBackendTimeout borne le test entier (un ssh ou un cluster lent).
var TestBackendTimeout = tun.Of(45 * time.Second)

// TestBackend : étapes du test de b (pool : chaque serveur à son tour,
// étapes préfixées par le serveur). Au-delà du délai, une dernière étape
// « délai dépassé » conclut avec ce qui a pu être vérifié.
func TestBackend(b BackendConf) []TestStep {
	return testBackend(b, runtime.GOOS, runtimeEngineEnv(), exec.LookPath)
}

func testBackend(b BackendConf, goos string, ev engineEnv, look func(string) (string, error)) []TestStep {
	// les étapes vont dans une liste protégée, jamais dans un canal : la
	// goroutine du test ne se bloque pas quand le délai a expiré et que plus
	// personne ne lit (elle finit son travail, puis s'arrête)
	var mu sync.Mutex
	var steps []TestStep
	done := make(chan struct{})
	go func() {
		defer close(done)
		testStepsTo(b, goos, ev, look, func(st TestStep) {
			mu.Lock()
			steps = append(steps, st)
			mu.Unlock()
		})
	}()
	timer := time.NewTimer(TestBackendTimeout.Get())
	defer timer.Stop()
	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		return steps
	case <-timer.C:
		mu.Lock()
		defer mu.Unlock()
		return append(append([]TestStep(nil), steps...), TestStep{Label: "délai", Detail: fmt.Sprintf("test interrompu après %d s", int(TestBackendTimeout.Get().Seconds())),
			Fix: "le backend répond trop lentement : vérifier le réseau, le démon ou le cluster"})
	}
}

// testStepsTo : le test lui-même (sans délai global) ; chaque étape est remise
// dès qu'elle est connue, pour que le délai garde ce qui a été vérifié.
func testStepsTo(b BackendConf, goos string, ev engineEnv, look func(string) (string, error), emit func(TestStep)) {
	if b.IsPool() {
		for _, h := range b.Servers() {
			testStepsTo(b.At(h), goos, ev, look, func(st TestStep) {
				st.Label = h + " : " + st.Label
				emit(st)
			})
		}
		return
	}
	if b.Type == "host" {
		if p, refused := localRefusal(goos, b, ev); refused {
			emit(TestStep{Label: "exécution sur l'hôte", Detail: p.What, Fix: p.Fix})
			return
		}
		emit(TestStep{Label: "exécution sur l'hôte", OK: true, Detail: "aucun conteneur : rien à vérifier"})
		return
	}
	if p, refused := localRefusal(goos, b, ev); refused {
		emit(TestStep{Label: "backend utilisable sur ce système", Detail: p.What, Fix: p.Fix})
		return
	}
	missing := false
	for _, t := range toolsStatus(goos, []BackendConf{b}, ev, look) {
		if t.Optional || t.Tool == "git" { // ce que le backend exige, pas le launcher
			continue
		}
		st := TestStep{Label: "outil requis : " + t.Tool, OK: t.Found, Detail: t.Path}
		if !t.Found {
			st.Detail, st.Fix, missing = t.Tool+" absent de cette machine", t.Install, true
		}
		emit(st)
	}
	if missing {
		return // sans l'outil, le reste ne peut pas être contrôlé
	}
	if b.Type == "ssh" {
		for _, st := range sshSecretSteps(b.Name) {
			emit(st)
		}
	}
	if b.Type == "k8s" {
		// k8sSteps rejoue le contrôle de kubectl : déjà fait ci-dessus
		for _, st := range k8sSteps(b) {
			if st.Label != "outil requis : kubectl" {
				emit(st)
			}
		}
		return
	}
	if ps := engineProblems(b); len(ps) > 0 {
		for _, p := range ps {
			emit(TestStep{Label: "moteur joignable", Detail: p.What, Fix: p.Fix})
		}
		return
	}
	emit(TestStep{Label: "moteur joignable", OK: true, Detail: engineLabel(b)})
	switch ps := sharedProblems(b, ""); {
	case !b.remote():
		emit(TestStep{Label: "stockage partagé", OK: true, Detail: "non requis (backend local)"})
	case len(ps) == 0:
		emit(TestStep{Label: "stockage partagé", OK: true, Detail: strings.Join(b.Shared, ", ")})
	default:
		for _, p := range ps {
			emit(TestStep{Label: "stockage partagé", Detail: p.What, Fix: p.Fix})
		}
	}
}

func engineLabel(b BackendConf) string {
	e := b.engine()
	if b.Host != "" {
		return e + " sur " + b.Host
	}
	return e
}

// Conf : le backend du formulaire en BackendConf, pools résolus (comme le
// chargement). Les règles de forme sont celles de backendsOf.
func (e BackendEntry) Conf(pools []PoolEntry) (BackendConf, error) {
	f := backendsFile{Pools: map[string]PoolConf{}}
	for _, p := range pools {
		f.Pools[p.Name] = PoolConf{Hosts: p.Hosts, Drain: p.Drain}
	}
	name := e.Name
	if name == "" {
		name = "essai"
	}
	f.Backends = map[string]BackendConf{name: {Type: e.Type, Context: e.Context, Host: e.Host, Hosts: e.Hosts, Pool: e.Pool,
		Engine: e.Engine, EngHost: e.EngineHost, Shared: e.Shared, Namespace: e.Namespace, Volumes: e.Volumes, Spread: e.Spread, Pod: e.Pod, Kubeconfig: kubeconfigFromAny(e.Kubeconfig),
		PVC: e.PVC, Volume: e.Volume, TLS: e.TLS}}
	bs, err := backendsOf(f)
	if err != nil {
		return BackendConf{}, err
	}
	for _, b := range bs {
		if b.Name == name {
			return b, nil
		}
	}
	return BackendConf{}, fmt.Errorf("backend %q introuvable", name)
}

// sshSecretSteps (#194) : « clé ssh enregistrée » et « empreinte de l'hôte connue ». Sans clé enregistrée, ssh utilise
// l'agent et ~/.ssh du poste (v6.3.0) : ce n'est pas un échec.
func sshSecretSteps(name string) []TestStep {
	info, err := SecretGet(name)
	if err != nil {
		return []TestStep{{Label: "clé ssh enregistrée", OK: true, Detail: "backend non enregistré : agent ssh et ~/.ssh du poste"}}
	}
	if !info.HasKey {
		return []TestStep{{Label: "clé ssh enregistrée", OK: true, Detail: "aucune : agent ssh et ~/.ssh du poste"}}
	}
	out := []TestStep{{Label: "clé ssh enregistrée", OK: info.PublicKey != "", Detail: info.Fingerprint,
		Fix: "enregistrer à nouveau la paire (clé publique manquante)"}}
	if len(info.KnownHosts) == 0 {
		return append(out, TestStep{Label: "empreinte de l'hôte connue", Detail: "aucune empreinte enregistrée (StrictHostKeyChecking=yes)",
			Fix: "relever l'empreinte de l'hôte et la confirmer dans les réglages du backend"})
	}
	fps := make([]string, 0, len(info.KnownHosts))
	for _, k := range info.KnownHosts {
		fps = append(fps, k.Type+" "+k.Fingerprint)
	}
	return append(out, TestStep{Label: "empreinte de l'hôte connue", OK: true, Detail: strings.Join(fps, ", ")})
}
