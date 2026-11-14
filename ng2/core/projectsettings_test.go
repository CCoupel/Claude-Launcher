package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// psEnv : config, cache et état isolés ; un projet factice (jamais un vrai).
func psEnv(t *testing.T) (proj string) {
	t.Helper()
	root := t.TempDir()
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "CL_CONFIG_DIR", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(k, filepath.Join(root, strings.ToLower(k)))
		_ = os.MkdirAll(filepath.Join(root, strings.ToLower(k)), 0o755)
	}
	proj = filepath.Join(root, "PROJETS", "alpha")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	return proj
}

func psWrite(t *testing.T, p, s string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSetProjectColor(t *testing.T) {
	unixOnly(t)
	multi := "# ma config\nUSAGE_REFRESH_SEC=300   # conso\ndeclare -A PROJECT_COLORS=(\n  # les miens\n  [\"alpha\"]=\"53\"   # prune\n  [\"beta\"]=\"17\"\n)\nRESUME_MODE=never\n"
	for name, tt := range map[string]struct{ src, name, color, want string }{
		"remplacement":   {multi, "alpha", "22", strings.Replace(multi, `["alpha"]="53"`, `["alpha"]="22"`, 1)},
		"ajout":          {multi, "gamma", "58", strings.Replace(multi, ")\nRESUME", "  [\"gamma\"]=\"58\"\n)\nRESUME", 1)},
		"retrait":        {multi, "beta", "", strings.Replace(multi, "  [\"beta\"]=\"17\"\n", "", 1)},
		"retrait absent": {multi, "inconnu", "", multi},
		"une ligne vide": {"declare -A PROJECT_COLORS=()\nX=1\n", "p", "5", "declare -A PROJECT_COLORS=([\"p\"]=\"5\")\nX=1\n"},
		"une ligne":      {"declare -A PROJECT_COLORS=([\"a\"]=\"1\")\n", "p", "5", "declare -A PROJECT_COLORS=([\"a\"]=\"1\" [\"p\"]=\"5\")\n"},
		"sans déclaration": {"X=1\n# declare -A PROJECT_COLORS=(\n#   [\"x\"]=\"1\"\n# )\n", "p", "5",
			"X=1\n# declare -A PROJECT_COLORS=(\n#   [\"x\"]=\"1\"\n# )\ndeclare -A PROJECT_COLORS=(\n  [\"p\"]=\"5\"\n)\n"},
		"sans fin de ligne":   {"X=1", "p", "5", "X=1\ndeclare -A PROJECT_COLORS=(\n  [\"p\"]=\"5\"\n)\n"},
		"clé sans guillemets": {"declare -A PROJECT_COLORS=(\n  [alpha]=53\n)\n", "alpha", "7", "declare -A PROJECT_COLORS=(\n  [\"alpha\"]=\"7\"\n)\n"},
	} {
		got, err := SetProjectColor(tt.src, tt.name, tt.color)
		if err != nil || got != tt.want {
			t.Errorf("%s :\n%q\nattendu\n%q (%v)", name, got, tt.want, err)
		}
		if HasErrors(ValidateConfig(ConfConf, []byte(got))) {
			t.Errorf("%s : config.conf invalide : %v\n%s", name, ValidateConfig(ConfConf, []byte(got)), got)
		}
	}
	for _, bad := range []struct{ name, color string }{{`a"b`, "5"}, {"a$b", "5"}, {"", "5"}, {"a", "256"}, {"a", "-1"}, {"a", "x"}, {"a", "05"}} {
		if _, err := SetProjectColor("", bad.name, bad.color); err == nil {
			t.Errorf("accepté : %+v", bad)
		}
	}
	// la lecture réelle (bash) voit la couleur écrite
	got, _ := SetProjectColor(multi, "gamma", "58")
	conf := filepath.Join(t.TempDir(), "config.conf")
	psWrite(t, conf, got)
	if c := loadConfigAt(conf); c.ProjectColors["gamma"] != "58" || c.ProjectColors["alpha"] != "53" {
		t.Errorf("couleurs relues : %v", c.ProjectColors)
	}
}

func TestMergedEnvLinesProvenance(t *testing.T) {
	proj := psEnv(t)
	psWrite(t, LauncherEnvPath(), "packages: [ripgrep]\nsetup:\n  - echo launcher\non_start:\n  - echo debut\n")
	psWrite(t, envTemplatePath(proj), "image: docker.io/library/debian:12\npackages: [git, jq]\nsetup:\n  - make deps\nwatch: [go.sum]\nmounts:\n  - { path: /data, ro: true }\n")
	proj2 := "image: docker.io/library/ubuntu:24.04\nprefer: lab\npackages: [jq, curl]\nsetup:\n  - echo projet\nlocal: [node_modules]\nmounts:\n  - { path: /data, ro: false }\n  - { path: /cache }\n"
	got, err := MergedEnvLines(proj, []byte(proj2))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range got {
		lines = append(lines, l.From+"|"+l.Text)
	}
	want := []string{
		"project|image: docker.io/library/ubuntu:24.04", "project|prefer: lab",
		"|packages:", "launcher|  - ripgrep", "template|  - git", "template|  - jq", "project|  - curl", // jq : revendiqué par le template
		"|setup:", "launcher|  - echo launcher", "template|  - make deps", "project|  - echo projet",
		"|on_start:", "launcher|  - echo debut",
		"|watch:", "template|  - go.sum",
		"|local:", "project|  - node_modules",
		"|mounts:", "project|  - { path: /data, ro: false }", "project|  - { path: /cache }", // le projet l'emporte sur ro
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("fusion :\n%s\nattendu\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// image du template quand le projet n'en a pas ; projet vide accepté
	got, err = MergedEnvLines(proj, nil)
	if err != nil || got[0].From != "template" || !strings.HasPrefix(got[0].Text, "image: ") {
		t.Errorf("projet vide : %+v %v", got, err)
	}
	// même résultat que la fusion réelle (LoadEnvFull) : mêmes paquets et mêmes étapes
	psWrite(t, EnvPath(proj), proj2)
	e, err := LoadEnvFull(proj)
	if err != nil || !reflect.DeepEqual(e.Packages, []string{"ripgrep", "git", "jq", "curl"}) ||
		!reflect.DeepEqual(e.Setup, []string{"echo launcher", "make deps", "echo projet"}) {
		t.Errorf("LoadEnvFull : %+v %v", e, err)
	}
	// YAML invalide : erreur
	if _, err := MergedEnvLines(proj, []byte("packages: [")); err == nil {
		t.Error("YAML invalide accepté")
	}
}

func TestReadProjectSettings(t *testing.T) {
	proj := psEnv(t)
	cfg := Config{Palette: []string{"17", "52"}, ProjectColors: map[string]string{}}
	s := ReadProjectSettings(proj, cfg, nil)
	if s.Assigned || s.Backend != "" || s.EnvExists || s.EnvMtime != "" || s.HasTemplate || !s.ColorAuto || s.Color == "" ||
		len(s.Env.Packages) != 0 || s.Env.Mounts == nil || len(s.Images) == 0 || len(s.Palette) != 2 || len(s.Problems) != 0 {
		t.Fatalf("projet vierge : %+v", s)
	}
	psWrite(t, envTemplatePath(proj), "packages: [git]\n")
	psWrite(t, EnvPath(proj), "# mon env\nimage: registry.example/img:1\npackages: [jq]\nmounts:\n  - { path: /d, ro: false }\n")
	if err := WriteAssignment(proj, "host"); err != nil {
		t.Fatal(err)
	}
	cfg.ProjectColors["alpha"] = "53"
	s = ReadProjectSettings(proj, cfg, nil)
	if !s.Assigned || s.Backend != "host" || !s.EnvExists || s.EnvMtime == "" || !s.HasTemplate || s.ColorAuto || s.Color != "53" ||
		s.Image != "registry.example/img:1" || s.Images[0] != "registry.example/img:1" ||
		!reflect.DeepEqual(s.Env.Packages, []string{"jq"}) || len(s.Env.Mounts) != 1 || s.Env.Mounts[0].RO == nil || *s.Env.Mounts[0].RO {
		t.Fatalf("projet configuré : %+v", s)
	}
	// aperçu d'un changement : modèle, fusion et problèmes du texte modifié, fichier intact
	before, _ := os.ReadFile(EnvPath(proj))
	p := ReadProjectSettings(proj, cfg, []ConfigChange{{Op: "set", At: []string{"packages"}, Value: []any{"jq", "curl"}}})
	if !reflect.DeepEqual(p.Env.Packages, []string{"jq", "curl"}) || len(p.Problems) != 0 {
		t.Fatalf("aperçu : %+v", p)
	}
	found := false
	for _, l := range p.Merged {
		found = found || l.Text == "  - curl" && l.From == "project"
	}
	if !found {
		t.Errorf("fusion de l'aperçu : %+v", p.Merged)
	}
	if after, _ := os.ReadFile(EnvPath(proj)); string(after) != string(before) {
		t.Fatal("l'aperçu a écrit le fichier")
	}
	// aperçu impossible : un problème, le modèle reste celui du fichier
	p = ReadProjectSettings(proj, cfg, []ConfigChange{{Op: "boum", At: []string{"x"}}})
	if !HasErrors(p.Problems) || !reflect.DeepEqual(p.Env.Packages, []string{"jq"}) {
		t.Fatalf("aperçu invalide : %+v", p)
	}
}

func TestApplyProjectSettings(t *testing.T) {
	unixOnly(t)
	proj := psEnv(t)
	str := func(s string) *string { return &s }
	psWrite(t, BackendsPath(), "backends:\n  lab: { type: docker, host: 'tcp://10.0.0.1:2375', shared: [/share] }\n")
	psWrite(t, ConfigPath(), "# ma config\nUSAGE_REFRESH_SEC=300\n")
	envSrc := "# env du projet — ne pas toucher\nimage: docker.io/library/debian:12   # socle\npackages: [jq]\n"
	psWrite(t, EnvPath(proj), envSrc)
	st, _ := os.Stat(EnvPath(proj))
	mt := st.ModTime().Format(time.RFC3339Nano)
	set := func(at string, v any) ConfigChange { return ConfigChange{Op: "set", At: []string{at}, Value: v} }

	// projet ouvert : backend et image refusés, rien n'est écrit
	if r := ApplyProjectSettings(proj, true, ProjectChange{Backend: str("lab"), Image: str("docker.io/library/alpine:3.20")}); r.OK || !strings.Contains(r.Msg, "projet ouvert") {
		t.Fatalf("projet ouvert : %+v", r)
	}
	if Assigned(proj) {
		t.Fatal("affectation écrite malgré le refus")
	}
	// mais env.yaml et couleur restent modifiables sur un projet ouvert
	r := ApplyProjectSettings(proj, true, ProjectChange{Color: str("22"), Env: &EnvChange{MTime: mt, Changes: []ConfigChange{set("packages", []any{"jq", "git"})}}})
	if !r.OK || r.Env == nil || !r.Env.Saved || r.Env.MTime == "" || r.Note == "" || !r.Color {
		t.Fatalf("env + couleur : %+v", r)
	}
	got, _ := os.ReadFile(EnvPath(proj))
	if !strings.Contains(string(got), "# env du projet — ne pas toucher") || !strings.Contains(string(got), "# socle") || !strings.Contains(string(got), "git") {
		t.Fatalf("env.yaml :\n%s", got)
	}
	if bak, _ := os.ReadFile(EnvPath(proj) + ".bak"); string(bak) != envSrc {
		t.Fatal("copie .bak absente")
	}
	conf, _ := os.ReadFile(ConfigPath())
	if want := "# ma config\nUSAGE_REFRESH_SEC=300\ndeclare -A PROJECT_COLORS=(\n  [\"alpha\"]=\"22\"\n)\n"; string(conf) != want {
		t.Fatalf("config.conf : %q", conf)
	}
	// conflit : env.yaml changé depuis l'ouverture — rien d'autre n'est écrit
	r = ApplyProjectSettings(proj, false, ProjectChange{Color: str("58"), Backend: str("lab"), Env: &EnvChange{MTime: mt, Changes: []ConfigChange{set("packages", []any{"x"})}}})
	if r.OK || r.Env == nil || !r.Env.Conflict || r.Env.MTime == "" {
		t.Fatalf("conflit : %+v", r)
	}
	if c, _ := os.ReadFile(ConfigPath()); strings.Contains(string(c), "58") || Assigned(proj) {
		t.Fatal("écritures faites malgré le conflit de env.yaml")
	}
	// invalide : image supprimée (et pas de template) → refus de la validation existante
	st, _ = os.Stat(EnvPath(proj))
	mt = st.ModTime().Format(time.RFC3339Nano)
	r = ApplyProjectSettings(proj, false, ProjectChange{Env: &EnvChange{MTime: mt, Changes: []ConfigChange{{Op: "delete", At: []string{"image"}}}}})
	if r.OK || r.Env == nil || !r.Env.Invalid || !HasErrors(r.Env.Problems) {
		t.Fatalf("invalide : %+v", r)
	}
	r = ApplyProjectSettings(proj, false, ProjectChange{Env: &EnvChange{MTime: mt, Changes: []ConfigChange{{Op: "delete", At: []string{"absent"}}}}})
	if r.OK || r.Env == nil || !r.Env.Invalid {
		t.Fatalf("changement impossible : %+v", r)
	}
	// backend et image : contrôles
	for _, bad := range []ProjectChange{{Backend: str("inconnu")}, {Backend: str("lab"), Image: str("  ")}, {Image: str("x:1")}} {
		if r := ApplyProjectSettings(proj, false, bad); r.OK || r.Msg == "" {
			t.Errorf("accepté : %+v", r)
		}
	}
	// affectation : conteneur puis hôte
	if r := ApplyProjectSettings(proj, false, ProjectChange{Backend: str("lab"), Image: str("docker.io/library/alpine:3.20")}); !r.OK {
		t.Fatalf("lab : %+v", r)
	}
	if ReadAssignments()[proj] != "lab" {
		t.Fatalf("affectation : %v", ReadAssignments())
	}
	if e, _ := LoadEnv(proj); e.Image != "docker.io/library/alpine:3.20" {
		t.Fatalf("image : %+v", e)
	}
	if r := ApplyProjectSettings(proj, false, ProjectChange{Image: str("docker.io/library/debian:12")}); !r.OK { // image seule : backend actuel gardé
		t.Fatalf("image seule : %+v", r)
	}
	if ReadAssignments()[proj] != "lab" {
		t.Fatal("backend perdu en changeant l'image")
	}
	if r := ApplyProjectSettings(proj, false, ProjectChange{Backend: str("host")}); !r.OK || ReadAssignments()[proj] != "host" {
		t.Fatalf("host : %+v %v", r, ReadAssignments())
	}
	// couleur retirée : l'entrée disparaît, le reste du fichier est intact
	if r := ApplyProjectSettings(proj, false, ProjectChange{Color: str("")}); !r.OK {
		t.Fatalf("couleur auto : %+v", r)
	}
	conf, _ = os.ReadFile(ConfigPath())
	if strings.Contains(string(conf), "alpha") || !strings.Contains(string(conf), "# ma config") {
		t.Fatalf("config.conf : %q", conf)
	}
}
