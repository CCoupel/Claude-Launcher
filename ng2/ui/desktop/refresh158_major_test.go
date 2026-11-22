package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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
	pt := &projTerm{a: a, path: path, name: filepath.Base(path), state: "ready", stop: make(chan struct{})}
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

// Lecture du backend non aboutie (Errs, Down, Diag) : décision utilisateur — la fermeture forcée est
// PROPOSÉE AVEC AVERTISSEMENT : `why` annonce « backend injoignable : le projet tourne peut-être
// encore », `items` ne contient AUCUN arrêt de conteneur, et la confirmation ne nettoie que l'état
// local (aucun stop/kill/rm demandé au backend, session ou conteneur laissés tels quels).
func TestForceCloseWarnsWhenBackendReadFailed(t *testing.T) {
	cases := []struct {
		name string
		set  func(c *core.CtrView)
	}{
		{"erreur de listing (Errs)", func(c *core.CtrView) { c.Errs["lab"] = "docker : délai dépassé" }},
		{"serveur injoignable (Down)", func(c *core.CtrView) { c.Down["lab"] = "srv1, srv2" }},
		{"backend inutilisable (Diag)", func(c *core.CtrView) { c.Diag["lab"] = []core.Problem{{What: "daemon injoignable"}} }},
	}
	for _, withFound := range []bool{false, true} {
		for _, tc := range cases {
			name := tc.name
			if withFound {
				name += " + conteneur connu d'avant la panne"
			}
			t.Run(name, func(t *testing.T) {
				path := containerProject(t)
				calls := filepath.Join(t.TempDir(), "docker.log") // appels au faux docker : aucun stop/kill/rm attendu
				bin := t.TempDir()
				_ = os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho \"$*\" >> '"+calls+"'\necho 'Cannot connect to the Docker daemon' >&2\nexit 1\n"), 0o755)
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				var r recorder
				a := newApp(t, r.emit)
				containerTab(t, a, path)
				a.mu.Lock()
				a.ctr = core.CtrView{Env: map[string]bool{path: true}, Assign: map[string]string{path: "lab"}, Found: map[string]core.Found{},
					Errs: map[string]string{}, Diag: map[string][]core.Problem{}, Bad: map[string]string{}, Live: map[string]core.LiveSess{},
					Dups: map[string][]string{}, Down: map[string]string{}}
				if withFound {
					a.ctr.Found[path] = core.Found{Name: "p-alpha", Running: true}
				}
				tc.set(&a.ctr)
				a.mu.Unlock()
				if !canForceOf(t, a, path) {
					t.Error("canForce:false : la fermeture forcée doit être proposée (avec avertissement)")
				}
				res, err := a.Dispatch("forceClose", rawPath(path))
				if err != nil {
					t.Fatal(err)
				}
				q, _ := res.(map[string]any)
				if q == nil || q["question"] == nil {
					t.Fatalf("forceClose : %v, attendu une question avec avertissement", res)
				}
				why, _ := q["why"].(string)
				if !strings.Contains(why, "backend injoignable") || !strings.Contains(why, "tourne peut-être encore") {
					t.Errorf("why = %q, attendu « backend injoignable : le projet tourne peut-être encore »", why)
				}
				items, _ := q["items"].([]any)
				for _, it := range items {
					l := strings.ToLower(it.(string))
					if strings.Contains(l, "conteneur") || strings.Contains(l, "pod") || strings.Contains(l, "arrêt") {
						t.Errorf("items contient un arrêt côté backend : %q (état inconnu : nettoyage local seulement)", it)
					}
				}
				if len(items) == 0 || !strings.Contains(strings.ToLower(strings.Join(anyStrings(items), " ")), "onglet") {
					t.Errorf("items = %v, attendu au moins le nettoyage local (onglet)", items)
				}
				res, err = a.Dispatch("forceCloseConfirm", rawPath(path))
				if err != nil {
					t.Fatal(err)
				}
				if m, _ := res.(map[string]any); m == nil || m["ok"] != true {
					t.Fatalf("forceCloseConfirm : %v, attendu {ok:true}", res)
				}
				a.mu.Lock()
				_, still := a.terms[path]
				a.mu.Unlock()
				if still {
					t.Error("onglet conservé : l'état local doit être nettoyé")
				}
				if b, _ := os.ReadFile(calls); regexp.MustCompile(`(?m)\b(stop|kill|rm|rmi|restart|exec)\b`).Match(b) {
					t.Errorf("appel destructif au backend pendant la fermeture forcée : %q", b)
				}
			})
		}
	}
}

func anyStrings(l []any) []string {
	out := make([]string, 0, len(l))
	for _, v := range l {
		out = append(out, fmt.Sprint(v))
	}
	return out
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
}
