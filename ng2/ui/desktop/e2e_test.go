package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Bout en bout de l'interface native sur un vrai backend : ouverture d'un
// projet en conteneur (core.OpenCtr : conteneur, bootstrap, session), attache
// en mode contrôle, sortie et saisie dans le pane du leader, fermeture
// (/exit, session, arrêt du conteneur). Mêmes variables que les e2e de core :
//
//	CL_E2E_POOL=<racine partagée>,tcp://srvA:2375[,…]   (docker, premier serveur)
//	CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>    (+ KUBECONFIG explicite)
//
// claude est remplacé dans le conteneur par un script (setup de env.yaml).
func TestE2EDesktopContainer(t *testing.T) {
	type target struct{ name, yaml, root string }
	var targets []target
	if spec := os.Getenv("CL_E2E_POOL"); spec != "" {
		f := strings.Split(spec, ",")
		targets = append(targets, target{"docker", fmt.Sprintf("  e2e-desk: { type: docker, host: %s, shared: [%s] }\n", f[1], f[0]), f[0]})
	}
	if spec := os.Getenv("CL_E2E_K8S"); spec != "" {
		f := strings.Split(spec, ",")
		if len(f) != 4 {
			t.Fatal("CL_E2E_K8S=<contexte>,<namespace>,<racine>,<pvc>")
		}
		targets = append(targets, target{"k8s", fmt.Sprintf("  e2e-desk: { type: k8s, context: %s, namespace: %s, volumes: { %s: %s } }\n", f[0], f[1], f[2], f[3]), f[2]})
	}
	if len(targets) == 0 {
		t.Skip("CL_E2E_POOL=… ou CL_E2E_K8S=… pour lancer ce test")
	}
	for _, tg := range targets {
		t.Run(tg.name, func(t *testing.T) { desktopE2E(t, tg.root, tg.yaml) })
	}
}

func desktopE2E(t *testing.T, root, backend string) {
	h := filepath.Join(root, ".cl-e2e-desk-"+strconv.Itoa(os.Getpid()))
	kube := os.Getenv("KUBECONFIG")
	ps := sandbox(t, h, "demo-desk")
	t.Cleanup(func() { os.RemoveAll(h) })
	if kube != "" {
		t.Setenv("KUBECONFIG", kube)
	}
	proj := ps[0]
	cfgDir := os.Getenv("CL_CONFIG_DIR")
	_ = os.WriteFile(filepath.Join(cfgDir, "backends.yaml"), []byte("backends:\n"+backend+
		"defaults:\n  browser_server: { listen: \"off\" }\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, ".claude-launcher", "env.yaml"), []byte(`image: docker.io/library/debian:12
setup:
  - |
    mkdir -p "$HOME/.local/bin"
    printf '%s\n' '#!/bin/sh' 'echo faux claude prêt' 'while read -r l; do echo "reçu:$l"; [ "$l" = /exit ] && exit 0; done' > "$HOME/.local/bin/claude"
    chmod 755 "$HOME/.local/bin/claude"
`), 0o644)
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	if err := core.WriteAssignment(proj, "e2e-desk"); err != nil {
		t.Fatal(err)
	}
	b, ok := core.FindBackend("e2e-desk")
	if !ok {
		t.Fatal("backend e2e-desk introuvable")
	}
	e, _ := core.EngineOf(b)
	t.Cleanup(func() {
		if b.K8s() {
			_ = exec.Command("kubectl", "--context", b.Context, "-n", b.Namespace, "delete", "statefulset", core.CtrName(proj), "--wait=false").Run()
		} else {
			_ = exec.Command("docker", "-H", b.Host, "rm", "-f", "-v", core.CtrName(proj)).Run()
		}
	})

	var r recorder
	a := NewApp(r.emit)
	if _, err := a.open(proj, core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	a.term(proj).resize(140, 40)
	launchLog := func() string {
		pt := a.term(proj)
		if pt == nil {
			return ""
		}
		pt.launchMu.Lock()
		defer pt.launchMu.Unlock()
		return pt.launchLog.String()
	}
	end := time.Now().Add(6 * time.Minute)
	for time.Now().Before(end) {
		if pt := a.term(proj); pt != nil && pt.info().State == "ready" {
			break
		}
		if pt := a.term(proj); pt != nil && pt.info().State == "error" {
			t.Fatalf("ouverture : %s\n%s", pt.errMsg, launchLog())
		}
		time.Sleep(time.Second)
	}
	if pt := a.term(proj); pt == nil || pt.info().State != "ready" {
		t.Fatalf("pas prêt en 6 min :\n%s", launchLog())
	}
	t.Logf("ouverture :\n%s", launchLog())
	r.wait(t, 30*time.Second, "layout", isTerm(proj, "layout", nil))
	wait := func(s string) {
		t.Helper()
		end := time.Now().Add(60 * time.Second)
		for time.Now().Before(end) && !strings.Contains(r.output(proj), s) {
			time.Sleep(200 * time.Millisecond)
		}
		if !strings.Contains(r.output(proj), s) {
			t.Fatalf("%q absent de la sortie : %q", s, r.output(proj))
		}
	}
	wait("faux claude prêt")
	pt := a.term(proj)
	pt.input(pt.lastLayout().Leader, "bonjour é\r")
	wait("reçu:bonjour é")
	if o := e.Owner(pt.ctr); o.Id != core.LauncherID() {
		t.Errorf("propriétaire %q, attendu %q", o.Id, core.LauncherID())
	}

	// fermeture : /exit, session fermée, conteneur arrêté
	a.readCtr()
	res, err := a.askClose(proj)
	q, _ := res.(map[string]any)["question"].(string)
	if err != nil || !strings.HasPrefix(q, "Fermer demo-desk ? claude") {
		t.Fatalf("question de fermeture : %v %v", res, err)
	}
	if err := a.confirmClose(proj); err != nil {
		t.Fatal(err)
	}
	end = time.Now().Add(3 * time.Minute)
	for time.Now().Before(end) {
		a.mu.Lock()
		_, busy := a.closing[proj]
		msg := a.msg
		a.mu.Unlock()
		if !busy && strings.HasPrefix(msg, "✓") {
			break
		}
		if !busy && strings.HasPrefix(msg, "⚠") {
			t.Fatalf("fermeture : %s", msg)
		}
		time.Sleep(time.Second)
	}
	if a.term(proj) != nil {
		t.Error("onglet encore ouvert après la fermeture")
	}
	// k8s : StatefulSet à 0 réplique, le pod met quelques secondes à partir
	running := func() *core.Found {
		fs, _ := e.List()
		for _, f := range fs {
			if f.Project == proj && f.Running {
				return &f
			}
		}
		return nil
	}
	end = time.Now().Add(2 * time.Minute)
	for running() != nil && time.Now().Before(end) {
		time.Sleep(2 * time.Second)
	}
	if f := running(); f != nil {
		t.Errorf("conteneur encore en marche : %+v", *f)
	}
}
