package core

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// TestBackendTimeout borne le test entier (un ssh ou un cluster lent).
var TestBackendTimeout = 45 * time.Second

// TestBackend : étapes du test de b (pool : chaque serveur à son tour,
// étapes préfixées par le serveur). Au-delà du délai, une dernière étape
// « délai dépassé » conclut avec ce qui a pu être vérifié.
func TestBackend(b BackendConf) []TestStep {
	return testBackend(b, runtime.GOOS, runtimeEngineEnv(), exec.LookPath)
}

func testBackend(b BackendConf, goos string, ev engineEnv, look func(string) (string, error)) []TestStep {
	var steps []TestStep
	done := make(chan struct{})
	progress := make(chan TestStep, 64)
	go func() {
		defer close(done)
		defer close(progress)
		for _, st := range testSteps(b, goos, ev, look) {
			progress <- st
		}
	}()
	timer := time.NewTimer(TestBackendTimeout)
	defer timer.Stop()
	for {
		select {
		case st, ok := <-progress:
			if !ok {
				return steps
			}
			steps = append(steps, st)
		case <-timer.C:
			return append(steps, TestStep{Label: "délai", Detail: fmt.Sprintf("test interrompu après %d s", int(TestBackendTimeout.Seconds())),
				Fix: "le backend répond trop lentement : vérifier le réseau, le démon ou le cluster"})
		}
	}
}

// testSteps : le test lui-même (sans délai global).
func testSteps(b BackendConf, goos string, ev engineEnv, look func(string) (string, error)) []TestStep {
	if b.IsPool() {
		var all []TestStep
		for _, h := range b.Servers() {
			for _, st := range testSteps(b.At(h), goos, ev, look) {
				st.Label = h + " : " + st.Label
				all = append(all, st)
			}
		}
		return all
	}
	if b.Type == "host" {
		if p, refused := localRefusal(goos, b, ev); refused {
			return []TestStep{{Label: "exécution sur l'hôte", Detail: p.What, Fix: p.Fix}}
		}
		return []TestStep{{Label: "exécution sur l'hôte", OK: true, Detail: "aucun conteneur : rien à vérifier"}}
	}
	if p, refused := localRefusal(goos, b, ev); refused {
		return []TestStep{{Label: "backend utilisable sur ce système", Detail: p.What, Fix: p.Fix}}
	}
	var steps []TestStep
	missing := false
	for _, t := range toolsStatus(goos, []BackendConf{b}, ev, look) {
		if t.Optional || t.Tool == "git" { // ce que le backend exige, pas le launcher
			continue
		}
		st := TestStep{Label: "outil requis : " + t.Tool, OK: t.Found, Detail: t.Path}
		if !t.Found {
			st.Detail, st.Fix, missing = t.Tool+" absent de cette machine", t.Install, true
		}
		steps = append(steps, st)
	}
	if missing {
		return steps // sans l'outil, le reste ne peut pas être contrôlé
	}
	if b.Type == "k8s" {
		// k8sSteps rejoue le contrôle de kubectl : déjà fait ci-dessus
		for _, st := range k8sSteps(b) {
			if st.Label != "outil requis : kubectl" {
				steps = append(steps, st)
			}
		}
		return steps
	}
	if ps := engineProblems(b); len(ps) > 0 {
		for _, p := range ps {
			steps = append(steps, TestStep{Label: "moteur joignable", Detail: p.What, Fix: p.Fix})
		}
		return steps
	}
	steps = append(steps, TestStep{Label: "moteur joignable", OK: true, Detail: engineLabel(b)})
	switch ps := sharedProblems(b, ""); {
	case !b.remote():
		steps = append(steps, TestStep{Label: "stockage partagé", OK: true, Detail: "non requis (backend local)"})
	case len(ps) == 0:
		steps = append(steps, TestStep{Label: "stockage partagé", OK: true, Detail: strings.Join(b.Shared, ", ")})
	default:
		for _, p := range ps {
			steps = append(steps, TestStep{Label: "stockage partagé", Detail: p.What, Fix: p.Fix})
		}
	}
	return steps
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
		Engine: e.Engine, EngHost: e.EngineHost, Shared: e.Shared, Namespace: e.Namespace, Volumes: e.Volumes, Spread: e.Spread, Pod: e.Pod}}
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
