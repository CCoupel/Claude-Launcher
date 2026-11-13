package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBackends(t *testing.T, y string) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	if err := os.WriteFile(filepath.Join(d, "backends.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
}

func backendNamed(bs []BackendConf, n string) BackendConf {
	for _, b := range bs {
		if b.Name == n {
			return b
		}
	}
	return BackendConf{}
}

const poolsYAML = `pools:
  dev:
    hosts: [a, b, c]
    drain: [c]
backends:
  docker-dev: { type: docker, pool: dev, host: "tcp://{}:2375", shared: [/] }
  other-dev:  { type: docker, pool: dev, host: "tcp://{}:2376", shared: [/] }
  ssh-dev:    { type: ssh, pool: dev, host: "cyril@{}", shared: [/] }
`

func TestNamedPools(t *testing.T) {
	writeBackends(t, poolsYAML)
	bs, err := LoadBackends()
	if err != nil {
		t.Fatal(err)
	}
	d := backendNamed(bs, "docker-dev")
	if strings.Join(d.Hosts, " ") != "tcp://a:2375 tcp://b:2375 tcp://c:2375" || d.Host != "" || !d.IsPool() {
		t.Errorf("adresses : %+v", d)
	}
	if !d.Drain["tcp://c:2375"] || d.Drain["tcp://a:2375"] || d.machine("tcp://b:2375") != "b" {
		t.Errorf("drain / machines : %v %v", d.Drain, d.Machines)
	}
	if s := backendNamed(bs, "ssh-dev"); s.Hosts[0] != "cyril@a" || len(s.Mates) != 2 {
		t.Errorf("ssh : %+v", s)
	}
	if len(d.Mates) != 2 || d.Mates[0].Mates != nil {
		t.Errorf("pool partagé : %d autres backends", len(d.Mates))
	}
	if m := d.At("tcp://b:2375"); m.Host != "tcp://b:2375" || m.IsPool() {
		t.Errorf("membre : %+v", m)
	}
}

func TestNamedPoolsErrors(t *testing.T) {
	cases := map[string]string{
		"pools:\n  dev: [a, b]\nbackends: {}\n":                                                         "pas une liste",
		"pools:\n  dev: { hosts: [a], drains: [a] }\nbackends: {}\n":                                    "inconnue",
		"pools:\n  dev: { hosts: [] }\nbackends: {}\n":                                                  "sans machine",
		"pools:\n  dev: { hosts: [a, a] }\nbackends: {}\n":                                              "en double",
		"pools:\n  dev: { hosts: [a], drain: [z] }\nbackends: {}\n":                                     "absent",
		"pools:\n  dev: { hosts: [a] }\nbackends:\n  x: { type: docker, pool: nope }\n":                 "inconnu",
		"pools:\n  dev: { hosts: [a] }\nbackends:\n  x: { type: k8s, pool: dev }\n":                     "réservé",
		"pools:\n  dev: { hosts: [a] }\nbackends:\n  x: { type: docker, pool: dev, hosts: [b] }\n":      "pas les deux",
		"pools:\n  dev: { hosts: [a] }\nbackends:\n  x: { type: docker, pool: dev, host: tcp://a:1 }\n": "modèle",
	}
	for y, want := range cases {
		writeBackends(t, y)
		if _, err := LoadBackends(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q : %v, attendu ~%q", y, err, want)
		}
	}
	// sans host : l'entrée du pool est l'adresse
	writeBackends(t, "pools:\n  dev: { hosts: [tcp://a:1, tcp://b:1] }\nbackends:\n  x: { type: docker, pool: dev }\n")
	if bs, err := LoadBackends(); err != nil || backendNamed(bs, "x").Hosts[1] != "tcp://b:1" {
		t.Errorf("adresses complètes : %v", err)
	}
}

func TestNamedPoolsPlace(t *testing.T) {
	_, put := withFakeDocker(t)
	// sans ssh-dev : pas de vraie connexion ssh dans ce test
	writeBackends(t, strings.Replace(poolsYAML, "  ssh-dev:", "  #ssh-dev:", 1))
	bs, err := LoadBackends()
	if err != nil {
		t.Fatal(err)
	}
	e := Engine{backendNamed(bs, "docker-dev")}
	// a : 1 conteneur docker-dev ; b : 0 docker-dev mais 2 d'un autre backend du pool
	put("tcp://a:2375", "cl-x", "running", "/p/x")
	put("tcp://b:2376", "cl-y", "running", "/p/y")
	put("tcp://b:2376", "cl-z", "running", "/p/z")
	proj := t.TempDir()
	a, out := answer("")
	pe, unlock, err := e.Place(proj, a)
	unlock()
	// c est vide mais en drain ; charge commune : a=1, b=2 → a
	if err != nil || pe.B.Host != "tcp://a:2375" || !strings.Contains(out.String(), "tous backends du pool") {
		t.Fatalf("charge commune + drain : %v %+v %q", err, pe.B, out.String())
	}
	// projet existant sur une machine en drain : il y reste
	proj2 := t.TempDir()
	put("tcp://c:2375", CtrName(proj2), "exited", proj2)
	if pe, unlock, err = e.Place(proj2, a); err != nil || pe.B.Host != "tcp://c:2375" {
		t.Errorf("projet sur machine en drain : %v %+v", err, pe.B)
	}
	unlock()
	// tout en drain : nouveau projet refusé
	writeBackends(t, "pools:\n  p: { hosts: [a], drain: [a] }\nbackends:\n  d: { type: docker, pool: p, host: \"tcp://{}:2375\", shared: [/] }\n")
	bs, _ = LoadBackends()
	if _, unlock, err = (Engine{backendNamed(bs, "d")}).Place(t.TempDir(), a); err == nil || !strings.Contains(err.Error(), "tous en drain") {
		t.Errorf("tout en drain : %v", err)
	}
	unlock()
}
