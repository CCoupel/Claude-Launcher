package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cfgEditHome(t *testing.T) string {
	t.Helper()
	h := withHome(t)
	t.Setenv("CL_CONFIG_DIR", filepath.Join(h, "cfg"))
	t.Setenv("CL_ROOTS", "")
	// un podman présent (runner de CI) crée son stockage sous XDG_DATA_HOME, avec
	// des droits que le nettoyage de t.TempDir ne peut pas effacer : hors du test
	t.Setenv("XDG_DATA_HOME", filepath.Join(os.TempDir(), "cl-cfgedit-test-data"))
	_ = os.MkdirAll(filepath.Join(h, "cfg"), 0o755)
	return h
}

func findProblem(ps []ConfigProblem, sev, part string) *ConfigProblem {
	for i := range ps {
		if ps[i].Severity == sev && strings.Contains(ps[i].Message, part) {
			return &ps[i]
		}
	}
	return nil
}

func TestConfigFiles(t *testing.T) {
	h := cfgEditHome(t)
	proj := filepath.Join(h, "p")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, ".claude-launcher", "env-template.yaml"), []byte("image: debian:12\n"), 0o644)
	_ = os.WriteFile(BackendsPath(), []byte("backends: {}\n"), 0o644)
	fs := ConfigFiles(proj)
	if len(fs) != 4 {
		t.Fatalf("fichiers : %+v", fs)
	}
	kinds := []ConfigKind{ConfConf, ConfBackends, ConfLauncherEnv, ConfProjectEnv}
	for i, f := range fs {
		if f.Kind != kinds[i] || f.Path == "" || f.Label == "" {
			t.Errorf("fichier %d : %+v", i, f)
		}
	}
	if fs[0].Exists || !fs[1].Exists || fs[1].ModTime.IsZero() || fs[3].Exists || fs[3].Template == "" {
		t.Errorf("présence / date / modèle : %+v", fs)
	}
	if len(ConfigFiles("")) != 3 {
		t.Error("sans projet : 3 fichiers")
	}
	for _, k := range kinds {
		if ConfigTemplate(k) == "" {
			t.Errorf("pas de modèle pour %s", k)
		}
	}
}

func TestValidateBackends(t *testing.T) {
	cfgEditHome(t)
	// le modèle d'un fichier absent est valide
	if ps := ValidateConfig(ConfBackends, []byte(ConfigTemplate(ConfBackends))); HasErrors(ps) {
		t.Errorf("modèle : %+v", ps)
	}
	// syntaxe YAML : ligne du message de yaml
	ps := ValidateConfig(ConfBackends, []byte("backends:\n  a: { type: docker\n  b: x\n"))
	if !HasErrors(ps) || ps[0].Line == 0 {
		t.Errorf("syntaxe : %+v", ps)
	}
	// schéma (même contrôle que LoadBackends) : ligne du backend en cause
	ps = ValidateConfig(ConfBackends, []byte("backends:\n  ok: { type: docker, host: tcp://h:2375 }\n  bad: { type: lxc }\n"))
	if p := findProblem(ps, SevError, "non géré"); p == nil || p.Line != 3 {
		t.Errorf("type inconnu : %+v", ps)
	}
	ps = ValidateConfig(ConfBackends, []byte("backends:\n  s: { type: ssh }\n"))
	if p := findProblem(ps, SevError, "sans « host »"); p == nil || p.Line != 2 {
		t.Errorf("ssh sans host : %+v", ps)
	}
	// avertissements seulement : refus des backends locaux sous Windows,
	// correspondance de chemins invalide
	w := validateBackends([]byte("backends:\n  loc: { type: docker }\nhubs:\n  pc:\n    paths:\n      - { local: 'x', remote: rel }\n"), "windows", "pc")
	if HasErrors(w) || findProblem(w, SevWarning, "loc") == nil || findProblem(w, SevWarning, "hubs.pc") == nil {
		t.Errorf("avertissements Windows : %+v", w)
	}
	if l := validateBackends([]byte("backends:\n  loc: { type: docker }\n"), "linux", "pc"); len(l) != 0 {
		t.Errorf("Linux : rien à signaler : %+v", l)
	}
}

func TestValidateEnv(t *testing.T) {
	h := cfgEditHome(t)
	if ps := ValidateConfig(ConfLauncherEnv, []byte("packages: [jq]\nsetup: [\"echo\"]\n")); len(ps) != 0 {
		t.Errorf("env.yaml valide : %+v", ps)
	}
	ps := ValidateConfig(ConfLauncherEnv, []byte("packages: [jq\n"))
	if !HasErrors(ps) || ps[0].Line == 0 {
		t.Errorf("env.yaml invalide : %+v", ps)
	}
	ps = ValidateConfig(ConfLauncherEnv, []byte("local:\n  - ../dehors\n"))
	if p := findProblem(ps, SevError, "local"); p == nil || p.Line != 2 {
		t.Errorf("local hors projet : %+v", ps)
	}
	// projet : « image » requise au total, après fusion avec env-template.yaml
	proj := filepath.Join(h, "p")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	if ps := ValidateProjectEnv(proj, []byte("packages: [jq]\n")); findProblem(ps, SevError, "image") == nil {
		t.Errorf("sans image : %+v", ps)
	}
	_ = os.WriteFile(filepath.Join(proj, ".claude-launcher", "env-template.yaml"), []byte("image: debian:12\npackages: [git]\n"), 0o644)
	if ps := ValidateProjectEnv(proj, []byte("packages: [jq]\n")); HasErrors(ps) {
		t.Errorf("image du template : %+v", ps)
	}
	m, err := MergedProjectEnv(proj, []byte("packages: [jq]\n"))
	if err != nil || !strings.Contains(m, "debian:12") || !strings.Contains(m, "jq") || !strings.Contains(m, "git") {
		t.Errorf("fusion : %q %v", m, err)
	}
	if ps := ValidateProjectEnv(proj, []byte("prefer: inexistant\n")); findProblem(ps, SevWarning, "prefer") == nil {
		t.Errorf("prefer inconnu : %+v", ps)
	}
}

func TestValidateConf(t *testing.T) {
	cfgEditHome(t)
	if ps := ValidateConfig(ConfConf, []byte("USAGE_REFRESH_SEC=120\nGITHUB_DIRS=(\"/\")\n")); HasErrors(ps) {
		t.Errorf("config valide : %+v", ps)
	}
	ps := ValidateConfig(ConfConf, []byte("A=1\nGITHUB_DIRS=(\"/a\"\nB='x\n"))
	if !HasErrors(ps) || ps[0].Line == 0 {
		t.Errorf("config invalide : %+v", ps)
	}
	ps = ValidateConfig(ConfConf, []byte("GITHUB_DIRS=(\"/nexiste/pas/du/tout\")\n"))
	if p := findProblem(ps, SevWarning, "introuvable"); p == nil || p.Line != 1 || HasErrors(ps) {
		t.Errorf("racine introuvable : %+v", ps)
	}
}

func TestConfLiteCheck(t *testing.T) {
	for src, want := range map[string]int{
		"A=1\nB=(x y)\n":            0,
		"A='a(b'\n# (commentaire\n": 0,
		"A=\"x\\\"y\"\n":            0,
		"A=(x\nB=2\n":               1,
		"A=1\nB='x\n":               2,
		"A=x)\n":                    1,
	} {
		ps := confLiteCheck(src)
		switch {
		case want == 0 && len(ps) != 0:
			t.Errorf("%q : %+v", src, ps)
		case want != 0 && (len(ps) == 0 || ps[0].Line != want):
			t.Errorf("%q : ligne %d attendue, %+v", src, want, ps)
		}
	}
}

func TestSaveConfig(t *testing.T) {
	cfgEditHome(t)
	path := BackendsPath()
	// absent : créé, sans .bak
	ps, err := SaveConfig(ConfBackends, path, []byte("backends: {}\n"), time.Time{})
	if err != nil || len(ps) != 0 {
		t.Fatalf("création : %+v %v", ps, err)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error(".bak pour un fichier neuf")
	}
	st, _ := os.Stat(path)
	// erreur de validation : rien n'est écrit
	if _, err := SaveConfig(ConfBackends, path, []byte("backends:\n  x: { type: lxc }\n"), st.ModTime()); !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("invalide : %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "backends: {}\n" {
		t.Errorf("fichier modifié malgré l'erreur : %q", b)
	}
	// enregistrement : .bak de la version précédente, commentaires gardés
	next := "# mes backends\nbackends:\n  d: { type: docker, host: tcp://h:2375 }  # distant\n"
	if _, err := SaveConfig(ConfBackends, path, []byte(next), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != next {
		t.Errorf("contenu : %q", b)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != "backends: {}\n" {
		t.Errorf(".bak : %q", b)
	}
	if bs, err := LoadBackends(); err != nil || len(bs) < 2 {
		t.Errorf("relu par LoadBackends : %+v %v", bs, err)
	}
	// conflit : modifié sur le disque depuis l'ouverture
	opened, _ := os.Stat(path)
	time.Sleep(20 * time.Millisecond)
	_ = os.WriteFile(path, []byte("backends: {}\n# autre éditeur\n"), 0o644)
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)
	if _, err := SaveConfig(ConfBackends, path, []byte(next), opened.ModTime()); !errors.Is(err, ErrConfigConflict) {
		t.Errorf("conflit non vu : %v", err)
	}
	if _, err := SaveConfig(ConfBackends, path, []byte(next), time.Time{}); !errors.Is(err, ErrConfigConflict) {
		t.Errorf("fichier apparu depuis l'ouverture : %v", err)
	}
	// écraser après conflit : avec la date actuelle du fichier
	now, _ := os.Stat(path)
	if _, err := SaveConfig(ConfBackends, path, []byte(next), now.ModTime()); err != nil {
		t.Errorf("écraser : %v", err)
	}
	// seulement les fichiers de configuration
	if _, err := SaveConfig(ConfBackends, filepath.Join(filepath.Dir(path), "autre.yaml"), []byte("x: 1\n"), time.Time{}); err == nil {
		t.Error("chemin quelconque accepté")
	}
	if _, err := SaveConfig(ConfProjectEnv, filepath.Join(filepath.Dir(path), "env.yaml"), []byte("image: x\n"), time.Time{}); err == nil {
		t.Error("env.yaml hors d'un .claude-launcher accepté")
	}
}

func TestSaveConfigPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("droits Unix")
	}
	cfgEditHome(t)
	path := ConfigPath()
	if _, err := SaveConfig(ConfConf, path, []byte("USAGE_REFRESH_SEC=90\n"), time.Time{}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("config.conf : droits %v (jeton GitHub possible : 0600)", st.Mode().Perm())
	}
	if LoadConfig().UsageRefreshSec != 90 {
		t.Error("config.conf non relue")
	}
}
