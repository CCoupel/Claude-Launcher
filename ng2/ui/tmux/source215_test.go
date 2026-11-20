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

// Coque : l'affectation (applyExec) n'est écrite qu'APRÈS un clonage réussi ; un échec n'en laisse aucune trace.
func TestAffectationApresClonageSeulement215(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	m, root := srcModel(t)
	m.form = newForm{mode: formExec, base: root, name: "p", source: "git", url: "https://github.com/a/b", exec: 0}
	dir := filepath.Join(root, "p")
	nm, cmd := m.createProject()
	if cmd == nil || nm.(model).job == nil {
		t.Fatal("clonage non lancé")
	}
	if core.Assigned(dir) {
		t.Error("affectation écrite AVANT le clonage")
	}
	nm.(model).job.cancelNow()
	got := nm.(model).cloneDone(cloneDoneMsg{job: nm.(model).job, err: context.Canceled})
	if core.Assigned(dir) {
		t.Error("affectation restée après annulation")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("dossier restant après annulation")
	}
	_ = got
	// succès : l'affectation est écrite à ce moment
	m2, root2 := srcModel(t)
	d2, undo, err := core.CreateProjectDir(m2.cfg.Roots, root2, "", false, "q")
	if err != nil {
		t.Fatal(err)
	}
	m2.form = newForm{mode: formCloning, base: root2, name: "q", source: "git", exec: 0}
	job := &cloneJob{undo: undo, dir: d2}
	m2.job = job
	_ = m2.cloneDone(cloneDoneMsg{job: job})
	if !core.Assigned(d2) {
		t.Error("affectation absente après clonage réussi")
	}
}

// Un projet qui attend sa proposition /init-project la reçoit à l'ouverture normale ; un projet brut jamais.
func TestModeEffectifPropose215(t *testing.T) {
	d := t.TempDir()
	if effectiveMode(d, launchNormal) != launchNormal {
		t.Error("mode changé sans marqueur")
	}
	if err := core.MarkInitProposed(d); err != nil {
		t.Fatal(err)
	}
	if effectiveMode(d, launchNormal) != launchProposed {
		t.Error("proposition non faite malgré le marqueur")
	}
	if effectiveMode(d, launchNewRaw) != launchNewRaw {
		t.Error("projet brut proposé")
	}
	core.ClearInitProposal(d)
	if effectiveMode(d, launchNormal) != launchNormal {
		t.Error("proposition répétée après retrait du marqueur")
	}
}

// Coque : clone sans env.yaml + socle → none (décidé sur le dépôt avant applyExec), ouverture sans question.
func TestCloneSansEnvAvecSocleResteNone215(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	m, root := srcModel(t)
	dir, undo, err := core.CreateProjectDir(m.cfg.Roots, root, "", false, "q")
	if err != nil {
		t.Fatal(err)
	}
	m.form = newForm{mode: formCloning, base: root, name: "q", source: "git", url: "https://github.com/a/b", images: []string{"fedora:40"}, exec: 1, ctrBe: "srv1"}
	if err := core.RegisterClone(dir, m.form.url, "fedora:40"); err != nil {
		t.Fatal(err)
	}
	job := &cloneJob{undo: undo, dir: dir}
	m.job = job
	_ = m.cloneDone(cloneDoneMsg{job: job})
	if !core.HasEnv(dir) {
		t.Fatal("applyExec n'a pas écrit env.yaml")
	}
	if core.EnvTrust(dir) != core.TrustNone {
		t.Errorf("état = %s, none attendu", core.EnvTrust(dir))
	}
	if e, err := core.LoadEnv(dir); err != nil || e.Image != "fedora:40" {
		t.Errorf("LoadEnv = %+v %v", e, err)
	}
}
