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

// LOT-3.2 Batch 0 (#275, T23) — réponses de /files : un fichier d'un conteneur d'utilisateur est NON FIABLE (dépôt hostile) : jamais
// interprété comme une page de l'origine du launcher. Tous les types actifs (HTML, SVG, JS, XML…) sont servis avec « nosniff » et une
// Content-Security-Policy « sandbox » (sans allow-same-origin). Écrit avant le code ; fileServer avec csp = filesCSP comme en production.

func TestT23EnTetesDesFichiersServis(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "PROJETS", "p")
	files := map[string]string{"a.html": "<script>alert(1)</script>", "b.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"c.js": "alert(1)", "d.xml": "<x/>", "e.xhtml": "<html/>", "f.txt": "ok", "g.md": "# t"}
	for n, c := range files {
		if err := os.MkdirAll(proj, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proj, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(fileServer{roots: func() []string { return []string{root} }, csp: filesCSP})
	defer srv.Close()
	for n := range files {
		resp, err := http.Get(srv.URL + escapePath(filepath.Join(proj, n)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s : %d", n, resp.StatusCode)
			continue
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s : X-Content-Type-Options = %q", n, resp.Header.Get("X-Content-Type-Options"))
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-same-origin") {
			t.Errorf("%s : Content-Security-Policy = %q (sandbox sans allow-same-origin attendu)", n, csp)
		}
	}
}
