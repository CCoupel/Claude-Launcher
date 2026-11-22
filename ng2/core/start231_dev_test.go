package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #231 : /start-session proposé (O/n) pour un projet géré sans team ; rien si team présente (team_name) ou projet non géré.
func TestStartSessionQuestion231(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("le script de démarrage est du bash (tmux de l'hôte ou d'un conteneur) : jamais exécuté sous Windows")
	}
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	t.Setenv("CL_TEST_ISOLATION", "1")
	run := func(proj, answer string) (script, got string) {
		bin := filepath.Join(t.TempDir(), "claude")
		out := filepath.Join(t.TempDir(), "arg")
		_ = os.WriteFile(bin, []byte("#!/bin/bash\necho \"$*\" > "+out+"\n"), 0o755)
		t.Setenv("CL_CLAUDE_BIN", bin)
		start, _ := StartCommands(Config{}, proj, "", LaunchNormal)
		cmd := exec.Command("bash", "-c", "cd "+Shq(proj)+"; "+start)
		cmd.Stdin = strings.NewReader(answer)
		_ = cmd.Run()
		b, _ := os.ReadFile(out)
		return start, strings.TrimSpace(string(b))
	}
	managed := t.TempDir()
	_ = os.MkdirAll(filepath.Join(managed, ".claude", "commands"), 0o755)
	_ = os.WriteFile(StartSessionPath(managed), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(managed, ".claude", "project-config.json"), []byte(`{"team_name":"t1"}`), 0o644)
	if _, g := run(managed, "\n"); !strings.HasSuffix(g, "/start-session") {
		t.Errorf("Entrée : %q", g)
	}
	if _, g := run(managed, "n\n"); strings.Contains(g, "start-session") {
		t.Errorf("refus : %q", g)
	}
	_ = os.MkdirAll(filepath.Join(cfgDir, "teams", "t1"), 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "teams", "t1", "config.json"), []byte(`{"members":[]}`), 0o644)
	if s, _ := run(managed, "\n"); strings.Contains(s, "start-session") {
		t.Errorf("team présente : question posée")
	}
	raw := t.TempDir()
	if s, _ := run(raw, "\n"); strings.Contains(s, "start-session") {
		t.Errorf("projet non géré : question posée")
	}
}

// team_name connu mais sa team absente : pas de repli par cwd, même si une autre team a le projet pour cwd.
func TestTeamExistsNamedMissingNoCwdFallback231(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	proj := t.TempDir()
	_ = os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, ".claude", "project-config.json"), []byte(`{"team_name":"absente"}`), 0o644)
	other := filepath.Join(cfgDir, "teams", "autre")
	_ = os.MkdirAll(other, 0o755)
	cfgJSON, _ := json.Marshal(map[string]any{"members": []any{map[string]any{"cwd": ClaudePath(proj)}}}) // échappe les « \\ » Windows
	_ = os.WriteFile(filepath.Join(other, "config.json"), cfgJSON, 0o644)
	if TeamExists(proj) {
		t.Error("team nommée absente : TeamExists ne doit pas retomber sur une autre team au même cwd")
	}
	// sans team_name : repli par cwd
	_ = os.WriteFile(filepath.Join(proj, ".claude", "project-config.json"), []byte(`{}`), 0o644)
	if !TeamExists(proj) {
		t.Error("team_name absent : repli par cwd attendu")
	}
}
