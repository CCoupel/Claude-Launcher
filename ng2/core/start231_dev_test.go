package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #231 : /start-session proposé (O/n) pour un projet géré sans team ; rien si team présente (team_name) ou projet non géré.
func TestStartSessionQuestion231(t *testing.T) {
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
