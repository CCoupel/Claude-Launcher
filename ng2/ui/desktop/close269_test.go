package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #269 — un pod k8s en marche à l'ancien modèle de montage (StatefulSet sans cl.mounts) doit pouvoir être fermé :
// Fermer et Forcer fermer exécutent « scale statefulset <nom> --replicas=0 » ; la carte « Montages d'avant v7.0.0 » reste affichée.
// Faux kubectl en tête du PATH, qui journalise ses appels : jamais de vrai cluster, jamais de vrai projet.

type t269 struct {
	a     *App
	proj  string
	calls string // journal du faux kubectl
}

func (x t269) scaled() bool {
	b, _ := os.ReadFile(x.calls)
	return strings.Contains(string(b), "scale statefulset p-a --replicas=0")
}

func (x t269) reset() { _ = os.Remove(x.calls) }

func t269Setup(t *testing.T, stsMounts string) t269 {
	t.Helper()
	unixOnlyDesktop(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	root, err := os.MkdirTemp("", "cl269-")
	if err != nil {
		t.Fatal(err)
	}
	ps := sandbox(t, root, "p")
	proj := ps[0]
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + calls + `"
while [ "$1" = "-n" ] || [ "$1" = "--context" ] || [ "$1" = "--kubeconfig" ]; do shift 2; done
case "$1 $2" in
  "auth can-i") echo yes ;;
  "get namespace") echo namespace/ns ;;
  "get pvc") printf 'Bound ["ReadWriteMany"]' ;;
  "get statefulsets")
    case "$*" in
      *readyReplicas*) printf 'p-a|1|1|%s|` + stsMounts + `\n' "` + proj + `" ;;
      *) printf 'p-a|1|` + stsMounts + ` ' ;;
    esac ;;
  *) echo "{}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.MkdirAll(filepath.Dir(core.EnvPath(proj)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.EnvPath(proj), []byte("image: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	y := "defaults:\n  browser_server: { listen: off }\nbackends:\n  k269:\n    type: k8s\n    namespace: ns\n    volumes:\n      " + root + ": pvc-x\n"
	if err := os.MkdirAll(filepath.Dir(core.BackendsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core.Ng2ConfigDir(), "projects.json"), []byte(`{"`+proj+`":"k269"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var r recorder
	a := newApp(t, r.emit)
	a.readCtr()
	return t269{a, proj, calls}
}

func (x t269) info(t *testing.T) core.ProjInfo {
	t.Helper()
	x.a.mu.Lock()
	defer x.a.mu.Unlock()
	n, ok := x.a.node(x.proj)
	if !ok {
		t.Fatal("projet absent de l'arbre")
	}
	return x.a.projInfo(n)
}

func TestClose269CarteMontagesAncienModeleReste(t *testing.T) {
	x := t269Setup(t, "")
	if ps := x.a.ctr.Diag["k269"]; len(ps) != 0 {
		t.Fatalf("diagnostic bloquant pour un pod à l'ancien modèle : %v", ps)
	}
	p := x.info(t)
	if !p.CtrKnown || !p.Ctr.Running || !p.Ctr.OldMounts {
		t.Fatalf("conteneur non connu / non marqué ancien modèle : %+v", p)
	}
	if c := x.a.cardsFast(x.proj); c.Warn != core.OldMountsNote {
		t.Errorf("carte « Montages d'avant v7.0.0 » absente : Warn = %q", c.Warn)
	}
}

func TestClose269FermerArreteLePod(t *testing.T) {
	x := t269Setup(t, "")
	p := x.info(t)
	b, ok := core.FindBackend(p.Backend)
	if !ok || !p.CtrKnown {
		t.Fatalf("backend / conteneur introuvables : %q %+v", p.Backend, p)
	}
	e, _ := core.EngineOf(b)
	e = e.Of(p.Ctr)
	x.reset()
	if err := x.a.runClose(closePlan{path: x.proj, name: "p", engine: &e, ctr: p.Ctr.Name}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !x.scaled() {
		t.Error("Fermer n'a pas exécuté « scale statefulset p-a --replicas=0 »")
	}
}

func TestClose269ForcerFermerArreteLePod(t *testing.T) {
	x := t269Setup(t, "")
	p := x.info(t)
	x.reset()
	notes, err := x.a.runForceClose(x.proj, p, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !x.scaled() {
		t.Errorf("Forcer fermer n'a pas exécuté « scale statefulset p-a --replicas=0 » (notes : %v)", notes)
	}
}

// Garde-fou : un pod en marche, sain, au modèle courant n'est jamais arrêté par Forcer fermer.
func TestClose269ForcerFermerLaisseUnPodSainAuModeleCourant(t *testing.T) {
	x := t269Setup(t, "v2")
	p := x.info(t)
	if p.Ctr.OldMounts {
		t.Fatalf("pod au modèle courant vu à l'ancien modèle : %+v", p.Ctr)
	}
	x.reset()
	if _, err := x.a.runForceClose(x.proj, p, nil, false); err != nil {
		t.Fatal(err)
	}
	if x.scaled() {
		t.Error("pod sain au modèle courant arrêté par Forcer fermer")
	}
}
