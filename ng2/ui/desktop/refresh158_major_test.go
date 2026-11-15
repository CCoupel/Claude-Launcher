package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #158 (LOT-2), revue de code — deux MAJEURS :
//  (1) un projet qui revit entre la question et la confirmation de fermeture forcée ne doit rien perdre :
//      refus, aucune session tuée, aucune marque de sortie, conteneur non arrêté ;
//  (2) une lecture de backend non aboutie (Errs, Down, Diag, liste non revenue) ne prouve pas que le
//      conteneur a disparu : fermeture forcée non proposée, onglet non détaché par refreshProject.

// ── (1) revival entre la question et la confirmation ──────────────────────

func markerExists(path string) bool {
	_, err := os.Stat(core.CleanExitMarker(path))
	return err == nil
}

func confirmForce(t *testing.T, a *App, path string) map[string]any {
	t.Helper()
	res, err := a.Dispatch("forceCloseConfirm", rawPath(path))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	return m
}

// La session tmux locale du projet est recréée (rouverte, autre fenêtre) après la question : la
// confirmation est refusée, la session recréée survit, aucune marque de sortie propre n'est posée.
func TestForceCloseConfirmRefusedWhenHostSessionRevives(t *testing.T) {
	f := openFakeProject(t)
	f.deadButShown(t)
	res, err := f.a.Dispatch("forceClose", rawPath(f.path))
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := res.(map[string]any); q == nil || q["question"] == nil {
		t.Fatalf("forceClose : %v, attendu une question (projet affiché mort)", res)
	}
	// le projet revit : la session locale est recréée avant la confirmation
	f.tmux("new-session", "-d", "-s", hostSession(f.path), "sleep 600")
	if !f.sessionAlive() {
		t.Fatal("session non recréée par le test")
	}
	m := confirmForce(t, f.a, f.path)
	if m == nil || m["ok"] == true {
		t.Errorf("forceCloseConfirm accepté alors que la session a revécu : %v, attendu un refus {ok:false}", m)
	}
	if !f.sessionAlive() {
		t.Error("la session recréée a été tuée par la fermeture forcée (perte de travail)")
	}
	if markerExists(f.path) {
		t.Error("marque de sortie propre posée sur un projet redevenu vivant (la reprise ↺ serait abandonnée)")
	}
}

// Une session de conteneur vivante (HasLive) apparaît après la question : refus, rien nettoyé.
func TestForceCloseConfirmRefusedWhenLiveSessionAppears(t *testing.T) {
	f := openFakeProject(t)
	f.deadButShown(t)
	if res, _ := f.a.Dispatch("forceClose", rawPath(f.path)); res == nil || res.(map[string]any)["question"] == nil {
		t.Fatalf("forceClose : %v, attendu une question", res)
	}
	f.a.mu.Lock()
	if f.a.ctr.Live == nil {
		f.a.ctr.Live = map[string]core.LiveSess{}
	}
	f.a.ctr.Live[f.path] = core.LiveSess{Owner: core.OwnerInfo{Id: core.LauncherID(), At: time.Now()}, Panes: 2, Claude: true}
	f.a.mu.Unlock()
	m := confirmForce(t, f.a, f.path)
	if m == nil || m["ok"] == true {
		t.Errorf("forceCloseConfirm accepté alors qu'une session vit : %v, attendu un refus {ok:false}", m)
	}
	if markerExists(f.path) {
		t.Error("marque de sortie propre posée sur un projet vivant")
	}
	if !f.hasTab() {
		t.Error("onglet retiré d'un projet vivant")
	}
}

// ── (2) lecture de backend non aboutie ────────────────────────────────────

// containerTab : onglet « conteneur » factice (non hôte), sans moteur ni tmux : l'état d'un projet
// en conteneur dont l'interface affiche l'onglet.
func containerTab(t *testing.T, a *App, path string) *projTerm {
	t.Helper()
	pt := &projTerm{a: a, path: path, name: filepath.Base(path), host: false, state: "ready", stop: make(chan struct{})}
	a.mu.Lock()
	a.terms[path] = pt
	a.order = append(a.order, path)
	a.mu.Unlock()
	return pt
}

// containerProject : projet en conteneur (env.yaml, backend « lab » docker) ; PATH : un `docker` factice
// qui échoue aussitôt (moteur injoignable) — jamais de vrai moteur.
func containerProject(t *testing.T) (path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux docker en shell : hors Windows")
	}
	ps := sandbox(t, "", "alpha")
	path = ps[0]
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho 'Cannot connect to the Docker daemon' >&2\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = os.WriteFile(core.BackendsPath(), []byte("backends:\n  lab: { type: docker, host: 'tcp://127.0.0.1:9', shared: [/share] }\n"), 0o644)
	envFile := core.EnvPath(path)
	_ = os.MkdirAll(filepath.Dir(envFile), 0o755)
	_ = os.WriteFile(envFile, []byte("image: docker.io/library/debian:12\n"), 0o644)
	if err := core.WriteAssignment(path, "lab"); err != nil {
		t.Fatal(err)
	}
	return path
}

func canForceOf(t *testing.T, a *App, path string) bool {
	t.Helper()
	b, _ := json.Marshal(a.Snapshot())
	var s struct {
		Rows []struct {
			Path     string `json:"path"`
			CanForce bool   `json:"canForce"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(b, &s)
	for _, r := range s.Rows {
		if r.Path == path {
			return r.CanForce
		}
	}
	t.Fatalf("ligne %s absente de l'instantané", path)
	return false
}

// Liste des conteneurs non revenue (erreur du backend, serveur injoignable, backend inutilisable) :
// le conteneur n'est pas connu, sans preuve qu'il a disparu → fermeture forcée NON proposée.
func TestForceCloseNotProposedWhenBackendReadFailed(t *testing.T) {
	cases := []struct {
		name string
		set  func(c *core.CtrView)
	}{
		{"erreur de listing (Errs)", func(c *core.CtrView) { c.Errs["lab"] = "docker : délai dépassé" }},
		{"serveur injoignable (Down)", func(c *core.CtrView) { c.Down["lab"] = "srv1, srv2" }},
		{"backend inutilisable (Diag)", func(c *core.CtrView) { c.Diag["lab"] = []core.Problem{{What: "daemon injoignable"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := containerProject(t)
			var r recorder
			a := newApp(t, r.emit)
			containerTab(t, a, path)
			a.mu.Lock()
			a.ctr = core.CtrView{Env: map[string]bool{path: true}, Assign: map[string]string{path: "lab"}, Found: map[string]core.Found{},
				Errs: map[string]string{}, Diag: map[string][]core.Problem{}, Bad: map[string]string{}, Live: map[string]core.LiveSess{},
				Dups: map[string][]string{}, Down: map[string]string{}}
			tc.set(&a.ctr)
			a.mu.Unlock()
			if canForceOf(t, a, path) {
				t.Error("canForce:true alors que la lecture du backend n'a pas abouti")
			}
			res, err := a.Dispatch("forceClose", rawPath(path))
			if err != nil {
				t.Fatal(err)
			}
			if m, _ := res.(map[string]any); m == nil || m["question"] != nil {
				t.Errorf("forceClose : %v, attendu {msg} sans question (état inconnu : rien de destructif)", res)
			}
			if _, err := os.Stat(core.CleanExitMarker(path)); err == nil {
				t.Error("marque de sortie propre posée")
			}
		})
	}
}

// refreshProject pendant une panne du backend : l'onglet d'un projet qui vit peut-être n'est pas
// détaché (rien de destructif sur un état inconnu) ; l'opération se termine par done:true.
func TestRefreshProjectKeepsTabWhenBackendReadFails(t *testing.T) {
	path := containerProject(t)
	var r recorder
	a := newApp(t, r.emit)
	containerTab(t, a, path)
	res, err := a.Dispatch("refreshProject", rawPath(path))
	if err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("refreshProject : %v %v", res, err)
	}
	waitRefreshDone(t, &r, path, 40*time.Second)
	a.mu.Lock()
	_, still := a.terms[path]
	a.mu.Unlock()
	if !still {
		t.Error("onglet détaché par refreshProject alors que la lecture du backend n'a pas abouti")
	}
	if res, _ := a.Dispatch("forceClose", rawPath(path)); res != nil {
		if m, _ := res.(map[string]any); m != nil && m["question"] != nil {
			t.Errorf("fermeture forcée proposée après une lecture en échec : %v", m["question"])
		}
	}
}
