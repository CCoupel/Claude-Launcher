package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

func moveModel(t *testing.T) (model, string, string) {
	t.Helper()
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	root := filepath.Join(t.TempDir(), "PROJETS")
	proj := filepath.Join(root, "INFRA", "alpha")
	_ = os.MkdirAll(proj, 0o755)
	_ = os.MkdirAll(filepath.Join(root, "AUTRE"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, "a.txt"), []byte("x"), 0o644)
	m := model{cfg: core.Config{Roots: []string{root}}, windows: map[string]bool{}}
	m.nodes = withSpecial([]core.Node{{Name: "alpha", Path: proj, Project: true, Key: "alpha"}})
	return m, root, proj
}

func keyEnter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func TestDeplacerDepuisF2216(t *testing.T) {
	m, root, proj := moveModel(t)
	m = m.startMove(proj, "alpha")
	if m.form.mode != formCategory || m.form.moveFrom != proj {
		t.Fatalf("mode=%v moveFrom=%q", m.form.mode, m.form.moveFrom)
	}
	// catégorie AUTRE (liste : racine, AUTRE, INFRA, ＋) puis nom
	m.form.catIdx = 1
	nm, _ := m.onFormKey(keyEnter())
	m = nm.(model)
	if m.form.mode != formName || m.form.category != "AUTRE" || m.form.name != "alpha" {
		t.Fatalf("après catégorie : mode=%v cat=%q name=%q", m.form.mode, m.form.category, m.form.name)
	}
	m.form.name = "beta"
	nm, _ = m.onFormKey(keyEnter())
	m = nm.(model)
	if m.form.mode != formMoveConfirm {
		t.Fatalf("confirmation attendue, mode=%v err=%q", m.form.mode, m.form.err)
	}
	out := m.renderForm(80)
	for _, want := range []string{"Déplacer / renommer", proj, filepath.Join(root, "AUTRE", "beta"), "trust.json", "conteneur"} {
		if !strings.Contains(strings.Join(strings.Fields(out), ""), strings.Join(strings.Fields(want), "")) {
			t.Errorf("écran de confirmation sans %q", want)
		}
	}
	nm, _ = m.onFormKey(key215("o"))
	m = nm.(model)
	dst := filepath.Join(root, "AUTRE", "beta")
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("projet non déplacé : %v (msg %q err %q)", err, m.msg, m.form.err)
	}
	if _, err := os.Stat(proj); err == nil {
		t.Error("source restée")
	}
	if m.form.mode != formNone || !strings.Contains(m.msg, "déplacé") {
		t.Errorf("mode=%v msg=%q", m.form.mode, m.msg)
	}
}

func TestDeplacerRefusesEtBlockers216(t *testing.T) {
	m, root, proj := moveModel(t)
	// projet ouvert ici : refusé d'emblée
	m.windows["alpha"] = true
	m.form = newForm{mode: formExec, existing: proj, name: "alpha"}
	nm, _ := m.onFormKey(key215("m"))
	if f := nm.(model).form; f.err == "" || !strings.Contains(f.err, "ouvert dans ce launcher") || f.moveFrom != "" {
		t.Errorf("projet ouvert : err=%q moveFrom=%q", f.err, f.moveFrom)
	}
	m.windows["alpha"] = false
	// destination existante / identique / traversée : refusées à la saisie du nom
	_ = os.MkdirAll(filepath.Join(root, "AUTRE", "existe"), 0o755)
	for name, tc := range map[string]struct{ cat, nom string }{
		"existante": {"AUTRE", "existe"}, "identique": {"INFRA", "alpha"}, "traversée": {"../x", "z"}, "nom": {"AUTRE", "a/b"},
	} {
		mm := m.startMove(proj, "alpha")
		mm.form.mode, mm.form.category, mm.form.name = formName, tc.cat, tc.nom
		nm, _ := mm.onFormKey(keyEnter())
		if f := nm.(model).form; f.mode != formName || f.err == "" {
			t.Errorf("%s : mode=%v err=%q", name, f.mode, f.err)
		}
	}
}

// Déplacement inachevé : écran de reprise / annulation (p / a).
func TestDeplacementInachevePAReprendreAnnuler216(t *testing.T) {
	m, root, proj := moveModel(t)
	dst := filepath.Join(root, "AUTRE", "beta")
	n := 0
	env := m.moveEnv()
	env.Roots = m.cfg.Roots
	env.Fail = func(s string, undo bool) error {
		if s == core.MoveStepAssign && !undo && n == 0 {
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
	m = m.startMove(dst, "beta")
	if m.form.mode != formMovePending || m.form.moveInfo.Pending == nil {
		t.Fatalf("écran de reprise absent : mode=%v", m.form.mode)
	}
	flat := strings.Join(strings.Fields(m.renderForm(80)), "")
	for _, want := range []string{"Déplacementinachevé", "reprendre", "annuler"} {
		if !strings.Contains(flat, want) {
			t.Errorf("écran sans %q", want)
		}
	}
	nm, _ := m.onFormKey(key215("r")) // reprendre : rejoue les étapes non faites
	m = nm.(model)
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil || m.form.mode != formNone {
		t.Errorf("reprise : projet déplacé=%v mode=%v msg=%q err=%q", err == nil, m.form.mode, m.msg, m.form.err)
	}
}

// Retour test humain : projet OUVERT ⇒ « Déplacer / renommer » grisé dans F2 avec la mention, touche m sans effet.
func TestDeplacerGriseQuandLeProjetEstOuvert216(t *testing.T) {
	m, _, proj := moveModel(t)
	m.windows["alpha"] = true
	n := core.Node{Name: "alpha", Path: proj, Project: true, Key: "alpha"}
	// pas de backend conteneur dans ce banc : on part de l'écran F2 construit comme startExecChoice le fait
	m.form = newForm{mode: formExec, existing: proj, name: "alpha", moveOpen: m.moveEnv().OpenHere(n.Path)}
	if !m.form.moveOpen {
		t.Fatal("moveOpen attendu pour un projet dont la fenêtre existe")
	}
	out := strings.Join(strings.Fields(m.renderForm(100)), " ")
	if !strings.Contains(out, "fermez le projet pour le déplacer") {
		t.Errorf("mention absente :\n%s", out)
	}
	if strings.Contains(out, "m déplacer / renommer · esc") {
		t.Errorf("l'entrée active reste annoncée :\n%s", out)
	}
	nm, cmd := m.onFormKey(key215("m"))
	f := nm.(model).form
	if f.mode != formExec || f.moveFrom != "" || f.err != "" || cmd != nil {
		t.Errorf("m doit être sans effet : mode=%v moveFrom=%q err=%q cmd=%v", f.mode, f.moveFrom, f.err, cmd != nil)
	}
	if nm.(model).msg != "" {
		t.Errorf("message inattendu : %q", nm.(model).msg)
	}
	// projet fermé : entrée active (annoncée) et m ouvre le déplacement
	m.windows["alpha"] = false
	m.form = newForm{mode: formExec, existing: proj, name: "alpha", moveOpen: m.moveEnv().OpenHere(n.Path)}
	out = strings.Join(strings.Fields(m.renderForm(100)), " ")
	if !strings.Contains(out, "m déplacer / renommer") || strings.Contains(out, "fermez le projet") {
		t.Errorf("entrée active attendue :\n%s", out)
	}
	nm, _ = m.onFormKey(key215("m"))
	if f := nm.(model).form; f.moveFrom != proj {
		t.Errorf("m n'ouvre pas le déplacement d'un projet fermé : %+v", f.mode)
	}
}

// Sans backend conteneur, F2 ouvre directement le déplacement — sauf projet ouvert : mention, rien ne s'ouvre.
func TestF2SansBackendProjetOuvert216(t *testing.T) {
	// « sans backend conteneur » doit être POSÉ, pas supposé : sans backends.yaml, le launcher détecte podman / docker du
	// poste (cas d'un runner de CI) et F2 ouvre alors l'écran d'exécution au lieu du déplacement
	m, _, proj := moveModel(t) // pose son propre CL_CONFIG_DIR : backends.yaml s'écrit APRÈS
	if err := os.WriteFile(filepath.Join(os.Getenv("CL_CONFIG_DIR"), "backends.yaml"), []byte("backends: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := core.DefaultCtrBackend(); ok {
		t.Fatal("un backend conteneur est encore détecté malgré backends.yaml vide : le banc n'est pas isolé")
	}
	n := core.Node{Name: "alpha", Path: proj, Project: true, Key: "alpha"}
	m.windows["alpha"] = true
	out := m.startExecChoice(n, false)
	if out.form.mode != formNone || !strings.Contains(out.msg, "fermez le projet pour le déplacer") {
		t.Errorf("mode=%v msg=%q", out.form.mode, out.msg)
	}
	m.windows["alpha"] = false
	if out := m.startExecChoice(n, false); out.form.moveFrom != proj {
		t.Errorf("projet fermé : déplacement non ouvert (mode=%v)", out.form.mode)
	}
}

// LOT-3 : F2 (applyExecExisting) n'écrit ni env.yaml ni l'affectation quand le projet est ouvert — la mention est dite.
func TestF2EnvNonModifiableProjetOuvert216(t *testing.T) {
	m, _, proj := moveModel(t)
	m.windows["alpha"] = true
	m.form = newForm{mode: formExec, existing: proj, name: "alpha", images: []string{"docker.io/library/debian:12"}, exec: 1, ctrBe: "srv1"}
	m = m.applyExecExisting()
	if !strings.Contains(m.form.err, "fermez le projet pour modifier son environnement") {
		t.Errorf("mention absente : %q", m.form.err)
	}
	if core.HasEnv(proj) || core.Assigned(proj) {
		t.Error("env.yaml ou affectation écrits malgré le refus")
	}
}
