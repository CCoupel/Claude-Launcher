package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Bout en bout (CL_E2E_PODMAN=1, ou CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>
// avec une racine sur le stockage partagé) : dossiers « local » masqués et à
// l'utilisateur, setup rejoué seulement si un fichier watch change, on_start.
func TestE2ELocalWatchOnStart(t *testing.T) {
	k8sSpec := os.Getenv("CL_E2E_K8S")
	if os.Getenv("CL_E2E_PODMAN") != "1" && k8sSpec == "" {
		t.Skip("CL_E2E_PODMAN=1 ou CL_E2E_K8S=… pour lancer ce test")
	}
	realData := os.Getenv("XDG_DATA_HOME")
	if realData == "" {
		realData = filepath.Join(os.Getenv("HOME"), ".local", "share") // cache d'images réel
	}
	h := withHome(t)
	e := Engine{BackendConf{Name: "podman", Type: "podman"}}
	if k8sSpec != "" {
		f := strings.Split(k8sSpec, ",")
		if len(f) != 4 {
			t.Fatal("CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>")
		}
		h = filepath.Join(f[2], ".cl-e2e-"+strconv.Itoa(os.Getpid()))
		t.Setenv("HOME", h)
		t.Cleanup(func() { os.RemoveAll(h) })
		e = Engine{BackendConf{Name: "e2e", Type: "k8s", Context: f[0], Namespace: f[1], Volumes: map[string]string{f[2]: f[3]}, Shared: []string{f[2]}}}
	}
	t.Setenv("XDG_DATA_HOME", realData)
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	_ = os.MkdirAll(filepath.Join(h, ".claude"), 0o755)
	proj := filepath.Join(h, "PROJETS", "TEST", "demo-local")
	_ = os.MkdirAll(proj, 0o755)
	_ = os.WriteFile(filepath.Join(proj, "lock.txt"), []byte("v1"), 0o644)
	env := func(local string) EnvSpec {
		writeEnvFile(t, proj, "image: docker.io/library/debian:12\nlocal: ["+local+"]\nwatch: [lock.txt]\n"+
			"setup: [\"echo setup >> setup.log\"]\non_start: [\"echo start >> start.log\"]\n")
		e, err := LoadEnv(proj)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	name := CtrName(proj)
	if e.B.K8s() {
		name = k8sPod(proj)
		defer e.kcmd("delete", "statefulset", k8sName(proj), "--wait=false").Run()
	} else {
		defer exec.Command("podman", "rm", "-f", "-v", name).Run()
	}
	lines := func(f string) int {
		b, _ := os.ReadFile(filepath.Join(proj, f))
		return strings.Count(string(b), "\n")
	}
	run := func(en EnvSpec) string {
		t.Helper()
		var out bytes.Buffer
		n, err := e.Ensure(proj, en, &out)
		if err != nil {
			t.Fatalf("ensure : %v\n%s", err, out.String())
		}
		name = n
		if err := e.Bootstrap(name, proj, en, &out); err != nil {
			t.Fatalf("bootstrap : %v\n%s", err, out.String())
		}
		return out.String()
	}
	en := env("node_modules")
	run(en)
	if lines("setup.log") != 1 {
		t.Fatalf("setup après bootstrap complet : %d", lines("setup.log"))
	}
	// hostname : le projet (conteneur) ; <catégorie>-<projet>-0 (pod de StatefulSet)
	var out bytes.Buffer
	if err := e.Exec(name, false, nil, &out, "hostname"); err != nil || strings.TrimSpace(out.String()) != map[bool]string{true: "test-demo-local-0", false: "demo-local"}[e.B.K8s()] {
		t.Errorf("hostname : %q %v", out.String(), err)
	}
	out.Reset()
	// local : à l'utilisateur, et masqué (rien n'arrive dans le dossier partagé)
	if err := e.Exec(name, false, nil, &out, "sh", "-c", "touch node_modules/x && stat -c %u node_modules"); err != nil {
		t.Fatalf("écriture dans local : %v %s", err, out.String())
	}
	if strings.TrimSpace(out.String()) != itoa(os.Getuid()) {
		t.Errorf("propriétaire de node_modules : %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "node_modules", "x")); err == nil {
		t.Error("node_modules/x visible côté hôte : dossier non masqué")
	}
	if s := run(en); lines("setup.log") != 1 || !strings.Contains(s, "bootstrap à jour") {
		t.Errorf("rien n'a changé : setup rejoué (%d) ou message %q", lines("setup.log"), s)
	}
	_ = os.WriteFile(filepath.Join(proj, "lock.txt"), []byte("v2"), 0o644)
	if s := run(en); lines("setup.log") != 2 || !strings.Contains(s, "fichiers surveillés modifiés") || strings.Contains(s, "paquets système") {
		t.Errorf("watch : setup %d, sortie %q", lines("setup.log"), s)
	}
	if err := e.OnStart(name, proj, en); err != nil || lines("start.log") != 1 {
		t.Errorf("on_start : %v, %d", err, lines("start.log"))
	}
	// liste local changée : conteneur recréé, volume neuf (x disparu)
	s := run(env("node_modules, .venv"))
	if !strings.Contains(s, "dossiers « local » changés") {
		t.Errorf("recréation attendue : %q", s)
	}
	out.Reset()
	_ = e.Exec(name, false, nil, &out, "sh", "-c", "ls -A node_modules; test -O .venv && echo venv-ok")
	if strings.Contains(out.String(), "x") || !strings.Contains(out.String(), "venv-ok") {
		t.Errorf("après recréation : %q", out.String())
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
