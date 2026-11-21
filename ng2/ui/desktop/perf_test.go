package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #125 : second lancement servi par les caches — arbre repris sans parcours,
// badges sans git pour les projets dont l'état est en cache, journal des
// durées écrit ; durées mesurées (t.Log) pour 40 projets.
func TestStartupFromCaches(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, fmt.Sprintf("p%02d", i))
	}
	// CL_PERF_ROOT : projets sur un disque réseau (ex. le stockage partagé,
	// /share/MFS/ROOT/CLAUDE_PROJETS) pour des durées représentatives
	root := ""
	if r := os.Getenv("CL_PERF_ROOT"); r != "" {
		root = filepath.Join(r, ".cl-e2e-perf-"+strconv.Itoa(os.Getpid()))
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	ps := sandbox(t, root, names...)
	var r recorder
	// Le second lancement ne doit lancer AUCUN git : on COMPTE les lancements (un git enveloppé qui journalise puis passe
	// la main au vrai) au lieu de comparer des durées, qui échouent sous -race ou en charge (252 ms contre 28 ms).
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	bin, journal := t.TempDir(), filepath.Join(t.TempDir(), "git.log")
	wrapper := "#!/bin/sh\necho x >> " + journal + "\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	launches := func() int { b, _ := os.ReadFile(journal); return strings.Count(string(b), "x\n") }

	// premier lancement : parcours et git de tous les projets
	t0 := time.Now()
	a := newApp(t, r.emit)
	if a.treeCached {
		t.Fatal("arbre en cache au premier lancement")
	}
	newCold := time.Since(t0)
	t0 = time.Now()
	a.gitAll()
	gitCold := time.Since(t0)
	if len(a.git) != 40 {
		t.Fatalf("git : %d projets", len(a.git))
	}
	if n := launches(); n < 40 {
		t.Fatalf("premier lancement : %d git lancés pour 40 projets (comptage git inopérant ?)", n)
	}
	for _, p := range ps { // état préparé comme le ferait le gestionnaire d'état
		writeStateForTest(t, core.ComputeState(p))
	}

	// second lancement : tout vient des caches
	coldLaunches := launches()
	t0 = time.Now()
	b := newApp(t, r.emit)
	newWarm := time.Since(t0)
	if !b.treeCached || len(b.projectPaths()) != 40 {
		t.Fatalf("arbre du lancement précédent : %v, %d projets", b.treeCached, len(b.projectPaths()))
	}
	if len(b.git) != 40 || len(b.fromCache) != 40 {
		t.Fatalf("badges en cache : git %d, état %d", len(b.git), len(b.fromCache))
	}
	t0 = time.Now()
	b.gitAll() // rien à relancer : tous les états sont en cache
	gitWarm := time.Since(t0)
	b.perf.add("git", gitWarm)
	b.perf.write(40)
	if lg, err := os.ReadFile(filepath.Join(core.CacheDir(), "desktop-perf.log")); err != nil || !strings.Contains(string(lg), "40 projets") {
		t.Errorf("journal : %q %v", lg, err)
	}
	t.Logf("40 projets : premier lancement %s + git %s ; second %s + git %s",
		newCold.Round(time.Millisecond), gitCold.Round(time.Millisecond), newWarm.Round(time.Millisecond), gitWarm.Round(time.Millisecond))
	if n := launches() - coldLaunches; n != 0 {
		t.Errorf("second lancement : %d git lancés (création de l'App + gitAll), attendu 0 — tout vient des caches", n)
	}
	// les durées restent journalisées (t.Log) pour comparaison à la main, jamais en assertion : instables en charge
}

// Cache d'état du hub Linux (hub_cache) : lu sous le chemin des backends,
// préféré au cache local s'il est plus frais.
func TestHubStateCache(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	hub := t.TempDir()
	st := core.ComputeState(ps[0])
	st.Path = core.RemotePath(ps[0])
	st.Git.Branch = "depuis-le-hub"
	writeStateTo(t, hub, st)
	got, ok := bestState(hub, ps[0])
	if !ok || got.Git.Branch != "depuis-le-hub" || got.Path != ps[0] {
		t.Fatalf("état du hub : %+v %v", got, ok)
	}
	if _, ok := bestState("", ps[0]); ok {
		t.Error("sans hub ni cache local : pas d'état")
	}
}

func writeStateForTest(t *testing.T, ps core.ProjState) {
	t.Helper()
	writeStateTo(t, core.CacheDir(), ps)
}

// writeStateTo : un fichier d'état au format du cache (core/cache.go).
func writeStateTo(t *testing.T, dir string, ps core.ProjState) {
	t.Helper()
	p := core.StatePath(ps.Path)
	if dir != core.CacheDir() {
		p = filepath.Join(dir, "state", filepath.Base(p))
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	writeJSON(p, ps)
}
