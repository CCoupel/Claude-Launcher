package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeDocker : faux client docker sur le PATH. État de chaque serveur (-H) dans
// $FAKE_DOCKER/<serveur>/<conteneur> = « statut|projet » ; un serveur listé dans
// $FAKE_DOWN ne répond pas. Commandes : ps -a -q, inspect -f, rm -f -v, version.
const fakeDockerScript = `#!/bin/bash
h=local; [ "$1" = -H ] && { h=$2; shift 2; }
d="$FAKE_DOCKER/$(printf %s "$h" | tr -c 'A-Za-z0-9\n' _)"
case " $FAKE_DOWN " in *" $h "*) echo "Cannot connect to the Docker daemon at $h" >&2; exit 1;; esac
mkdir -p "$d"
case "$1" in
  version) echo 27.0.0;;
  ps) ls "$d";;
  inspect) shift 3; for n in "$@"; do IFS='|' read -r st pj < "$d/$n"; echo "/$n|$st|$pj"; done;;
  rm) shift; for n in "$@"; do case $n in -*) ;; *) rm -f "$d/$n"; echo "$h $n" >> "$FAKE_DOCKER/removed";; esac; done;;
  *) echo "fake docker : $*" >&2; exit 2;;
esac
`

func withFakeDocker(t *testing.T) (state string, put func(host, name, status, proj string)) {
	t.Helper()
	bin := t.TempDir()
	state = t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER", state)
	t.Setenv("FAKE_DOWN", "")
	put = func(host, name, status, proj string) {
		d := filepath.Join(state, strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, host))
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, name), []byte(status+"|"+proj+"\n"), 0o644)
	}
	return state, put
}

func poolBackend() BackendConf {
	return BackendConf{Name: "pool", Type: "docker", Hosts: []string{"tcp://a:2375", "tcp://b:2375", "tcp://c:2375"}, Shared: []string{"/"}}
}

func answer(s string) (Asker, *bytes.Buffer) {
	var out bytes.Buffer
	return Asker{&out, bufio.NewReader(strings.NewReader(s))}, &out
}

func TestPoolMembers(t *testing.T) {
	b := poolBackend()
	m := b.At("tcp://b:2375")
	if m.Host != "tcp://b:2375" || m.Server != "tcp://b:2375" || m.IsPool() || m.Name != "pool" {
		t.Errorf("membre : %+v", m)
	}
	if single := (BackendConf{Host: "x"}); !b.remote() || len(b.Servers()) != 3 || single.At("y").Host != "x" {
		t.Error("groupe : distant, 3 serveurs ; hors groupe at() sans effet")
	}
	if got := (Engine{m}).cli("ps"); strings.Join(got, " ") != "docker -H tcp://b:2375 ps" {
		t.Errorf("commande du membre : %v", got)
	}
}

func TestPoolList(t *testing.T) {
	_, put := withFakeDocker(t)
	put("tcp://a:2375", "cl-p1", "running", "/p/p1")
	put("tcp://b:2375", "cl-p2", "exited", "/p/p2")
	put("tcp://b:2375", "cl-p3", "running", "/p/p3")
	t.Setenv("FAKE_DOWN", "tcp://c:2375")
	e := Engine{poolBackend()}
	fs, down := e.ListPool()
	if len(fs) != 3 || len(down) != 1 || down["tcp://c:2375"] == nil {
		t.Fatalf("listPool : %+v %v", fs, down)
	}
	srv := map[string]string{}
	for _, f := range fs {
		srv[f.Project] = f.Server
	}
	if srv["/p/p1"] != "tcp://a:2375" || srv["/p/p3"] != "tcp://b:2375" {
		t.Errorf("serveurs : %v", srv)
	}
	if l := LoadOf(e.B, fs, down); l["tcp://a:2375"] != 1 || l["tcp://b:2375"] != 1 {
		t.Errorf("charge (en marche seulement, injoignable exclu) : %v", l)
	}
	// list() : pas d'erreur tant qu'un serveur répond ; erreur si tous sont muets
	if _, err := e.List(); err != nil {
		t.Errorf("list avec un serveur muet : %v", err)
	}
	t.Setenv("FAKE_DOWN", "tcp://a:2375 tcp://b:2375 tcp://c:2375")
	if _, err := e.List(); err == nil {
		t.Error("list sans aucun serveur : erreur attendue")
	}
}

func TestPoolPlace(t *testing.T) {
	state, put := withFakeDocker(t)
	e := Engine{poolBackend()}
	proj := t.TempDir()
	// existant (même arrêté) : son serveur, sans verrou ni question
	put("tcp://c:2375", CtrName(proj), "exited", proj)
	a, _ := answer("")
	pe, unlock, err := e.Place(proj, a)
	unlock()
	if err != nil || pe.B.Host != "tcp://c:2375" {
		t.Fatalf("projet existant : %v %+v", err, pe.B)
	}

	// nouveau : serveur le moins chargé (conteneurs NG2 en marche)
	proj2 := t.TempDir()
	put("tcp://a:2375", "cl-x", "running", "/p/x")
	put("tcp://a:2375", "cl-y", "running", "/p/y")
	put("tcp://b:2375", "cl-z", "running", "/p/z")
	a, out := answer("")
	pe, unlock, err = e.Place(proj2, a)
	if err != nil || pe.B.Host != "tcp://c:2375" {
		t.Fatalf("nouveau projet : %v %+v", err, pe.B)
	}
	if _, err := os.Stat(lockPath(proj2)); err != nil {
		t.Error("verrou absent pendant la création")
	}
	unlock()
	if _, err := os.Stat(lockPath(proj2)); err == nil {
		t.Error("verrou non rendu")
	}
	if !strings.Contains(out.String(), "serveur choisi : tcp://c:2375") {
		t.Errorf("message : %q", out.String())
	}

	// serveur injoignable : question ; non → annulé, oui → le moins chargé des autres
	t.Setenv("FAKE_DOWN", "tcp://c:2375")
	proj3 := t.TempDir()
	a, out = answer("n\n")
	if _, unlock, err = e.Place(proj3, a); err == nil || !strings.Contains(out.String(), "tcp://c:2375 injoignable") {
		t.Errorf("injoignable, refus : %v %q", err, out.String())
	}
	unlock()
	if _, err := os.Stat(lockPath(proj3)); err == nil {
		t.Error("verrou laissé après annulation")
	}
	a, _ = answer("o\n")
	pe, unlock, err = e.Place(proj3, a)
	unlock()
	if err != nil || pe.B.Host != "tcp://b:2375" {
		t.Errorf("injoignable, accord : %v %+v", err, pe.B)
	}

	// doublon : on garde celui choisi, l'autre est supprimé
	t.Setenv("FAKE_DOWN", "")
	proj4 := t.TempDir()
	put("tcp://a:2375", CtrName(proj4), "running", proj4)
	put("tcp://b:2375", CtrName(proj4), "exited", proj4)
	a, out = answer("2\n")
	pe, unlock, err = e.Place(proj4, a)
	unlock()
	rm, _ := os.ReadFile(filepath.Join(state, "removed"))
	if err != nil || pe.B.Host != "tcp://b:2375" || !strings.Contains(string(rm), "tcp://a:2375 "+CtrName(proj4)) {
		t.Errorf("doublon : %v %+v supprimés=%q sortie=%q", err, pe.B, rm, out.String())
	}

	// aucun serveur : erreur claire
	t.Setenv("FAKE_DOWN", "tcp://a:2375 tcp://b:2375 tcp://c:2375")
	a, _ = answer("")
	if _, unlock, err = e.Place(t.TempDir(), a); err == nil || !strings.Contains(err.Error(), "aucun serveur") {
		t.Errorf("aucun serveur : %v", err)
	}
	unlock()

	// hors groupe : rien ne change
	single := Engine{BackendConf{Name: "d", Type: "docker", Host: "tcp://a:2375"}}
	if pe, unlock, err := single.Place(proj, a); err != nil || pe.B.Host != "tcp://a:2375" {
		t.Errorf("hors groupe : %v %+v", err, pe.B)
	} else {
		unlock()
	}
}

func TestLockProject(t *testing.T) {
	proj := t.TempDir()
	un, err := lockProject(proj, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockProject(proj, 600*time.Millisecond); err == nil || !strings.Contains(err.Error(), "autre launcher") {
		t.Errorf("verrou pris deux fois : %v", err)
	}
	un()
	// verrou abandonné (plus vieux que lockStale) : repris
	un, _ = lockProject(proj, time.Second)
	old := time.Now().Add(-2 * lockStale)
	_ = os.Chtimes(lockPath(proj), old, old)
	if un2, err := lockProject(proj, time.Second); err != nil {
		t.Errorf("verrou abandonné non repris : %v", err)
	} else {
		un2()
	}
	un()
}

func TestPoolConfig(t *testing.T) {
	d := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", d)
	cases := map[string]string{
		"pool: {type: k8s, context: c, namespace: n, hosts: [a]}":             "réservé à podman, docker et ssh",
		"pool: {type: docker, host: tcp://a:1, hosts: [tcp://b:1]}":           "pas les deux",
		"pool: {type: ssh, hosts: [srv1, srv1]}":                              "en double",
		"pool: {type: ssh, hosts: [srv1, srv2], engine: podman, shared: [/]}": "",
	}
	for y, want := range cases {
		_ = os.WriteFile(filepath.Join(d, "backends.yaml"), []byte("backends:\n  "+y+"\n"), 0o644)
		bs, err := LoadBackends()
		switch {
		case want == "" && err != nil:
			t.Errorf("%s : refusé : %v", y, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s : %v, attendu ~%q", y, err, want)
		case want == "":
			for _, b := range bs {
				if b.Name == "pool" && (!b.IsPool() || len(b.Servers()) != 2) {
					t.Errorf("groupe ssh : %+v", b)
				}
			}
		}
	}
}

func TestPoolDiagnose(t *testing.T) {
	withFakeDocker(t)
	root := t.TempDir()
	b := poolBackend()
	b.Shared = []string{root}
	t.Setenv("HOME", root) // ~/.claude sous la racine partagée
	_ = os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
	t.Setenv("FAKE_DOWN", "tcp://a:2375 tcp://b:2375")
	if ps := Diagnose(b); len(ps) != 0 {
		t.Errorf("un serveur sur trois répond : groupe utilisable attendu, %v", ps)
	}
	if pp := PoolProblems(b); len(pp) != 2 || pp["tcp://c:2375"] != nil {
		t.Errorf("problèmes par serveur : %v", pp)
	}
	t.Setenv("FAKE_DOWN", "tcp://a:2375 tcp://b:2375 tcp://c:2375")
	if ps := Diagnose(b); len(ps) < 3 || !strings.HasPrefix(ps[0].What, "tcp://a:2375 : ") {
		t.Errorf("aucun serveur : problèmes de chacun attendus, %v", ps)
	}
}
