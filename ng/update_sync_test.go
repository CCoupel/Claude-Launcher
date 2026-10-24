package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncInitProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_SCRIPT", filepath.Join(dir, "claude-launcher.sh"))
	dst := filepath.Join(dir, "init-project.md")

	status, body := 200, "version A"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/CCoupel/claude_project_template/abc123/init-project.md" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	old := rawBase
	rawBase = srv.URL
	defer func() { rawBase = old }()

	if !syncInitProject("abc123") {
		t.Fatal("la synchronisation aurait dû réussir")
	}
	if b, _ := os.ReadFile(dst); string(b) != "version A" {
		t.Fatalf("contenu = %q", b)
	}
	// le cache n'est écrasé ni par une erreur, ni par une réponse vide, ni par un mauvais commit
	status, body = 500, "cassé"
	if syncInitProject("abc123") {
		t.Error("une erreur serveur ne doit pas compter comme un succès")
	}
	status, body = 200, ""
	if syncInitProject("abc123") {
		t.Error("une réponse vide ne doit pas compter comme un succès")
	}
	status, body = 200, "version B"
	if syncInitProject("autre-commit") {
		t.Error("un commit inconnu (404) ne doit pas compter comme un succès")
	}
	if b, _ := os.ReadFile(dst); string(b) != "version A" {
		t.Fatalf("le cache a été écrasé : %q", b)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o600 {
		t.Errorf("droits = %v, attendu 0600 (comme le bash)", st.Mode().Perm())
	}
}
