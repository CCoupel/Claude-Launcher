package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatalf("settings.json invalide : %v\n%s", err, b)
	}
	return d
}

func TestEnvVars(t *testing.T) {
	c := Config{GithubToken: "ghp_x", DisableMouse: true, ExpTeams: true,
		ExtraEnvs: []string{`MY_URL="https://a.b/c?d=1"`, "PLAIN=v", "bad name=1", "NOEQ", "GH_TOKEN=autre"}}
	got := map[string]string{}
	for _, v := range c.envVars() {
		got[v.K] = v.V
	}
	want := map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_x", "GH_TOKEN": "autre",
		"CLAUDE_CODE_DISABLE_MOUSE": "1", "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1",
		"MY_URL": "https://a.b/c?d=1", "PLAIN": "v"}
	if len(got) != len(want) {
		t.Fatalf("envVars = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, attendu %q", k, got[k], v)
		}
	}
	// secrets jamais tapés dans le shell quand settings.json est à jour
	e := c.exports(false)
	if strings.Contains(e, "ghp_x") || strings.Contains(e, "GH_TOKEN") || !strings.Contains(e, "export PLAIN='v'") {
		t.Errorf("exports(false) = %q", e)
	}
	if e := c.exports(true); !strings.Contains(e, "export GITHUB_PERSONAL_ACCESS_TOKEN='ghp_x'") {
		t.Errorf("exports(true) = %q", e)
	}
}

func TestSyncSettingsEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	orig := `{
  "env": {"CLAUDE_CODE_SPAWN_BACKEND": "tmux", "MINE": "garde"},
  "statusLine": {"type": "command", "command": "bash s.sh 2>&1 <x"},
  "teammateMode": "tmux"
}`
	if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncSettingsEnv(dir, []envVar{{"GH_TOKEN", "ghp_1"}, {"FOO", "bar"}}); err != nil {
		t.Fatal(err)
	}
	d := readSettings(t, dir)
	env := d["env"].(map[string]any)
	if env["GH_TOKEN"] != "ghp_1" || env["FOO"] != "bar" || env["MINE"] != "garde" || env["CLAUDE_CODE_SPAWN_BACKEND"] != "tmux" {
		t.Errorf("env = %v", env)
	}
	if d["teammateMode"] != "tmux" || d["statusLine"].(map[string]any)["command"] != "bash s.sh 2>&1 <x" {
		t.Errorf("reste du fichier perdu : %v", d)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), `\u00`) {
		t.Errorf("échappement HTML : %s", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("secret dans un fichier en %v", fi.Mode().Perm())
	}

	// sans changement : fichier non réécrit
	fi1, _ := os.Stat(p)
	if err := syncSettingsEnv(dir, []envVar{{"GH_TOKEN", "ghp_1"}, {"FOO", "bar"}}); err != nil {
		t.Fatal(err)
	}
	if fi2, _ := os.Stat(p); !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Error("réécrit sans changement")
	}

	// FOO retirée de la config : ôtée ; MINE (pas au launcher) gardée
	if err := syncSettingsEnv(dir, []envVar{{"GH_TOKEN", "ghp_2"}}); err != nil {
		t.Fatal(err)
	}
	env = readSettings(t, dir)["env"].(map[string]any)
	if _, ok := env["FOO"]; ok || env["GH_TOKEN"] != "ghp_2" || env["MINE"] != "garde" {
		t.Errorf("après retrait : %v", env)
	}

	// JSON invalide : laissé tel quel
	_ = os.WriteFile(p, []byte("{pas du json"), 0o600)
	if err := syncSettingsEnv(dir, []envVar{{"GH_TOKEN", "x"}}); err == nil {
		t.Error("JSON invalide accepté")
	}
	if b, _ := os.ReadFile(p); string(b) != "{pas du json" {
		t.Errorf("fichier invalide modifié : %s", b)
	}

	// pas de settings.json : créé
	d2 := t.TempDir()
	if err := syncSettingsEnv(d2, []envVar{{"A", "1"}}); err != nil {
		t.Fatal(err)
	}
	if readSettings(t, d2)["env"].(map[string]any)["A"] != "1" {
		t.Error("création")
	}
}
