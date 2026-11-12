package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sans réglage : serveur sur 127.0.0.1:8765, CL_BROWSER_PATH_MAP = une seule
// entrée « / » → http://localhost:8765 (le chemin de l'URL est le chemin interne).
func TestBrowserServerDefaultMap(t *testing.T) {
	ps, src, warns := resolveBrowserPaths(backendsFile{}, "pc", []string{"/share/MFS/ROOT"}, true)
	if got := browserPathJSON(ps); got != `[{"container":"/","host":"http://localhost:8765","os":"url"}]` || len(warns) != 0 {
		t.Fatalf("%s (%s) %v", got, src, warns)
	}
	if !strings.Contains(src, "serveur web") {
		t.Fatalf("source %q", src)
	}
	// browser_paths déclarés : prioritaires, base http acceptée
	f := parseBackendsYAML(t, `
defaults:
  browser_paths:
    - { container: /share/MFS/ROOT/, host: 'http://pc:9000/mfs/' }
    - { container: /x, host: 'P:\', os: url }
`)
	ps, _, warns = resolveBrowserPaths(f, "pc", nil, false)
	if browserPathJSON(ps) != `[{"container":"/share/MFS/ROOT","host":"http://pc:9000/mfs","os":"url"}]` || len(warns) != 1 {
		t.Fatalf("déclaré : %s %v", browserPathJSON(ps), warns)
	}
}

func TestResolveBrowserServer(t *testing.T) {
	f := parseBackendsYAML(t, `
defaults:
  browser_server: { listen: "9001", allow: [/data] }
hubs:
  serveur:
    browser_server: { listen: "0.0.0.0:9002" }
  web:
    browser_server: { url: "https://cl.maison.lan/" }
  muet:
    browser_server: { listen: off }
  faux:
    browser_server: { listen: "nimporte" }
`)
	cases := []struct {
		host, listen, base string
		off, bad           bool
	}{
		{"pc", "127.0.0.1:9001", "http://localhost:9001", false, false},
		{"serveur.maison.lan", "0.0.0.0:9002", "http://serveur:9002", false, false},
		{"web", "127.0.0.1:9001", "https://cl.maison.lan", false, false},
		{"muet", "off", "", true, false},
		{"faux", "", "", true, true},
	}
	for _, c := range cases {
		got, err := resolveBrowserServer(f, c.host)
		if (err != nil) != c.bad || got.Off != c.off || !c.off && (got.Listen != c.listen || got.Base != c.base) {
			t.Errorf("%s : %+v %v", c.host, got, err)
		}
	}
	if got, _ := resolveBrowserServer(f, "serveur"); len(got.Allow) != 1 || got.Allow[0] != "/data" {
		t.Errorf("allow hérité de defaults : %+v", got)
	}
	if got, _ := resolveBrowserServer(backendsFile{}, "pc"); got.Listen != defaultBrowserListen || got.Base != "http://localhost:8765" {
		t.Errorf("défaut : %+v", got)
	}
}

func TestServedRoots(t *testing.T) {
	got := servedRoots([]string{"/a/", "relatif", "/", "/b"}, []string{"/a", "/c/../d"})
	if strings.Join(got, ",") != "/a,/b,/d" {
		t.Fatal(got)
	}
}

// Fichiers servis : racines seulement, liens qui sortent refusés, Host
// contrôlé, lecture seule, pas de cache, index.html et liste de dossier.
func TestBrowserFileServer(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "share", "PROJ")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "maq", "é t"), 0o755))
	must(os.WriteFile(filepath.Join(root, "maq", "a.html"), []byte("<p>ok</p>"), 0o644))
	must(os.WriteFile(filepath.Join(root, "maq", "é t", "n.md"), []byte("# titre"), 0o644))
	must(os.WriteFile(filepath.Join(d, "secret"), []byte("non"), 0o644))
	must(os.Symlink(filepath.Join(d, "secret"), filepath.Join(root, "fuite")))
	must(os.Symlink(filepath.Join(root, "maq", "a.html"), filepath.Join(root, "lien.html")))
	site := filepath.Join(root, "site")
	must(os.MkdirAll(site, 0o755))
	must(os.WriteFile(filepath.Join(site, "index.html"), []byte("accueil"), 0o644))

	c, _ := resolveBrowserServer(backendsFile{}, "pc")
	srv := httptest.NewServer(fileServer{hosts: c.allowedHosts("pc.maison.lan"), roots: func() []string { return []string{root} }})
	defer srv.Close()
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(method, p, host string) (int, string, http.Header) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+p, nil)
		req.Host = host
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	code, body, h := get("GET", escapePath(root+"/maq/a.html"), "localhost:8765")
	if code != 200 || body != "<p>ok</p>" || h.Get("Cache-Control") != "no-store" || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Fatalf("fichier : %d %q %v", code, body, h)
	}
	if code, body, h = get("GET", escapePath(root+"/maq/é t/n.md"), "127.0.0.1"); code != 200 || body != "# titre" || h.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("markdown, chemin encodé : %d %q %v", code, body, h)
	}
	if code, body, _ = get("GET", escapePath(root+"/lien.html"), "pc"); code != 200 || body != "<p>ok</p>" {
		t.Fatalf("lien interne : %d %q", code, body)
	}
	for _, p := range []string{d + "/secret", root + "/fuite", root + "/../secret", root + "/maq/%2e%2e/%2e%2e/../secret", "/etc/passwd", root + "/absent"} {
		if code, body, _ = get("GET", p, "localhost"); code != 404 || strings.Contains(body, "non") {
			t.Errorf("%s : %d %q", p, code, body)
		}
	}
	if code, _, _ = get("GET", root+"/maq/a.html", "piege.example.com"); code != 403 {
		t.Errorf("Host étranger : %d", code)
	}
	if code, _, _ = get("POST", root+"/maq/a.html", "localhost"); code != 405 {
		t.Errorf("POST : %d", code)
	}
	if code, _, h = get("GET", root+"/maq", "localhost"); code != 301 || h.Get("Location") != root+"/maq/" {
		t.Errorf("dossier sans / : %d %v", code, h.Get("Location"))
	}
	if code, body, _ = get("GET", root+"/maq/", "localhost"); code != 200 || !strings.Contains(body, `href="a.html"`) || !strings.Contains(body, `href="%C3%A9%20t/"`) {
		t.Errorf("liste : %d %q", code, body)
	}
	if code, body, _ = get("GET", root+"/site/", "localhost"); code != 200 || body != "accueil" {
		t.Errorf("index.html : %d %q", code, body)
	}
	if code, body, _ = get("GET", browserHealthPath, "localhost"); code != 200 || !strings.HasPrefix(body, NgName) {
		t.Errorf("santé : %d %q", code, body)
	}
}
