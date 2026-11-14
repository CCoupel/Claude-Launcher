package core

import (
	"fmt"
	"runtime"
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
	diagCache.m[backendKey(ok)] = diagEntry{time.Now(), nil}
	diagCache.m[backendKey(ko)] = diagEntry{time.Now(), []Problem{{"démon injoignable", "le démarrer"}}}
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

// Clé du cache de diagnostic : deux lectures du même fichier donnent des
// backends égaux dont les pointeurs (spread) diffèrent ; la clé ne doit pas en
// dépendre, sinon le diagnostic (kubectl) serait relancé à chaque lecture.
func TestBackendKeyIgnoresPointers(t *testing.T) {
	yes, yes2, no := true, true, false
	mk := func(sp *bool) BackendConf {
		return BackendConf{Name: "lab", Type: "k8s", Context: "k8s", Namespace: "x", Spread: sp,
			Volumes: map[string]string{"/a": "p1", "/b": "p2"}, Pod: map[string]any{"nodeSelector": map[string]any{"a": "b"}}}
	}
	if backendKey(mk(&yes)) != backendKey(mk(&yes2)) {
		t.Fatal("clés différentes pour des valeurs égales")
	}
	if backendKey(mk(&yes)) == backendKey(mk(&no)) || backendKey(mk(&yes)) == backendKey(mk(nil)) {
		t.Fatal("spread true / false / absent confondus")
	}
	// le cache rend la même entrée pour l'autre lecture : pas de second diagnostic
	diagCache.Lock()
	diagCache.m[backendKey(mk(&yes))] = diagEntry{time.Now(), nil}
	diagCache.Unlock()
	if ps, ok := peekDiagnose(mk(&yes2)); !ok || ps != nil {
		t.Fatalf("cache manqué : %v %v", ps, ok)
	}
	launched := make(chan string, 2)
	old := diagnoseBg
	diagnoseBg = func(b BackendConf) { launched <- b.Name }
	t.Cleanup(func() { diagnoseBg = old })
	if s := BackendStates([]BackendConf{mk(&yes2)})["lab"]; s.Pending || !s.OK {
		t.Fatalf("état : %+v", s)
	}
	select {
	case n := <-launched:
		t.Fatalf("diagnostic relancé pour %s", n)
	case <-time.After(200 * time.Millisecond):
	}
}

// Une adresse ou un contexte qui commence par « - » serait pris pour une
// option : refusé par la validation et avant tout test, et « -- » précède la
// destination dans les commandes ssh.
func TestOptionLikeAddressRefused(t *testing.T) {
	for _, y := range []string{
		"backends:\n  s: { type: ssh, host: '-oProxyCommand=touch /tmp/x' }\n",
		"backends:\n  d: { type: docker, host: '-H' }\n",
		"backends:\n  d: { type: docker, hosts: ['tcp://a:1', '-x'] }\n",
		"backends:\n  s: { type: ssh, host: srv1, engine_host: '--evil' }\n",
		"backends:\n  k: { type: k8s, context: '--kubeconfig=/etc/passwd' }\n",
	} {
		ps := ValidateConfig(ConfBackends, []byte(y))
		if !HasErrors(ps) || !strings.Contains(ps[0].Message, "commencer par « - »") {
			t.Errorf("accepté : %s → %+v", y, ps)
		}
	}
	for _, e := range []BackendEntry{{Type: "ssh", Host: "-oProxyCommand=x"}, {Type: "k8s", Context: "-x"}, {Type: "docker", Hosts: []string{"-y"}}} {
		if _, err := e.Conf(nil); err == nil {
			t.Errorf("backendTest : %+v accepté", e)
		}
	}
	// pool : une machine en « - » donne une adresse en « - »
	if _, err := (BackendEntry{Type: "ssh", Pool: "p"}).Conf([]PoolEntry{{Name: "p", Hosts: []string{"-bad"}}}); err == nil {
		t.Error("machine de pool « -bad » acceptée")
	}
	argv := Engine{BackendConf{Type: "ssh", Host: "srv1"}}.cli("info")
	i := -1
	for k, a := range argv {
		if a == "srv1" {
			i = k
		}
	}
	if i < 1 || argv[i-1] != "--" {
		t.Errorf("« -- » absent avant la destination : %v", argv)
	}
}

// Le délai ne laisse pas la goroutine du test bloquée : même avec plus d'étapes
// que n'en contiendrait un canal tamponné, elle va au bout et s'arrête.
func TestTestBackendTimeoutLeavesNoGoroutine(t *testing.T) {
	old := TestBackendTimeout
	TestBackendTimeout = 5 * time.Millisecond
	t.Cleanup(func() { TestBackendTimeout = old })
	hosts := make([]string, 200)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("srv%d", i)
	}
	look := func(string) (string, error) { time.Sleep(time.Millisecond); return "", fmt.Errorf("absent") }
	before := runtime.NumGoroutine()
	st := testBackend(BackendConf{Name: "p", Type: "ssh", Hosts: hosts}, "linux", engineEnv{}, look)
	if len(st) == 0 || st[len(st)-1].Label != "délai" {
		t.Fatalf("délai : %d étapes", len(st))
	}
	if len(st) >= 200 {
		t.Fatalf("le test n'a pas été interrompu : %d étapes", len(st))
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Fatalf("goroutine du test encore là : %d > %d", n, before)
	}
}
