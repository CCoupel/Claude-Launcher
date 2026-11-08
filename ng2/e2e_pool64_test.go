package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Bout en bout d'un pool nommé (#64), mêmes serveurs que TestE2EPool :
//
//	CL_E2E_POOL=<racine partagée>,tcp://srvA:2375,tcp://srvB:2375,tcp://srvC:2375
//
// deux backends docker dans le même pool (charge commune), dernière machine en
// drain. Conteneurs étiquetés cl.backend=e2e64-a / e2e64-b, supprimés à la fin.
func TestE2ENamedPool(t *testing.T) {
	spec := os.Getenv("CL_E2E_POOL")
	if spec == "" {
		t.Skip("CL_E2E_POOL=<racine>,tcp://…,tcp://…,tcp://… pour lancer ce test")
	}
	f := strings.Split(spec, ",")
	root, urls := f[0], f[1:]
	if len(urls) < 3 {
		t.Fatal("au moins trois serveurs")
	}
	var ms []string
	for _, u := range urls {
		ms = append(ms, strings.TrimSuffix(strings.TrimPrefix(u, "tcp://"), ":2375"))
	}
	h := filepath.Join(root, ".cl-e2e-pool64-"+strconv.Itoa(os.Getpid()))
	t.Setenv("HOME", h)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(h, ".claude"))
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	writeBackends(t, "pools:\n  lab:\n    hosts: ["+strings.Join(ms, ", ")+"]\n    drain: ["+ms[len(ms)-1]+"]\n"+
		"backends:\n"+
		"  e2e64-a: { type: docker, pool: lab, host: \"tcp://{}:2375\", shared: ["+root+"] }\n"+
		"  e2e64-b: { type: docker, pool: lab, host: \"tcp://{}:2375\", shared: ["+root+"] }\n")
	bs, err := loadBackends()
	if err != nil {
		t.Fatal(err)
	}
	A, B := Engine{backendNamed(bs, "e2e64-a")}, Engine{backendNamed(bs, "e2e64-b")}
	t.Cleanup(func() {
		for _, e := range []Engine{A, B} {
			fs, _ := e.listPool()
			for _, c := range fs {
				_ = e.of(c).cmd("rm", "-f", "-v", c.Name).Run()
			}
		}
		os.RemoveAll(h)
	})
	env := EnvSpec{Image: "docker.io/library/debian:12"}
	open := func(e Engine, n string) string {
		t.Helper()
		p := filepath.Join(h, "PROJETS", "TEST", n)
		_ = os.MkdirAll(p, 0o755)
		a, out := answer("")
		pe, unlock, err := e.place(p, a)
		if err != nil {
			unlock()
			t.Fatalf("place %s : %v\n%s", n, err, out.String())
		}
		_, err = pe.ensure(p, env, &strings.Builder{})
		unlock()
		if err != nil {
			t.Fatalf("ensure %s : %v", n, err)
		}
		t.Logf("%s (%s) → %s : %s", n, e.b.Name, pe.b.Server, strings.TrimSpace(out.String()))
		return pe.b.Server
	}
	// A, puis B : B voit la charge de A sur la 1re machine → 2e machine
	s1 := open(A, "a1")
	s2 := open(B, "b1")
	if s1 != urls[0] || s2 != urls[1] {
		t.Errorf("charge commune : a1 sur %s, b1 sur %s (attendu %s, %s)", s1, s2, urls[0], urls[1])
	}
	// égalité 1/1 : ordre du pool ; jamais la machine en drain
	for i := 0; i < 2; i++ {
		if s := open(A, "a"+strconv.Itoa(i+2)); s == urls[len(urls)-1] {
			t.Errorf("projet placé sur la machine en drain %s", s)
		}
	}
}
