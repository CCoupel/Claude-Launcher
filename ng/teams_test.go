package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeadTeams(t *testing.T) {
	root := t.TempDir()
	team := func(name, cfg string) {
		d := filepath.Join(root, "teams", name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if cfg != "" {
			if err := os.WriteFile(filepath.Join(d, "config.json"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	team("morte", `{"members":[{"tmuxPaneId":"leader"},{"tmuxPaneId":"%130"},{"tmuxPaneId":"%131"}]}`)
	team("vivante", `{"members":[{"tmuxPaneId":"leader"},{"tmuxPaneId":"%5"},{"tmuxPaneId":"%131"}]}`)
	team("leader-seul", `{"members":[{"tmuxPaneId":"leader"}]}`)
	team("in-process", `{"members":[{"tmuxPaneId":"leader"},{"tmuxPaneId":""}]}`)
	team("sans-config", "")
	team("illisible", `{"members":[`)

	got := deadTeams(root, map[string]bool{"%1": true, "%5": true})
	if want := []string{"morte"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deadTeams=%v, attendu %v", got, want)
	}
}
