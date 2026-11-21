package tmux

// #214 : étape catégorie de la coque (formCategory / formNewCat), succès de la création, refus de chemin.
// Pilotage par les touches (onFormKey), bases factices sous t.TempDir() ; tmux isolé (isoInner) : aucune session
// claude-hub n'existe, l'ouverture du projet créé échoue proprement et ne touche à rien d'autre.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

func newModel214(t *testing.T) (model, string) {
	t.Helper()
	isoInner(t)
	// aucun backend conteneur : pas d'étape « socle » (formExec). Sans backends.yaml, LoadBackends détecte podman/docker
	// du PATH : un runner de CI qui en a un ajoutait l'étape et la création ne se faisait pas (cause du rouge CI).
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "backends.yaml"), []byte("backends: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_CONFIG_DIR", cfgDir)
	base := filepath.Join(t.TempDir(), "PROJETS")
	_ = os.MkdirAll(filepath.Join(base, "INFRA", "p1", ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(base, "VIDE"), 0o755)
	m := model{cfg: core.Config{Roots: []string{base}}, windows: map[string]bool{}, git: map[string]core.GitInfo{},
		tpl: map[string]core.TemplateState{}, detail: map[string]core.GitDetail{}, asked: map[string]bool{}}
	return m, base
}

func press214(m model, keys ...string) model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.onFormKey(msg)
		m = next.(model)
	}
	return m
}

func paths214(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error { out[p] = true; return nil })
	return out
}

func newPaths214(t *testing.T, root string, avant map[string]bool) []string {
	t.Helper()
	var out []string
	for p := range paths214(t, root) {
		if !avant[p] {
			out = append(out, strings.TrimPrefix(p, root))
		}
	}
	return out
}

func TestCoqueCategorieListeEtOrdre214(t *testing.T) {
	m, _ := newModel214(t)
	m = m.startNewProject("")
	if m.form.mode != formCategory {
		t.Fatalf("mode = %v, attendu formCategory (une seule base : étape catégorie directe)", m.form.mode)
	}
	var paths []string
	for _, c := range m.form.cats {
		paths = append(paths, c.Path)
	}
	if strings.Join(paths, ",") != "INFRA,VIDE" {
		t.Errorf("catégories = %v, attendu INFRA,VIDE (projets exclus)", paths)
	}
	if m.form.catIdx != 0 {
		t.Errorf("curseur initial = %d, attendu 0 (racine)", m.form.catIdx)
	}
	// le curseur reste dans 0 .. len(cats)+1 (« ＋ nouvelle » en dernier)
	m = press214(m, "up", "up")
	if m.form.catIdx != 0 {
		t.Errorf("curseur = %d après deux « haut »", m.form.catIdx)
	}
	m = press214(m, "down", "down", "down", "down", "down")
	if m.form.catIdx != len(m.form.cats)+1 {
		t.Errorf("curseur = %d, attendu %d (« ＋ nouvelle »)", m.form.catIdx, len(m.form.cats)+1)
	}
}

func TestCoqueCreationCategorieExistante214(t *testing.T) {
	m, base := newModel214(t)
	avant := paths214(t, filepath.Dir(base))
	m = m.startNewProject("")
	m = press214(m, "down", "enter") // INFRA
	if m.form.mode != formName || m.form.category != "INFRA" || m.form.newCat {
		t.Fatalf("après choix : mode=%v category=%q newCat=%v", m.form.mode, m.form.category, m.form.newCat)
	}
	m = press214(m, "n", "1", "enter", "b", "enter") // nom n1, source « Projet brut » (b), remote vide
	dir := filepath.Join(base, "INFRA", "n1")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("projet non créé en %s : %v (msg %q, err %q)", dir, err, m.msg, m.form.err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("git init absent (Projet brut) : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md minimal créé : plus d'option « géré » ni de CLAUDE.md minimal")
	}
	for _, p := range newPaths214(t, filepath.Dir(base), avant) {
		if !strings.HasPrefix(p, string(filepath.Separator)+"PROJETS"+string(filepath.Separator)+"INFRA"+string(filepath.Separator)+"n1") {
			t.Errorf("créé hors du projet : %s", p)
		}
	}
}

func TestCoqueCreationNouvelleCategorie214(t *testing.T) {
	m, base := newModel214(t)
	avant := paths214(t, filepath.Dir(base))
	m = m.startNewProject("")
	m = press214(m, "down", "down", "down", "enter") // « ＋ nouvelle » (après INFRA, VIDE)
	if m.form.mode != formNewCat {
		t.Fatalf("mode = %v, attendu formNewCat", m.form.mode)
	}
	// rien n'est créé tant que la création n'est pas validée
	m = press214(m, "N", "E", "W", "/", "S", "U", "B")
	if got := newPaths214(t, filepath.Dir(base), avant); len(got) != 0 {
		t.Errorf("écriture à la frappe : %v", got)
	}
	m = press214(m, "enter")
	if m.form.mode != formName || m.form.category != "NEW/SUB" || !m.form.newCat {
		t.Fatalf("mode=%v category=%q newCat=%v err=%q", m.form.mode, m.form.category, m.form.newCat, m.form.err)
	}
	if got := newPaths214(t, filepath.Dir(base), avant); len(got) != 0 {
		t.Errorf("catégorie créée avant la validation du projet : %v", got)
	}
	m = press214(m, "n", "2", "enter", "b", "enter")
	dir := filepath.Join(base, "NEW", "SUB", "n2")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("projet non créé en %s : %v (err %q)", dir, err, m.form.err)
	}
	for _, p := range newPaths214(t, filepath.Dir(base), avant) {
		if !strings.HasPrefix(p, string(filepath.Separator)+"PROJETS"+string(filepath.Separator)+"NEW") {
			t.Errorf("créé hors de la nouvelle catégorie : %s", p)
		}
	}
}

func TestCoqueCreationALaRacineDeLaBase214(t *testing.T) {
	m, base := newModel214(t)
	avant := paths214(t, filepath.Dir(base))
	m = m.startNewProject("")
	m = press214(m, "enter") // première ligne : racine de la base
	if m.form.mode != formName || m.form.category != "" || m.form.newCat {
		t.Fatalf("mode=%v category=%q", m.form.mode, m.form.category)
	}
	m = press214(m, "r", "1", "enter", "b", "enter")
	dir := filepath.Join(base, "r1")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("projet non créé en %s : %v (err %q)", dir, err, m.form.err)
	}
	for _, p := range newPaths214(t, filepath.Dir(base), avant) {
		if !strings.HasPrefix(p, string(filepath.Separator)+"PROJETS"+string(filepath.Separator)+"r1") {
			t.Errorf("créé hors du projet : %s", p)
		}
	}
}

func TestCoqueRefusDeChemin214(t *testing.T) {
	m, base := newModel214(t)
	avant := paths214(t, filepath.Dir(base))
	dehors := t.TempDir()
	_ = os.Symlink(dehors, filepath.Join(base, "SORTIE"))
	avant = paths214(t, filepath.Dir(base))
	m = m.startNewProject("")
	// nouvelle catégorie invalide : refus lisible, on reste sur l'étape
	for _, cat := range []string{"..", "../x", "/abs", `a\b`, ".h", "SORTIE/x"} {
		mm := press214(m, "down", "down", "down", "enter") // « ＋ nouvelle »
		for _, r := range cat {
			mm = press214(mm, string(r))
		}
		mm = press214(mm, "enter")
		if mm.form.mode != formNewCat || strings.TrimSpace(mm.form.err) == "" {
			t.Errorf("catégorie %q : mode=%v err=%q, refus attendu sur l'étape", cat, mm.form.mode, mm.form.err)
		}
	}
	// nom invalide : refus, on reste sur l'étape nom ; INFRA/p1 existe déjà
	mm := press214(m, "down", "enter")
	for _, n := range []string{"..", "../x", "a/b", ".caché"} {
		x := mm
		for _, r := range n {
			x = press214(x, string(r))
		}
		x = press214(x, "enter")
		if x.form.mode != formName || strings.TrimSpace(x.form.err) == "" {
			t.Errorf("nom %q : mode=%v err=%q", n, x.form.mode, x.form.err)
		}
	}
	x := press214(mm, "p", "1", "enter")
	if x.form.mode != formName || !strings.Contains(x.form.err, "existe déjà") {
		t.Errorf("nom existant : mode=%v err=%q", x.form.mode, x.form.err)
	}
	if got := newPaths214(t, filepath.Dir(base), avant); len(got) != 0 {
		t.Errorf("écriture malgré les refus : %v", got)
	}
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors des bases : %v", es)
	}
	// base d'un double-clic hors des bases configurées : refus
	if mm := m.startNewProject("/etc"); mm.form.mode == formCategory || !strings.Contains(mm.msg, "base inconnue") {
		t.Errorf("base hors config : mode=%v msg=%q", mm.form.mode, mm.msg)
	}
	// double-clic sur une catégorie : base et catégorie pré-sélectionnées
	pre := m.startNewProject(filepath.Join(base, "INFRA"))
	if pre.form.base != base || pre.form.category != "INFRA" || pre.form.mode != formName {
		t.Errorf("pré-sélection : base=%q category=%q mode=%v", pre.form.base, pre.form.category, pre.form.mode)
	}
}

// « Projet brut » dans la coque, avec et sans remote : git init, origin seulement si une URL est fournie, jamais de
// CLAUDE.md ni de marqueur /init-project ; un remote en forme d'option est refusé sans rien laisser.
func TestCoqueProjetBrutAvecEtSansRemote214(t *testing.T) {
	create := func(t *testing.T, name, remote string) (string, model) {
		m, base := newModel214(t)
		m = m.startNewProject("")
		m = press214(m, "enter") // racine de la base
		for _, r := range name {
			m = press214(m, string(r))
		}
		m = press214(m, "enter", "b") // source « Projet brut »
		if m.form.mode != formRemote {
			t.Fatalf("mode = %v, attendu formRemote (Projet brut : plus d'étape géré/brut)", m.form.mode)
		}
		for _, r := range remote {
			m = press214(m, string(r))
		}
		m = press214(m, "enter")
		return filepath.Join(base, name), m
	}
	t.Run("sans remote", func(t *testing.T) {
		dir, _ := create(t, "brut1", "")
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Fatalf("git init absent : %v", err)
		}
		if out, _ := exec.Command("git", "-C", dir, "remote").Output(); strings.TrimSpace(string(out)) != "" {
			t.Errorf("remote %q sans URL fournie", out)
		}
		for _, absent := range []string{"CLAUDE.md", filepath.Join(".claude-launcher", "init-proposed")} {
			if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
				t.Errorf("%s créé pour un projet brut", absent)
			}
		}
	})
	t.Run("avec remote", func(t *testing.T) {
		dir, _ := create(t, "brut2", "git@github.com:moi/depot-test.git")
		out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
		if err != nil || strings.TrimSpace(string(out)) != "git@github.com:moi/depot-test.git" {
			t.Errorf("origin = %q, %v", out, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			t.Error("CLAUDE.md créé pour un projet brut")
		}
	})
	t.Run("remote en forme d'option", func(t *testing.T) {
		dir, m := create(t, "brut3", "-u")
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("dossier %s laissé après un remote refusé", dir)
		}
		if m.form.err == "" {
			t.Error("refus sans message affiché")
		}
	})
}
