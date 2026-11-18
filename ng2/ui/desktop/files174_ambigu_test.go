package desktop

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// Un nom de projet porté par plusieurs projets (même catégorie et même nom sous des racines différentes) est ambigu :
// 404 sans rien servir, ambiguïté journalisée côté serveur, aucun chemin dans la réponse (#174).
func TestFilesNomDeProjetAmbigu(t *testing.T) {
	root1, root2 := t.TempDir(), t.TempDir()
	p1, p2 := filepath.Join(root1, "TEST", "alpha"), filepath.Join(root2, "TEST", "alpha")
	p3 := filepath.Join(root1, "TEST", "unique")
	for _, p := range []string{p1, p2, p3} {
		_ = os.MkdirAll(p, 0o755)
		_ = os.WriteFile(filepath.Join(p, "secret-"+filepath.Base(root1)+".txt"), []byte("DU-PROJET-1"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(p1, "m.html"), []byte("PROJET-1"), 0o644)
	_ = os.WriteFile(filepath.Join(p2, "m.html"), []byte("PROJET-2"), 0o644)
	_ = os.WriteFile(filepath.Join(p3, "m.html"), []byte("UNIQUE"), 0o644)
	if core.CtrName(p1) != core.CtrName(p2) || core.CtrName(p1) == core.CtrName(p3) {
		t.Fatalf("fixture : noms %q %q %q", core.CtrName(p1), core.CtrName(p2), core.CtrName(p3))
	}
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.mu.Lock()
	a.nodes = []core.Node{{Name: "alpha", Path: p1, Project: true}, {Name: "alpha", Path: p2, Project: true}, {Name: "unique", Path: p3, Project: true}}
	a.mu.Unlock()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	if root, ok := a.projectRootByName(core.CtrName(p1)); ok || root != "" {
		t.Errorf("nom ambigu résolu : %q %v", root, ok)
	}
	if !strings.Contains(logs.String(), core.CtrName(p1)) || !strings.Contains(logs.String(), "ambigu") {
		t.Errorf("ambiguïté non journalisée : %q", logs.String())
	}
	if root, ok := a.projectRootByName(core.CtrName(p3)); !ok || root != core.RemotePath(p3) {
		t.Errorf("nom unique non résolu : %q %v", root, ok)
	}
	// par la route : 404 sans contenu ni chemin ; le projet de nom unique reste servi
	w := &webBridge{app: a, token: "tok174tok174tok174", subs: map[chan []byte]bool{}, strictHost: true, access: core.AccessNone}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	get := func(p string) (int, string) {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return resp.StatusCode, b.String()
	}
	code, body := get("/files/" + core.CtrName(p1) + "/m.html")
	if code != 404 || strings.Contains(body, "PROJET-") || strings.Contains(body, root1) || strings.Contains(body, root2) {
		t.Errorf("nom ambigu servi : %d %q", code, body)
	}
	if code, body := get("/files/" + core.CtrName(p3) + "/m.html"); code != 200 || body != "UNIQUE" {
		t.Errorf("projet de nom unique : %d %q", code, body)
	}
}
