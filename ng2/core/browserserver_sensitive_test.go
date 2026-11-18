//go:build !windows

package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Éléments sensibles (audit sécurité LOT-4) : jamais servis ni listés par le serveur de fichiers (route /files et serveur
// de maquettes local), au chemin demandé comme après résolution des liens ; les maquettes de _work/mockup restent servies.
func sensitiveTree(t *testing.T) (srv *httptest.Server, root string) {
	t.Helper()
	root = t.TempDir()
	proj := filepath.Join(root, "PROJETS", "p")
	files := map[string]string{
		"ok.html":                   "ok",
		".env":                      "SECRET=1",
		".env.local":                "SECRET=2",
		".git/config":               "[core]",
		".claude/settings.json":     "{}",
		"PRIVATE/GLOBAL/ACCES.md":   "mot de passe",
		"PRIVATE/QUALIF/kubeconfig": "apiVersion: v1",
		"srv.pem":                   "-----BEGIN",
		"a.key":                     "key",
		"id_ed25519":                "priv",
		"id_rsa.pub":                "pub",
		"kubeconfig":                "kc",
		"kubeconfig-extra":          "kc",
		"sub/.env":                  "SECRET=3",
		"sub/ok.txt":                "ok",
		"_work/tmp/secret.txt":      "tmp",
		"_work/mockup/m.html":       "<h1>maquette</h1>",
		"_work/mockup/ui/a.html":    "ui",
		"_work/reports/r.md":        "rapport",
	}
	for rel, c := range files {
		p := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// liens vers des éléments sensibles, depuis un chemin d'apparence anodine
	for name, target := range map[string]string{"lien-git.txt": ".git/config", "lien-prive": "PRIVATE", "lien-env.txt": ".env", "lien-tmp": "_work/tmp"} {
		if err := os.Symlink(filepath.Join(proj, target), filepath.Join(proj, name)); err != nil {
			t.Skip("liens symboliques indisponibles : ", err)
		}
	}
	fs := fileServer{roots: func() []string { return []string{root} }}
	srv = httptest.NewServer(fs)
	t.Cleanup(srv.Close)
	return srv, proj
}

func fetch(t *testing.T, srv *httptest.Server, p string) (int, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + escapePath(p))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestSensibleJamaisServi(t *testing.T) {
	srv, proj := sensitiveTree(t)
	for _, rel := range []string{
		".env", ".env.local", ".git/config", ".claude/settings.json", "PRIVATE/GLOBAL/ACCES.md", "PRIVATE/QUALIF/kubeconfig",
		"srv.pem", "a.key", "id_ed25519", "id_rsa.pub", "kubeconfig", "kubeconfig-extra", "sub/.env", "_work/tmp/secret.txt",
		"lien-git.txt", "lien-prive/GLOBAL/ACCES.md", "lien-env.txt", "lien-tmp/secret.txt", ".ENV", ".Git/config", "PRIVATE/",
	} {
		if code, body := fetch(t, srv, filepath.Join(proj, rel)); code != http.StatusNotFound {
			t.Errorf("%s servi : %d %.40q (404 attendu)", rel, code, body)
		}
	}
}

func TestMaquettesEtFichiersOrdinairesServis(t *testing.T) {
	srv, proj := sensitiveTree(t)
	for rel, want := range map[string]string{"ok.html": "ok", "sub/ok.txt": "ok", "_work/mockup/m.html": "maquette", "_work/mockup/ui/a.html": "ui", "_work/reports/r.md": "rapport"} {
		if code, body := fetch(t, srv, filepath.Join(proj, rel)); code != 200 || !strings.Contains(body, want) {
			t.Errorf("%s : %d %.40q (servi attendu)", rel, code, body)
		}
	}
}

func TestSensibleMasqueDansLesListes(t *testing.T) {
	srv, proj := sensitiveTree(t)
	code, body := fetch(t, srv, proj+"/")
	if code != 200 || !strings.Contains(body, "ok.html") || !strings.Contains(body, "_work") {
		t.Fatalf("liste du projet : %d %.200q", code, body)
	}
	for _, hidden := range []string{".env", ".git", ".claude", "PRIVATE", "srv.pem", "a.key", "id_ed25519", "id_rsa.pub", "kubeconfig", "lien-git.txt", "lien-prive", "lien-env.txt", "lien-tmp"} {
		if strings.Contains(body, ">"+hidden+"<") || strings.Contains(body, ">"+hidden+"/<") {
			t.Errorf("%s listé", hidden)
		}
	}
	// _work : mockup et reports listés, tmp masqué ; sub : .env masqué
	if _, b := fetch(t, srv, proj+"/_work/"); !strings.Contains(b, "mockup") || !strings.Contains(b, "reports") || strings.Contains(b, ">tmp/<") {
		t.Errorf("liste de _work : %.200q", b)
	}
	if _, b := fetch(t, srv, proj+"/sub/"); !strings.Contains(b, "ok.txt") || strings.Contains(b, ".env") {
		t.Errorf("liste de sub : %.200q", b)
	}
}

func TestSensibleRel(t *testing.T) {
	for rel, want := range map[string]bool{
		"": false, "a/b.html": false, "_work/mockup/m.html": false, "_work/tmp": true, "x/_WORK/TMP/y": true, "docs/private-notes.md": false,
		"PRIVATE/x": true, "a/.git/HEAD": true, ".gitignore": false, ".envrc": true, "envoi.txt": false, "sec.pem": true, "a/B.KEY": true,
		"id_rsa": true, "idea.txt": false, "kubeconfig": true, "x/kubeconfig-extra": true, "mykubeconfig": false, ".github/x": false,
	} {
		if got := sensitiveRel(rel); got != want {
			t.Errorf("sensitiveRel(%q) = %v, attendu %v", rel, got, want)
		}
	}
}
