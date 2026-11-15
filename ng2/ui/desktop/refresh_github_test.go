package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// GATE LOT-2 (#158) : l'étape GitHub du rafraîchissement.

func githubProject(t *testing.T, ghScript string) (a *App, r *recorder, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux gh en shell : hors Windows")
	}
	ps := sandbox(t, "", "alpha")
	path = ps[0]
	cfg, _ := os.ReadFile(filepath.Join(path, ".git", "config"))
	_ = os.WriteFile(filepath.Join(path, ".git", "config"), append(cfg, []byte("[remote \"origin\"]\n\turl = https://github.com/x/y.git\n")...), 0o644)
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+ghScript+"\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r = &recorder{}
	return newApp(t, r.emit), r, path
}

// Cause probable : la relecture de TOUS les projets lancée par F5 occupe les 3 places de
// core.MsSem ; la relecture forcée du projet sélectionné attendait derrière (jusqu'au délai
// global de 30 s → « délai dépassé », étape rouge). Elle ne fait plus la queue.
func TestForcedMilestoneReadDoesNotQueueBehindBulkRead(t *testing.T) {
	a, _, path := githubProject(t, `echo '[]'`)
	for i := 0; i < cap(core.MsSem); i++ { // la relecture de masse tient toutes les places
		core.MsSem <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(core.MsSem); i++ {
			<-core.MsSem
		}
	}()
	done := make(chan error, 1)
	go func() { done <- a.fetchMilestone(path, true) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fetchMilestone : %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("la relecture forcée attend derrière la relecture de masse (core.MsSem)")
	}
}

func githubStep(t *testing.T, r *recorder, a *App, path string) (state, msg string) {
	t.Helper()
	if res, err := a.Dispatch("refreshProject", rawPath(path)); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("refreshProject : %v %v", res, err)
	}
	ev := waitRefreshDone(t, r, path, 40*time.Second)
	return stepStates(t, ev)["github"], ev["msg"].(string)
}

// gh non connecté : cas légitime, pas une erreur rouge ; la raison est dite.
func TestRefreshGitHubNotLoggedInIsNotRed(t *testing.T) {
	a, r, path := githubProject(t, `echo "To get started with GitHub CLI, please run:  gh auth login" >&2; exit 4`)
	state, msg := githubStep(t, r, a, path)
	if state != "ok" {
		t.Errorf("étape github = %q, attendu ok (gh non connecté : sans objet)", state)
	}
	if !strings.Contains(msg, "gh auth login") {
		t.Errorf("msg = %q, attendu la raison (gh non connecté)", msg)
	}
}

// Vraie panne : étape rouge, avec la cause réelle (stderr de gh) dans le message.
func TestRefreshGitHubRealFailureShowsCause(t *testing.T) {
	a, r, path := githubProject(t, `echo "error connecting to api.github.com" >&2; exit 1`)
	state, msg := githubStep(t, r, a, path)
	if state != "err" {
		t.Errorf("étape github = %q, attendu err", state)
	}
	if !strings.Contains(msg, "api.github.com") || strings.Contains(msg, "exit status") {
		t.Errorf("msg = %q, attendu la cause réelle de gh", msg)
	}
}
