package core

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Outils absents : le test s'arrête à l'étape « outil requis » avec la
// commande d'installation, sans aucun accès au réseau.
func TestTestBackendMissingTools(t *testing.T) {
	look := func(string) (string, error) { return "", fmt.Errorf("absent") }
	for _, tt := range []struct {
		b    BackendConf
		tool string
	}{
		{BackendConf{Name: "d", Type: "docker", Host: "tcp://10.0.0.1:2375"}, "docker"},
		{BackendConf{Name: "s", Type: "ssh", Host: "srv1"}, "ssh"},
		{BackendConf{Name: "k", Type: "k8s", Namespace: "x"}, "kubectl"},
		{BackendConf{Name: "p", Type: "podman"}, "podman"},
	} {
		steps := testBackend(tt.b, "linux", engineEnv{}, look)
		if len(steps) != 1 || steps[0].OK || steps[0].Label != "outil requis : "+tt.tool || steps[0].Fix == "" {
			t.Errorf("%s : %+v", tt.b.Name, steps)
		}
	}
	// host : rien à vérifier sous Linux ; refusé sous Windows
	if st := testBackend(BackendConf{Name: "host", Type: "host"}, "linux", engineEnv{}, look); len(st) != 1 || !st[0].OK {
		t.Errorf("host : %+v", st)
	}
	if st := testBackend(BackendConf{Name: "h2", Type: "host"}, "windows", engineEnv{}, look); len(st) != 1 || st[0].OK {
		t.Errorf("host sous Windows : %+v", st)
	}
	// pool : chaque serveur à son tour, étapes préfixées
	pool := BackendConf{Name: "pooled", Type: "ssh", Hosts: []string{"srv1", "srv2"}}
	st := testBackend(pool, "linux", engineEnv{}, look)
	if len(st) != 2 || !strings.HasPrefix(st[0].Label, "srv1 : ") || !strings.HasPrefix(st[1].Label, "srv2 : ") {
		t.Errorf("pool : %+v", st)
	}
}

// Délai : un test qui ne rend pas la main est interrompu, avec ce qui a été vu.
func TestTestBackendTimeout(t *testing.T) {
	old := TestBackendTimeout
	TestBackendTimeout = 150 * time.Millisecond
	t.Cleanup(func() { TestBackendTimeout = old })
	look := func(string) (string, error) { time.Sleep(2 * time.Second); return "", fmt.Errorf("lent") }
	t0 := time.Now()
	st := testBackend(BackendConf{Name: "d", Type: "docker", Host: "tcp://x:1"}, "linux", engineEnv{}, look)
	if time.Since(t0) > time.Second || len(st) == 0 || st[len(st)-1].Label != "délai" || st[len(st)-1].OK {
		t.Fatalf("délai : %v %+v", time.Since(t0), st)
	}
}

// k8s : sans kubectl dans le PATH, une seule étape d'échec ; le diagnostic
// existant (k8sProblems) en rend le même problème.
func TestK8sStepsNoKubectl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	b := BackendConf{Name: "k", Type: "k8s", Namespace: "x"}
	steps := k8sSteps(b)
	if len(steps) != 1 || steps[0].OK || !strings.Contains(steps[0].Detail, "kubectl absent") {
		t.Fatalf("étapes : %+v", steps)
	}
	if ps := k8sProblems(b); len(ps) != 1 || ps[0].What != steps[0].Detail || ps[0].Fix != steps[0].Fix {
		t.Fatalf("problèmes : %+v", ps)
	}
}

// Backend du formulaire (pas encore enregistré) : pool résolu, erreurs de forme
// de backendsOf rendues telles quelles.
func TestBackendEntryConf(t *testing.T) {
	pools := []PoolEntry{{Name: "labo", Hosts: []string{"srv1", "srv2"}, Drain: []string{"srv2"}}}
	b, err := BackendEntry{Name: "p", Type: "docker", Pool: "labo", Host: "tcp://{}:2375"}.Conf(pools)
	if err != nil || len(b.Hosts) != 2 || b.Hosts[0] != "tcp://srv1:2375" || !b.Drain["tcp://srv2:2375"] {
		t.Fatalf("pool : %+v %v", b, err)
	}
	if _, err := (BackendEntry{Type: "docker", Pool: "inconnu"}).Conf(pools); err == nil || !strings.Contains(err.Error(), "inconnu") {
		t.Fatalf("pool inconnu : %v", err)
	}
	if _, err := (BackendEntry{Type: "vapeur"}).Conf(pools); err == nil {
		t.Fatal("type inconnu accepté")
	}
}

// États depuis le cache du diagnostic : un backend en cache est rendu sans
// calcul ; un absent est « en cours » et diagnostiqué en tâche de fond.
func TestBackendStatesFromCache(t *testing.T) {
	ok := BackendConf{Name: "ok", Type: "docker", Host: "tcp://cache-ok:2375"}
	ko := BackendConf{Name: "ko", Type: "docker", Host: "tcp://cache-ko:2375"}
	diagCache.Lock()
	diagCache.m[fmt.Sprintf("%+v", ok)] = diagEntry{time.Now(), nil}
	diagCache.m[fmt.Sprintf("%+v", ko)] = diagEntry{time.Now(), []Problem{{"démon injoignable", "le démarrer"}}}
	diagCache.Unlock()
	st := BackendStates([]BackendConf{{Name: "host", Type: "host"}, ok, ko})
	if !st["host"].OK || !st["ok"].OK || st["ok"].Text != "joignable" || st["ko"].OK || st["ko"].Text != "démon injoignable" || st["ko"].Pending {
		t.Fatalf("états : %+v", st)
	}
	// absent : en cours (le diagnostic réel part en tâche de fond ; dans un test
	// sans docker il échoue vite, sans réseau : l'hôte est une adresse locale fermée)
	launched := make(chan string, 4)
	old := diagnoseBg
	diagnoseBg = func(b BackendConf) { launched <- b.Name }
	t.Cleanup(func() { diagnoseBg = old })
	cold := BackendConf{Name: "cold", Type: "docker", Host: "tcp://cold:1"}
	if s := BackendStates([]BackendConf{cold})["cold"]; !s.Pending || s.OK {
		t.Fatalf("absent : %+v", s)
	}
	select {
	case n := <-launched:
		if n != "cold" {
			t.Fatalf("diagnostic lancé pour %s", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("diagnostic de fond non lancé")
	}
}

func TestPoolLoads(t *testing.T) {
	mk := func(name string) BackendConf {
		return BackendConf{Name: name, Type: "docker", Pool: "labo", Machines: map[string]string{"tcp://s1:1": "s1", "tcp://s2:1": "s2"}}
	}
	bs := []BackendConf{mk("a"), mk("b"), {Name: "solo", Type: "docker"}}
	loads := PoolLoads(bs, []Found{
		{Backend: "a", Server: "tcp://s1:1", Running: true},
		{Backend: "b", Server: "tcp://s1:1", Running: true}, // charge commune : même machine, autre backend
		{Backend: "b", Server: "tcp://s1:1", Running: false},
		{Backend: "solo", Server: "", Running: true},
	})
	if len(loads) != 1 || loads["labo"]["s1"] != 2 || loads["labo"]["s2"] != 0 {
		t.Fatalf("charges : %v", loads)
	}
}
