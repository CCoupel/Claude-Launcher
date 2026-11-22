//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Deux create git forgés simultanés : une seule place (un seul job, annulable), l'autre refusée « création en cours » ;
// l'annulation nettoie le dossier créé.
func TestDeuxCreateGitConcurrentsUnSeulJob215(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	base := a.conf().Roots[0]
	type res struct {
		v   any
		err error
	}
	out := make(chan res, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := a.create(createArgs{Base: base, Name: "clone" + string(rune('a'+i)), Source: "git", URL: "https://github.com/a/b"})
			out <- res{v, err}
		}(i)
	}
	wg.Wait()
	close(out)
	jobs, busy := 0, 0
	var id string
	for r := range out {
		m, _ := r.v.(map[string]any)
		switch {
		case r.err != nil:
			t.Errorf("erreur inattendue : %v", r.err)
		case m["job"] != nil:
			jobs++
			id, _ = m["job"].(string)
		case m["msg"] == "création en cours":
			busy++
		default:
			t.Errorf("réponse inattendue : %v", m)
		}
	}
	if jobs != 1 || busy != 5 {
		t.Fatalf("jobs=%d refus=%d, attendu 1 et 5", jobs, busy)
	}
	if snap := a.newJobSnapLocked(); snap == nil || snap.ID != id || snap.State != "running" {
		t.Fatalf("instantané du job : %+v", snap)
	}
	if v, _ := a.createCancel(id); v.(map[string]any)["ok"] != true {
		t.Fatalf("annulation refusée : %v", v)
	}
	for end := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if s := a.newJobSnapLocked(); s != nil && s.State == "failed" {
			break
		}
		if time.Now().After(end) {
			t.Fatal("job non terminé après annulation")
		}
	}
	ents, _ := os.ReadDir(base)
	for _, e := range ents {
		if len(e.Name()) > 5 && e.Name()[:5] == "clone" {
			t.Errorf("dossier %s non nettoyé", e.Name())
		}
	}
}

// initProposalAnswer : seul un projet CONNU portant le marqueur est accepté.
func TestInitProposalAnswerRefuseUnChemin215(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	ext := t.TempDir() // hors arbre, avec marqueur
	if err := os.MkdirAll(filepath.Join(ext, ".claude-launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(ext, ".claude-launcher", "init-proposed"), nil, 0o644)
	for _, p := range []string{ext, "/etc", "", "../x"} {
		v, err := a.initProposalAnswer(p, true)
		m, _ := v.(map[string]any)
		if err != nil || m["ok"] != false {
			t.Errorf("chemin %q accepté : %v %v", p, v, err)
		}
	}
	if !core215Pending(ext) {
		t.Error("marqueur d'un chemin hors arbre retiré")
	}
}

func core215Pending(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-launcher", "init-proposed"))
	return err == nil
}
