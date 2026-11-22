package tmux

// #215 : coque, source « dépôt git » avec un socle conteneur — le dossier est cloné D'ABORD (git refuse un dossier non
// vide), puis env.yaml et l'affectation du backend sont écrits. Faux git (jamais le vrai, jamais de réseau) qui refuse
// un dossier non vide comme le vrai ; backend podman déclaré (aucun moteur n'est appelé).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-launcher-ng2/core"
)

// fakeGitClone215 : `git clone -- URL DEST` simulé ; refuse un DEST non vide ; journalise argv.
func fakeGitClone215(t *testing.T) (rec string) {
	t.Helper()
	bin := t.TempDir()
	rec = filepath.Join(t.TempDir(), "rec")
	script := `#!/bin/sh
case " $* " in
*" clone "*) ;;
*) echo "git inattendu : $*" >&2; exit 2;;
esac
echo "$*" >> ` + rec + `
for last; do :; done; dest="$last"
if [ -n "$(ls -A "$dest" 2>/dev/null)" ]; then
  echo "fatal: destination path '$dest' already exists and is not an empty directory." >&2
  exit 128
fi
mkdir -p "$dest/.git"
printf '[remote "origin"]\n\turl = https://github.com/a/b\n' > "$dest/.git/config"
echo hello > "$dest/README.md"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return rec
}

// runCmd215 : exécute une commande bubbletea (lots compris) et rend les messages reçus ; la minuterie de progression
// (400 ms) est ignorée.
func runCmd215(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmd215(c)...)
		}
		return out
	}
	if _, tick := msg.(cloneTickMsg); tick {
		return nil
	}
	return []tea.Msg{msg}
}

func press215(t *testing.T, m model, keys ...string) (model, []tea.Msg) {
	t.Helper()
	var msgs []tea.Msg
	for _, k := range keys {
		var km tea.KeyMsg
		switch k {
		case "enter":
			km = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			km = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.onFormKey(km)
		m = next.(model)
		msgs = append(msgs, runCmd215(cmd)...)
	}
	return m, msgs
}

func TestCoqueGitAvecSocleClonePuisEnvYaml215(t *testing.T) {
	isoInner(t)
	cfgDir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "backends.yaml"), []byte("backends:\n  pod: {type: podman}\ndefaults: {backend: pod}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := fakeGitClone215(t)
	base := filepath.Join(t.TempDir(), "PROJETS")
	_ = os.MkdirAll(filepath.Join(base, "INFRA", "p1", ".git"), 0o755)
	m := model{cfg: core.Config{Roots: []string{base}}, windows: map[string]bool{}, git: map[string]core.GitInfo{},
		tpl: map[string]core.TemplateState{}, detail: map[string]core.GitDetail{}, asked: map[string]bool{}}
	m = m.startNewProject("")
	m, _ = press215(t, m, "down", "enter") // INFRA
	m, _ = press215(t, m, "d", "e", "p", "o", "t", "enter")
	if m.form.mode != formSource {
		t.Fatalf("mode = %v, attendu formSource", m.form.mode)
	}
	m, _ = press215(t, m, "g") // source : dépôt git
	if m.form.mode != formURL {
		t.Fatalf("mode = %v, attendu formURL", m.form.mode)
	}
	for _, r := range "https://github.com/CCoupel/depot.git" {
		m, _ = press215(t, m, string(r))
	}
	m, _ = press215(t, m, "enter") // URL valide + backend conteneur déclaré : étape « socle »
	if m.form.mode != formExec || len(m.form.images) == 0 || m.form.ctrBe != "pod" {
		t.Fatalf("mode=%v images=%d ctrBe=%q, attendu l'étape socle (formExec) sur le backend pod", m.form.mode, len(m.form.images), m.form.ctrBe)
	}
	m, _ = press215(t, m, "1") // premier socle de la liste
	img := m.form.images[m.form.exec-1]
	dir := filepath.Join(base, "INFRA", "depot")
	// validation : le clonage est asynchrone ; on rejoue ses messages (le clone puis la minuterie ignorée)
	next, cmd := m.onFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.form.mode != formCloning || m.job == nil {
		t.Fatalf("mode=%v job=%v, attendu formCloning avec un job", m.form.mode, m.job)
	}
	var done *cloneDoneMsg
	for _, msg := range runCmd215(cmd) {
		if d, ok := msg.(cloneDoneMsg); ok {
			done = &d
		}
	}
	if done == nil {
		t.Fatal("aucun cloneDoneMsg")
	}
	if done.err != nil {
		t.Fatalf("clonage refusé : %v — env.yaml écrit AVANT le clone dans un dossier que git refuse alors (non vide)", done.err)
	}
	m = m.cloneDone(*done)
	// le dépôt est cloné ET configuré : ordre clone → env.yaml
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("dépôt non cloné : %v", err)
	}
	if b, err := os.ReadFile(core.EnvPath(dir)); err != nil || !strings.Contains(string(b), "image: "+img) {
		t.Errorf("env.yaml = %q, %v ; attendu image: %s", b, err, img)
	}
	if be := core.ProjectBackend(dir).Name; be != "pod" {
		t.Errorf("backend du projet = %q, attendu pod", be)
	}
	if b, _ := os.ReadFile(rec); !strings.Contains(string(b), "-- https://github.com/CCoupel/depot.git "+dir) {
		t.Errorf("argv du clone = %s", b)
	}
	if m.form.mode != formNone && m.form.err != "" {
		t.Errorf("formulaire encore ouvert avec l'erreur %q", m.form.err)
	}
	_ = time.Second
}
