package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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

	got := DeadTeams(root, map[string]bool{"%1": true, "%5": true}, 0)
	if want := []string{"morte"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deadTeams=%v, attendu %v", got, want)
	}

	// team morte mais session du leader encore reprenable : épargnée
	team("reprenable", `{"leadSessionId":"abcd1234-x","members":[{"tmuxPaneId":"leader"},{"tmuxPaneId":"%140"}]}`)
	conv := filepath.Join(root, "projects", "-p", "abcd1234-x.jsonl")
	_ = os.MkdirAll(filepath.Dir(conv), 0o755)
	_ = os.WriteFile(conv, []byte("{}\n"), 0o644)
	got = DeadTeams(root, map[string]bool{"%5": true}, time.Hour)
	if want := []string{"morte"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deadTeams (reprise 1 h)=%v, attendu %v", got, want)
	}
	got = DeadTeams(root, map[string]bool{"%5": true}, 0)
	if want := []string{"morte", "reprenable"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deadTeams (reprise désactivée)=%v, attendu %v", got, want)
	}
}

// Team d'un conteneur (pidSpace étranger) : ses panes %N sont ceux du tmux du
// conteneur, jamais jugés par l'hôte. Team de l'hôte d'un démarrage précédent
// (même hostname, autre namespace) : jugée normalement.
func TestDeadTeamsForeignPidSpace(t *testing.T) {
	root := t.TempDir()
	team := func(name, cfg string) {
		d := filepath.Join(root, "teams", name)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "config.json"), []byte(cfg), 0o644)
	}
	team("conteneur", `{"leadProcess":{"pid":12,"pidSpace":"cl-demo-1234#pid:[1]"},"members":[{"tmuxPaneId":"%1"}]}`)
	team("hote-ancien", `{"leadProcess":{"pid":12,"pidSpace":"`+HostName()+`#pid:[1]"},"members":[{"tmuxPaneId":"%1"}]}`)
	team("hote", `{"leadProcess":{"pid":12,"pidSpace":"`+HostPidSpace()+`"},"members":[{"tmuxPaneId":"%1"}]}`)
	got := DeadTeams(root, map[string]bool{"%9": true}, 0)
	if want := []string{"hote", "hote-ancien"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deadTeams=%v, attendu %v", got, want)
	}
}
