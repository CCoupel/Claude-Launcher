package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

func TestExecChoice(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  a-pod: {type: podman}\n  b-dock: {type: docker}\ndefaults: {backend: b-dock}\nimages: [docker.io/library/debian:12, quay.io/x/y:1]\n"), 0o644)
	if b, ok := core.DefaultCtrBackend(); !ok || b.Name != "b-dock" {
		t.Errorf("defaults.backend : %+v", b)
	}
	if im := core.BaseImages(); len(im) != 2 || core.ImageLabel(im[0]) != "debian:12" || core.ImageLabel(im[1]) != "quay.io/x/y:1" {
		t.Errorf("images : %v", im)
	}
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(p, 0o755)
	if err := applyExec(p, newForm{exec: 0, images: core.BaseImages(), ctrBe: "b-dock"}); err != nil || core.HasEnv(p) || !core.Assigned(p) || core.ProjectBackend(p).Name != "host" {
		t.Errorf("hôte : env.yaml créé ou affectation absente (%v)", err)
	}
	if err := applyExec(p, newForm{exec: 1, images: core.BaseImages(), ctrBe: "b-dock"}); err != nil {
		t.Fatal(err)
	}
	if e, err := core.LoadEnv(p); err != nil || e.Image != "docker.io/library/debian:12" || core.ProjectBackend(p).Name != "b-dock" {
		t.Errorf("socle : %v %+v %s", err, e, core.ProjectBackend(p).Name)
	}
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  a-pod: {type: podman}\n"), 0o644)
	if b, _ := core.DefaultCtrBackend(); b.Name != "a-pod" || len(core.BaseImages()) != len(core.DefaultImages) {
		t.Errorf("défauts : %s %v", b.Name, core.BaseImages())
	}
}

func TestExecDefaultsPreselect(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	write := func(s string) { _ = os.WriteFile(core.BackendsPath(), []byte(s), 0o644) }
	write("backends:\n  pod: {type: podman}\n  lab: {type: k8s, volumes: {/x: p}}\ndefaults: {backend: lab, image: docker.io/library/rockylinux:9}\n")
	f, ok := execForm(newForm{})
	if !ok || f.ctrBe != "lab" || f.exec == 0 || f.images[f.exec-1] != "docker.io/library/rockylinux:9" {
		t.Errorf("présélection lab/rocky : %+v", f)
	}
	f.cycleCtrBe(1)
	if f.ctrBe != "pod" {
		t.Errorf("←→ : %s", f.ctrBe)
	}
	write("backends:\n  pod: {type: podman}\ndefaults: {backend: host, image: quay.io/a/b:2}\n")
	if f, _ := execForm(newForm{}); f.exec != 0 || f.images[0] != "quay.io/a/b:2" {
		t.Errorf("présélection hôte, image hors liste en tête : %+v", f)
	}
	write("backends:\n  pod: {type: podman}\ndefaults: {backend: pod}\n")
	if f, _ := execForm(newForm{}); f.exec != 1 || f.images[0] != core.DefaultImages[0] {
		t.Errorf("backend sans image : premier socle : %+v", f)
	}
	write("backends: {}\n")
	if _, ok := execForm(newForm{}); ok && len(core.DetectedEngines()) == 0 {
		t.Error("aucun backend conteneur : pas d'étape Exécution")
	}
}

func TestReconfigure(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  pod: {type: podman}\n  lab: {type: k8s, volumes: {/x: p}}\ndefaults: {backend: pod}\n"), 0o644)
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
	_ = os.WriteFile(core.EnvPath(p), []byte("# commentaire gardé\nimage: docker.io/library/debian:12\npackages: [jq]\n"), 0o644)
	_ = core.WriteAssignment(p, "lab")
	m := model{}.startExecChoice(core.Node{Path: p, Name: "proj"}, false)
	f := m.form
	if f.ctrBe != "lab" || f.exec == 0 || f.images[f.exec-1] != "docker.io/library/debian:12" {
		t.Fatalf("préremplissage : %+v", f)
	}
	f.exec = indexOf(f.images, "docker.io/library/alpine:3.20") + 1
	f.ctrBe = "pod"
	if err := applyExec(p, f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(core.EnvPath(p))
	if !strings.Contains(string(b), "image: docker.io/library/alpine:3.20") || !strings.Contains(string(b), "# commentaire gardé") || !strings.Contains(string(b), "packages: [jq]") || core.ProjectBackend(p).Name != "pod" {
		t.Errorf("reconfiguration : %s / %s", b, core.ProjectBackend(p).Name)
	}
	f.exec = 0
	_ = applyExec(p, f)
	if core.ProjectBackend(p).Name != "host" || !core.HasEnv(p) {
		t.Error("retour à l'hôte : env.yaml doit être gardé")
	}
	if m := (model{}).startExecChoice(core.Node{Path: p, Name: "proj"}, false); m.form.exec != 0 {
		t.Errorf("projet sur l'hôte : « hôte » attendu, %+v", m.form)
	}
}
