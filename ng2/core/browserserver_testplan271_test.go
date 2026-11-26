//go:build !windows

package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #271 — _work/test-plan est servi comme _work/mockup (plans de test HTML ouverts dans Chrome) ; les autres dossiers de
// _work (reports, handoff, tasks, tmp…) restent refusés (404) et masqués des listes. Casse ignorée.
func testplanTree(t *testing.T) (srv *httptest.Server, proj string) {
	t.Helper()
	root := t.TempDir()
	proj = filepath.Join(root, "PROJETS", "p")
	for rel, c := range map[string]string{
		"_work/test-plan/x.html":                "<h1>plan</h1>",
		"_work/test-plan/sub/y.html":            "plan-sub",
		"_work/mockup/m.html":                   "maquette",
		"_work/reports/r.md":                    "rapport",
		"_work/handoff/h.md":                    "handoff",
		"_work/tasks/t.md":                      "tâche",
		"_work/tmp/secret.txt":                  "tmp",
		"_work/test-plan-autre/z.html":          "voisin",
		"_work/test-plan/.env":                  "SECRET=1",
		"_work/test-plan/id_rsa":                "priv",
		"_work/test-plan/PRIVATE/GLOBAL/ACC.md": "mdp",
	} {
		p := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv = httptest.NewServer(fileServer{roots: func() []string { return []string{root} }})
	t.Cleanup(srv.Close)
	return srv, proj
}

func TestTestPlan271Servi(t *testing.T) {
	srv, proj := testplanTree(t)
	for rel, want := range map[string]string{
		"_work/test-plan/x.html":     "plan",
		"_work/test-plan/sub/y.html": "plan-sub",
		"_work/mockup/m.html":        "maquette",
	} {
		if code, body := fetch(t, srv, filepath.Join(proj, rel)); code != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("%s : %d %.40q (200 attendu)", rel, code, body)
		}
	}
}

func TestTestPlan271AutresDossiersDeWorkRefuses(t *testing.T) {
	srv, proj := testplanTree(t)
	for _, rel := range []string{
		"_work/reports/r.md", "_work/handoff/h.md", "_work/tasks/t.md", "_work/tmp/secret.txt",
		"_work/test-plan-autre/z.html",                                                            // simple préfixe de « test-plan » : pas le même dossier
		"_work/test-plan/.env", "_work/test-plan/id_rsa", "_work/test-plan/PRIVATE/GLOBAL/ACC.md", // les règles générales restent valables dedans
	} {
		if code, body := fetch(t, srv, filepath.Join(proj, rel)); code != http.StatusNotFound {
			t.Errorf("%s servi : %d %.40q (404 attendu)", rel, code, body)
		}
	}
}

func TestTestPlan271Masques(t *testing.T) {
	srv, proj := testplanTree(t)
	_, w := fetch(t, srv, proj+"/_work/")
	if !strings.Contains(w, "test-plan") || !strings.Contains(w, "mockup") {
		t.Errorf("liste de _work sans test-plan/mockup : %.300q", w)
	}
	for _, hidden := range []string{"reports", "handoff", "tasks", ">tmp/<", "test-plan-autre"} {
		if strings.Contains(w, hidden) {
			t.Errorf("%s listé dans _work : %.300q", hidden, w)
		}
	}
	_, l := fetch(t, srv, proj+"/_work/test-plan/")
	if !strings.Contains(l, "x.html") || strings.Contains(l, ".env") || strings.Contains(l, "id_rsa") || strings.Contains(l, "PRIVATE") {
		t.Errorf("liste de test-plan : %.300q", l)
	}
}

func TestTestPlan271SensitiveRel(t *testing.T) {
	for rel, want := range map[string]bool{
		"_work/test-plan/x.html": false, "_work/test-plan": false, "_work/Test-Plan/a/b.html": false, "x/_work/test-plan/y.html": false,
		"_work/mockup/m.html": false,
		"_work/reports/r.md":  true, "_work/handoff/h.md": true, "_work/tasks/t.md": true, "_work/tmp": true, "_work/tmp/x": true,
		"_work/test-plan-autre/z.html": true, "_work/test-plans/z.html": true, "_work/test-plan/.env": true, "_work/test-plan/PRIVATE/x": true,
	} {
		if got := sensitiveRel(rel); got != want {
			t.Errorf("sensitiveRel(%q) = %v, attendu %v", rel, got, want)
		}
	}
}
