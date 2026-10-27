package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTeam(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agents := filepath.Join(proj, ".claude", "agents")
	write(filepath.Join(proj, ".claude", "project-config.json"), `{"agents":{
		"registry":[{"name":"teamleader","tag":"v3.29.1"},{"name":"dev-plugin","tag":"v3.29.1"},
		            {"name":"generic","tag":"v3.28.0"},{"name":"qa","tag":"v3.29.1"}],
		"generic":[{"name":"redacteur"}]}}`)
	write(filepath.Join(agents, "dev-plugin.md"), "## Perimetre\n")
	write(filepath.Join(agents, "qa.md"), "") // compagnon vide : ignoré
	write(filepath.Join(agents, "generic.redacteur.md"), "spec\n")
	write(filepath.Join(root, "teams", "s1", "config.json"), `{"members":[
		{"agentType":"team-lead","tmuxPaneId":"leader","cwd":"`+proj+`"},
		{"agentType":"dev-plugin","tmuxPaneId":"%24"},
		{"agentType":"dev-plugin","tmuxPaneId":"%25"},
		{"agentType":"custom","tmuxPaneId":"%26"},
		{"agentType":"qa","tmuxPaneId":"%99"}]}`)

	panes := []paneInfo{{"%21", "1", "2.1.311"}, {"%24", "1", "2.1.311"}, {"%25", "1", "2.1.310"},
		{"%26", "1", "2.1.311"}, {"%27", "1", "bash"}}
	got := liveTeam(readTeammates(proj), proj, panes)
	want := []teammate{
		{Name: "teamleader", Tag: "v3.29.1", Claude: "2.1.311", Count: 1},
		{Name: "dev-plugin", Tag: "v3.29.1", Companion: true, Claude: "2.1.311", Count: 2},
		{Name: "redacteur", Tag: "v3.28.0", Companion: true},
		{Name: "qa", Tag: "v3.29.1"},
		{Name: "custom", Claude: "2.1.311", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("liveTeam =\n%+v\nwant\n%+v", got, want)
	}

	// sans registre : copies *.template.md au tag du projet
	old := t.TempDir()
	write(filepath.Join(old, ".claude", ".template-source.json"), `{"tag":"v2.0.0"}`)
	write(filepath.Join(old, ".claude", "agents", "cdp.template.md"), "x")
	write(filepath.Join(old, ".claude", "agents", "cdp.md"), "x")
	if got := readTeammates(old); !reflect.DeepEqual(got, []teammate{{Name: "cdp", Tag: "v2.0.0", Companion: true}}) {
		t.Errorf("readTeammates sans registre = %+v", got)
	}
}
