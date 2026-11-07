package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Bout en bout d'un groupe de serveurs docker (#61) :
//
//	CL_E2E_POOL=<racine partagée>,tcp://srvA:2375,tcp://srvB:2375[,…]
//
// répartition, serveur gardé, deux ouvertures simultanées, doublon, serveur
// injoignable. Conteneurs étiquetés cl.backend=e2e-pool, supprimés à la fin.
func TestE2EPool(t *testing.T) {
	spec := os.Getenv("CL_E2E_POOL")
	if spec == "" {
		t.Skip("CL_E2E_POOL=<racine>,tcp://…,tcp://… pour lancer ce test")
	}
	f := strings.Split(spec, ",")
	root, hosts := f[0], f[1:]
	if len(hosts) < 2 {
		t.Fatal("au moins deux serveurs")
	}
	h := filepath.Join(root, ".cl-e2e-pool-"+strconv.Itoa(os.Getpid()))
	t.Setenv("HOME", h)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(h, ".claude"))
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	b := BackendConf{Name: "e2e-pool", Type: "docker", Hosts: hosts, Shared: []string{root}}
	e := Engine{b}
	t.Cleanup(func() {
		fs, _ := e.listPool()
		for _, c := range fs {
			_ = e.of(c).cmd("rm", "-f", "-v", c.Name).Run()
		}
		os.RemoveAll(h)
	})
	env := EnvSpec{Image: "docker.io/library/debian:12"}
	proj := func(n string) string {
		p := filepath.Join(h, "PROJETS", "TEST", n)
		_ = os.MkdirAll(p, 0o755)
		return p
	}
	open := func(e Engine, p, reply string) (Engine, string) {
		t.Helper()
		a, out := answer(reply)
		pe, unlock, err := e.place(p, a)
		if err != nil {
			unlock()
			t.Fatalf("place %s : %v\n%s", filepath.Base(p), err, out.String())
		}
		var log bytes.Buffer
		_, err = pe.ensure(p, env, &log)
		unlock()
		if err != nil {
			t.Fatalf("ensure %s sur %s : %v\n%s", filepath.Base(p), pe.b.Server, err, log.String())
		}
		return pe, out.String()
	}
	where := func(p string) []string {
		fs, _ := Engine{b}.listPool() // vue d'un autre launcher : rien en mémoire
		var s []string
		for _, c := range fs {
			if c.Project == p {
				s = append(s, c.Server)
			}
		}
		return s
	}

	// 1. répartition : un projet par serveur, dans l'ordre des moins chargés
	var ps []string
	seen := map[string]bool{}
	for i := 0; i < len(hosts); i++ {
		p := proj("p" + strconv.Itoa(i))
		ps = append(ps, p)
		pe, _ := open(e, p, "")
		seen[pe.b.Server] = true
	}
	if len(seen) != len(hosts) {
		t.Fatalf("répartition : %d serveurs utilisés pour %d projets (%v)", len(seen), len(hosts), seen)
	}
	t.Logf("répartition : %v", seen)

	// 2. un autre launcher retrouve chaque projet et son serveur
	for _, p := range ps {
		if w := where(p); len(w) != 1 {
			t.Errorf("%s vu sur %v", filepath.Base(p), w)
		}
	}

	// 3. serveur gardé, même conteneur arrêté
	first := where(ps[0])[0]
	pe, _ := open(e, ps[0], "")
	_ = pe.cmd("stop", "-t", "1", ctrName(ps[0])).Run()
	pe, _ = open(e, ps[0], "")
	if pe.b.Server != first || len(where(ps[0])) != 1 {
		t.Errorf("serveur non gardé : %s → %s (%v)", first, pe.b.Server, where(ps[0]))
	}

	// 4. deux launchers ouvrent le même nouveau projet au même instant : un seul conteneur
	p4 := proj("simultane")
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); open(Engine{b}, p4, "") }()
	}
	wg.Wait()
	if w := where(p4); len(w) != 1 {
		t.Errorf("ouverture simultanée : conteneur sur %v", w)
	}

	// 5. doublon : le même projet créé à la main sur un autre serveur
	other := hosts[0]
	if other == first {
		other = hosts[1]
	}
	args, err := Engine{b.at(other)}.runArgs(ctrName(ps[0]), ps[0], env)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := (Engine{b.at(other)}).cmd(args...).CombinedOutput(); err != nil {
		t.Fatalf("doublon : %v %s", err, out)
	}
	if w := where(ps[0]); len(w) != 2 {
		t.Fatalf("doublon non vu : %v", w)
	}
	keep, k := 1, 0 // garder celui de « first » : son rang dans la liste triée
	fs, _ := e.listPool()
	for _, c := range fs {
		if c.Project == ps[0] {
			k++
			if c.Server == first {
				keep = k
			}
		}
	}
	pe, out := open(e, ps[0], strconv.Itoa(keep)+"\n")
	if pe.b.Server != first || !strings.Contains(out, "doublon supprimé sur "+other) || len(where(ps[0])) != 1 {
		t.Errorf("doublon : gardé %s, attendu %s ; %v\n%s", pe.b.Server, first, where(ps[0]), out)
	}

	// 6. serveur injoignable dans le groupe : question, puis démarrage ailleurs
	bad := b
	bad.Hosts = append([]string{"tcp://192.0.2.1:2375"}, hosts...) // adresse de documentation : jamais joignable
	p6 := proj("injoignable")
	pe, out = open(Engine{bad}, p6, "o\n")
	if !strings.Contains(out, "192.0.2.1:2375 injoignable") || pe.b.Server == "tcp://192.0.2.1:2375" {
		t.Errorf("serveur injoignable : %s\n%s", pe.b.Server, out)
	}
}
