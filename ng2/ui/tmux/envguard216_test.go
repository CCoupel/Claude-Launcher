package tmux

// #216 (complément) : F2 de la coque — l'environnement d'un projet (backend, image, env.yaml) est refusé projet ouvert ;
// projet libre : écrit ; ouverture d'un projet jamais configuré : le contrôle passe (rien ne tourne). Moteurs factices en
// échec propre (jamais le vrai moteur), projets factices sous t.TempDir().

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

const mention216t = "fermez le projet pour modifier son environnement"

func f2Model216(t *testing.T) (model, string) {
	t.Helper()
	h := withHome(t)
	_ = os.MkdirAll(filepath.Dir(core.BackendsPath()), 0o755)
	if err := os.WriteFile(core.BackendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engines := t.TempDir() // moteur « vide » : aucun conteneur ne tourne, jamais le vrai moteur
	for _, n := range []string{"podman", "docker"} {
		_ = os.WriteFile(filepath.Join(engines, n), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	t.Setenv("PATH", engines+string(os.PathListSeparator)+os.Getenv("PATH"))
	proj := filepath.Join(h, "PROJETS", "T", "alpha")
	_ = os.MkdirAll(filepath.Join(proj, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	_ = os.WriteFile(core.EnvPath(proj), []byte("image: docker.io/library/debian:12\npackages: [jq]\n"), 0o644)
	_ = core.WriteAssignment(proj, "pod")
	n := core.Node{Name: "alpha", Key: "alpha", Path: proj, Project: true}
	m := model{cfg: core.Config{Roots: []string{filepath.Join(h, "PROJETS")}}, nodes: []core.Node{n}, visible: []int{0},
		windows: map[string]bool{}, git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{}}
	return m, proj
}

// F2 sur un projet OUVERT : aucun formulaire, la consigne est affichée, rien n'est écrit.
func TestF2ProjetOuvertRefuseSansFormulaire216(t *testing.T) {
	m, proj := f2Model216(t)
	m.windows["alpha"] = true
	envBefore, _ := os.ReadFile(core.EnvPath(proj))
	m2 := m.cycleBackend()
	if m2.form.mode != formNone {
		t.Errorf("formulaire ouvert pour un projet ouvert : mode=%v", m2.form.mode)
	}
	if !strings.Contains(m2.msg, mention216t) {
		t.Errorf("msg = %q, attendu la consigne « %s »", m2.msg, mention216t)
	}
	if after, _ := os.ReadFile(core.EnvPath(proj)); string(after) != string(envBefore) || core.ProjectBackend(proj).Name != "pod" {
		t.Error("environnement ou affectation modifiés")
	}
}

// Validation d'un formulaire F2 obtenu AVANT l'ouverture du projet (autre écran, ouverture entre-temps) : refusée à
// l'écriture, même consigne, rien d'écrit ; projet libre : écrit.
func TestF2RevereifieeALEcriture216(t *testing.T) {
	m, proj := f2Model216(t)
	m = m.cycleBackend()
	if m.form.mode != formExec || m.form.existing != proj {
		t.Fatalf("F2 sur projet fermé : mode=%v existing=%q", m.form.mode, m.form.existing)
	}
	if len(m.form.images) == 0 {
		t.Fatal("aucune image proposée")
	}
	m.form.exec = 1 // premier socle de la liste
	envBefore, _ := os.ReadFile(core.EnvPath(proj))
	m.windows["alpha"] = true // le projet s'ouvre pendant que le formulaire est affiché
	refused := m.applyExecExisting()
	if refused.form.mode != formExec || !strings.Contains(refused.form.err, mention216t) {
		t.Errorf("validation sur projet ouvert : mode=%v err=%q, attendu le formulaire conservé avec la consigne", refused.form.mode, refused.form.err)
	}
	if after, _ := os.ReadFile(core.EnvPath(proj)); string(after) != string(envBefore) {
		t.Error("env.yaml écrit malgré le refus")
	}
	m.windows["alpha"] = false
	done := m.applyExecExisting()
	if done.form.err != "" || done.form.mode != formNone {
		t.Fatalf("projet libre : mode=%v err=%q", done.form.mode, done.form.err)
	}
	if after, _ := os.ReadFile(core.EnvPath(proj)); !strings.Contains(string(after), m.form.images[0]) {
		t.Errorf("image du socle non écrite : %s", after)
	}
}

// Première ouverture d'un projet jamais configuré (openNext) : rien ne tourne, le contrôle ne bloque pas le choix du socle.
func TestF2PremiereOuvertureNonBloquee216(t *testing.T) {
	m, proj := f2Model216(t)
	_ = os.Remove(core.EnvPath(proj))
	m = m.startExecChoice(core.Node{Name: "alpha", Key: "alpha", Path: proj, Project: true}, true)
	if m.form.mode != formExec || !m.form.openNext {
		t.Fatalf("mode=%v openNext=%v", m.form.mode, m.form.openNext)
	}
	m.form.exec = 1
	done := m.applyExecExisting()
	if done.form.err != "" {
		t.Errorf("première ouverture refusée : %q", done.form.err)
	}
}
