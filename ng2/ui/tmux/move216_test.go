package tmux

// #216 : déplacer / renommer depuis F2 — compléments boîte noire à projectmove216_test.go : échec réel (historique de la
// destination déjà présent) → état initial, Échap / Annuler sans écriture, approbation d'environnement qui suit le
// projet. Projets factices sous t.TempDir() ; tmux isolé (isoInner) ; backend podman déclaré mais jamais appelé.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

func world216t(t *testing.T, root string) map[string]string {
	t.Helper()
	w := map[string]string{}
	_ = filepath.Walk(root, func(p string, st os.FileInfo, err error) error {
		if err != nil || st == nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if st.IsDir() {
			w[rel] = "dir"
		} else {
			b, _ := os.ReadFile(p)
			w[rel] = string(b)
		}
		return nil
	})
	return w
}

func sameWorld216t(t *testing.T, what string, a, b map[string]string) {
	t.Helper()
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			t.Errorf("%s : %s modifié ou supprimé", what, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			t.Errorf("%s : %s créé", what, k)
		}
	}
}

func confirm216(t *testing.T, m model, proj, cat, name string) model {
	t.Helper()
	m = m.startMove(proj, filepath.Base(proj))
	if m.form.mode != formCategory {
		t.Fatalf("mode = %v, attendu formCategory", m.form.mode)
	}
	idx := 0
	for i, c := range m.form.cats {
		if c.Path == cat {
			idx = i + 1
		}
	}
	m.form.catIdx = idx
	nm, _ := m.onFormKey(keyEnter())
	m = nm.(model)
	m.form.name = name
	nm, _ = m.onFormKey(keyEnter())
	m = nm.(model)
	if m.form.mode != formMoveConfirm {
		t.Fatalf("confirmation attendue, mode=%v err=%q", m.form.mode, m.form.err)
	}
	return m
}

func TestCoqueEchecDuDeplacementRetablitLEtatInitial216(t *testing.T) {
	m, root, proj := moveModel(t)
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	dst := filepath.Join(root, "AUTRE", "beta")
	stale := filepath.Join(claude, "projects", core.ProjectDirName(dst)) // historique d'un autre projet : jamais écrasé
	_ = os.MkdirAll(stale, 0o755)
	_ = os.WriteFile(filepath.Join(stale, "ancien.jsonl"), []byte("autre"), 0o644)
	src := filepath.Join(claude, "projects", core.ProjectDirName(proj)) // historique du projet déplacé : l'étape history a quelque chose à faire
	_ = os.MkdirAll(src, 0o755)
	_ = os.WriteFile(filepath.Join(src, "s.jsonl"), []byte("{}"), 0o644)
	before := world216t(t, root)
	m = confirm216(t, m, proj, "AUTRE", "beta")
	nm, _ := m.onFormKey(key215("o"))
	m = nm.(model)
	if m.form.mode != formMoveConfirm || !strings.Contains(m.form.err, "historique") {
		t.Fatalf("mode=%v err=%q, attendu la confirmation conservée avec la cause affichée", m.form.mode, m.form.err)
	}
	if !strings.Contains(strings.Join(strings.Fields(m.renderForm(80)), ""), "historique") {
		t.Error("l'erreur n'est pas rendue à l'écran")
	}
	sameWorld216t(t, "échec", before, world216t(t, root))
	if b, _ := os.ReadFile(filepath.Join(stale, "ancien.jsonl")); string(b) != "autre" {
		t.Error("historique de la destination écrasé")
	}
	if got := core.PendingMoves(proj); len(got) != 0 {
		t.Errorf("journal non vidé : %v", got)
	}
	// l'utilisateur change de nom et revalide : le déplacement aboutit
	m.form.name, m.form.mode, m.form.err = "gamma", formName, ""
	nm, _ = m.onFormKey(keyEnter())
	m = nm.(model)
	nm, _ = m.onFormKey(key215("o"))
	if _, err := os.Stat(filepath.Join(root, "AUTRE", "gamma", "a.txt")); err != nil {
		t.Errorf("second essai : %v (err %q)", err, nm.(model).form.err)
	}
}

func TestCoqueEchapEtAnnulerNEcriventRien216(t *testing.T) {
	m, root, proj := moveModel(t)
	before := world216t(t, root)
	m = confirm216(t, m, proj, "AUTRE", "beta")
	nm, _ := m.onFormKey(key215("esc"))
	if nm.(model).form.mode != formNone {
		t.Errorf("Échap : mode=%v", nm.(model).form.mode)
	}
	sameWorld216t(t, "Échap", before, world216t(t, root))
	// Annuler un déplacement inachevé : projet revenu à son chemin d'origine, journal vide
	dst := filepath.Join(root, "AUTRE", "beta")
	n := 0
	env := m.moveEnv()
	env.Fail = func(s string, undo bool) error {
		if s == core.MoveStepConf && !undo && n == 0 {
			n++
			return os.ErrPermission
		}
		if s == core.MoveStepDir && undo && n == 1 {
			n++
			return os.ErrPermission
		}
		return nil
	}
	if r := core.MoveProject(env, proj, core.MoveTarget{Base: root, Category: "AUTRE", Name: "beta"}); r.State != "partial" {
		t.Fatalf("partial attendu : %+v", r)
	}
	pm := m.startMove(dst, "beta")
	if pm.form.mode != formMovePending {
		t.Fatalf("mode=%v", pm.form.mode)
	}
	nm, _ = pm.onFormKey(key215("a"))
	am := nm.(model)
	if am.form.mode != formNone || !strings.Contains(am.msg, "annulé") {
		t.Errorf("Annuler : mode=%v msg=%q err=%q", am.form.mode, am.msg, am.form.err)
	}
	sameWorld216t(t, "Annuler", before, world216t(t, root))
	if got := core.PendingMoves(proj); len(got) != 0 {
		t.Errorf("journal après Annuler : %v", got)
	}
}

// Projet cloné approuvé : l'approbation SUIT le projet depuis la coque (aucune question à l'ouverture suivante, aucune
// entrée orpheline) ; un projet en attente de réponse garde sa question ; l'écran de confirmation liste la ligne.
func TestCoqueApprobationSuitLeProjet216(t *testing.T) {
	isoInner(t)
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	_ = os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	empty := t.TempDir() // moteur « vide » : aucun conteneur à supprimer à l'étape ctr, jamais le vrai moteur
	for _, n := range []string{"podman", "docker"} {
		_ = os.WriteFile(filepath.Join(empty, n), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	t.Setenv("PATH", empty+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := filepath.Join(t.TempDir(), "PROJETS")
	mk := func(name string) string {
		p := filepath.Join(root, "INFRA", name)
		_ = os.MkdirAll(filepath.Join(p, ".git"), 0o755)
		_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
		_ = os.WriteFile(core.EnvPath(p), []byte(guardEnv215), 0o644)
		_, _ = core.MarkEnvPending(p, "https://github.com/a/"+name+".git", "debian:12")
		_ = core.WriteAssignment(p, "pod")
		return p
	}
	acc, pen := mk("accepte"), mk("attente")
	info, _ := core.EnvTrustInfoFor(acc)
	if _, err := core.AnswerEnvTrust(acc, "accept", info.SHA); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "AUTRE"), 0o755)
	entBefore, _ := core.EnvTrustEntry(acc)
	m := model{cfg: core.Config{Roots: []string{root}}, windows: map[string]bool{}, launch: true,
		git: map[string]core.GitInfo{}, tpl: map[string]core.TemplateState{}, detail: map[string]core.GitDetail{}, asked: map[string]bool{}}
	m.nodes = withSpecial(core.ScanTree(m.cfg.Roots))
	m.visible = nil
	for i := range m.nodes {
		m.visible = append(m.visible, i)
	}

	m = confirm216(t, m, acc, "AUTRE", "accepte-renomme")
	flat := strings.Join(strings.Fields(m.renderForm(100)), "")
	if !strings.Contains(strings.ToLower(flat), "approbation") {
		t.Errorf("écran de confirmation sans la ligne d'approbation :\n%s", m.renderForm(100))
	}
	nm, _ := m.onFormKey(key215("o"))
	m = nm.(model)
	dst := filepath.Join(root, "AUTRE", "accepte-renomme")
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		t.Fatalf("projet non déplacé : %v (err %q)", err, m.form.err)
	}
	if e, ok := core.EnvTrustEntry(dst); !ok || e != entBefore {
		t.Errorf("entrée après déplacement = %+v (présente=%v), attendu %+v", e, ok, entBefore)
	}
	if _, orphan := core.EnvTrustEntry(acc); orphan {
		t.Error("entrée orpheline")
	}
	// ouverture : aucune question d'approbation (le projet est sélectionné par moveDone)
	m = m.openSelected()
	if m.form.mode == formTrust {
		t.Error("question d'approbation après déplacement d'un projet approuvé")
	}
	// projet en attente de réponse : déplacé, la question reste posée
	m.form = newForm{}
	m = confirm216(t, m, pen, "AUTRE", "attente-renomme")
	nm, _ = m.onFormKey(key215("o"))
	m = nm.(model)
	dst2 := filepath.Join(root, "AUTRE", "attente-renomme")
	if core.EnvTrust(dst2) != core.TrustPending {
		t.Errorf("état = %s, attendu pending conservé (rien n'est approuvé par le déplacement)", core.EnvTrust(dst2))
	}
	m = m.openSelected()
	if m.form.mode != formTrust {
		t.Errorf("mode=%v : la question d'approbation doit rester posée après le déplacement", m.form.mode)
	}
}
