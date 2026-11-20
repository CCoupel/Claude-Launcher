package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

func srcModel(t *testing.T) (model, string) {
	root := t.TempDir()
	m := model{cfg: core.Config{Roots: []string{root}}}
	return m, root
}

func TestChoixDeLaSource215(t *testing.T) {
	m, root := srcModel(t)
	m.form = newForm{mode: formSource, base: root, name: "p"}
	for i, want := range map[int]struct {
		src  string
		mode formMode
	}{0: {"empty", formKind}, 1: {"template", formRemote}, 2: {"git", formURL}} {
		nm, _ := m.chooseSource(i)
		f := nm.(model).form
		if f.source != want.src || f.mode != want.mode {
			t.Errorf("source %d → %q mode %v, attendu %q %v", i, f.source, f.mode, want.src, want.mode)
		}
	}
}

func TestURLRefuseeParLaCoque215(t *testing.T) {
	m, root := srcModel(t)
	for _, u := range []string{"file:///etc", "-u", "/tmp/x", ""} {
		m.form = newForm{mode: formURL, base: root, name: "p", source: "git", url: u}
		nm, cmd := m.onFormKey(tea.KeyMsg{Type: tea.KeyEnter})
		f := nm.(model).form
		if f.err == "" || f.mode != formURL || cmd != nil {
			t.Errorf("URL %q : err=%q mode=%v cmd=%v", u, f.err, f.mode, cmd != nil)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "p")); err == nil {
		t.Error("dossier créé malgré l'URL refusée")
	}
}

// Échec ou annulation du clonage : ce que l'appel a créé est supprimé (dossier du projet, catégorie créée vide) ;
// l'URL reste modifiable.
func TestEchecDuClonageNettoie215(t *testing.T) {
	for name, cerr := range map[string]error{"échec": errors.New("authentification refusée (HTTPS, jeton)"), "annulé": context.Canceled} {
		t.Run(name, func(t *testing.T) {
			m, root := srcModel(t)
			dir, undo, err := core.CreateProjectDir(m.cfg.Roots, root, "NEUVE", true, "p")
			if err != nil {
				t.Fatal(err)
			}
			m.form = newForm{mode: formCloning, base: root, name: "p", source: "git", url: "https://github.com/a/b"}
			job := &cloneJob{undo: undo, dir: dir}
			m.job = job
			m = m.cloneDone(cloneDoneMsg{job: job, err: cerr})
			if m.job != nil || m.form.mode != formURL || !strings.Contains(m.form.err, "supprimé") {
				t.Errorf("job=%v mode=%v err=%q", m.job, m.form.mode, m.form.err)
			}
			if _, err := os.Stat(filepath.Join(root, "NEUVE")); err == nil {
				t.Error("catégorie créée par l'appel non retirée")
			}
		})
	}
}
