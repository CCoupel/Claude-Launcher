package desktop

// Tests de spécification #174 (v6.4.0) — route GET /files/<chemin interne> du serveur web : racines servies, sortie
// par « .. » et lien symbolique refusées, en-têtes CSP sandbox / nosniff / no-store, liste de dossier, contrôles Host
// et Sec-Fetch-Site, session en mode token, aucune session en mode none, appariement sous /files. Contrat :
// contracts/web-server.md « Fichiers des projets par l'interface ». Écrits avant l'implémentation (TDD).

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// files174 : interface web allumée, arborescence de test sous la racine des projets (CL_ROOTS).
type files174 struct {
	port    int
	tok     string
	root    string // racine servie
	outside string // dossier hors racine
}

func files174Start(t *testing.T, mode string) *files174 {
	t.Helper()
	if mode != "" {
		t.Setenv("CL_WEB_ACCESS", mode)
	}
	var port int
	var tok string
	if mode == "none" { // aucun jeton ni session : seul le démarrage de l'interface (w171Start lit le jeton du poste)
		sandbox(t, "", "alpha")
		port = l4FreePort(t)
		l4WriteBackends(t, "  desktop_web: { enabled: true, listen: \"127.0.0.1:"+strconv.Itoa(port)+"\" }\n")
		a := l4StartNative(t)
		l4WaitWebUI(t, a, "on", 8*time.Second)
	} else {
		_, port, tok = w171Start(t, "")
	}
	root := os.Getenv("CL_ROOTS")
	f := &files174{port: port, tok: tok, root: root, outside: t.TempDir()}
	w := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(root, "TEST", "alpha", "doc", "maquette.html"), "<html>MAQUETTE-174</html>")
	w(filepath.Join(root, "TEST", "alpha", "doc", "notes.md"), "# notes 174")
	w(filepath.Join(f.outside, "secret.txt"), "SECRET-HORS-RACINE")
	if err := os.Symlink(f.outside, filepath.Join(root, "TEST", "alpha", "sortie")); err != nil {
		t.Skip("liens symboliques indisponibles : " + err.Error())
	}
	if err := os.Symlink(filepath.Join(root, "TEST", "alpha", "doc"), filepath.Join(root, "TEST", "alpha", "interne")); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *files174) get(t *testing.T, path string, hdr map[string]string, withSession bool) (*http.Response, string) {
	t.Helper()
	var ck []*http.Cookie
	if withSession && f.tok != "" {
		c, _ := w171Session(t, f.port, f.tok, "")
		if c != nil {
			ck = append(ck, c)
		}
	}
	return w171Do(t, f.port, "GET", path, "", hdr, "", ck...)
}

func (f *files174) url(rel string) string { return "/files" + f.root + "/TEST/alpha/" + rel }

func TestFilesServiEtEnTetes174(t *testing.T) {
	f := files174Start(t, "")
	resp, body := f.get(t, f.url("doc/maquette.html"), nil, true)
	if resp.StatusCode != 200 || !strings.Contains(body, "MAQUETTE-174") {
		t.Fatalf("fichier d'une racine : %d %q", resp.StatusCode, body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, attendu sandbox (la maquette ne s'exécute pas dans l'origine de l'interface)", csp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("X-Content-Type-Options: nosniff absent")
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control = %q, attendu no-store", resp.Header.Get("Cache-Control"))
	}
	// texte : affiché, pas téléchargé
	resp, body = f.get(t, f.url("doc/notes.md"), nil, true)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || !strings.Contains(body, "notes 174") {
		t.Errorf("fichier texte : %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

func TestFilesListeDeDossier174(t *testing.T) {
	f := files174Start(t, "")
	resp, body := f.get(t, f.url("doc/"), nil, true)
	if resp.StatusCode != 200 || !strings.Contains(body, "maquette.html") || !strings.Contains(body, "notes.md") {
		t.Errorf("liste du dossier : %d %q", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("CSP sandbox absente de la liste de dossier")
	}
	resp, _ = f.get(t, f.url("doc"), nil, true) // sans « / » final : redirection
	if resp.StatusCode != 301 || !strings.HasSuffix(resp.Header.Get("Location"), "/doc/") {
		t.Errorf("dossier sans / final : %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestFilesRienHorsRacine174(t *testing.T) {
	f := files174Start(t, "")
	for _, c := range []struct{ name, path string }{
		{"hors de toute racine", "/files/etc/passwd"},
		{"« .. » en clair", "/files" + f.root + "/TEST/alpha/doc/../../../../../etc/passwd"},
		{"« .. » encodé", "/files" + f.root + "/TEST/alpha/%2e%2e/%2e%2e/%2e%2e/etc/passwd"},
		{"« .. » double encodage", "/files" + f.root + "/TEST/alpha/%252e%252e/%252e%252e/etc/passwd"},
		{"lien symbolique sortant (fichier)", f.url("sortie/secret.txt")},
		{"lien symbolique sortant (dossier)", f.url("sortie/")},
		{"fichier du coffre", "/files" + os.Getenv("CL_SECRETS_DIR") + "/x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, body := f.get(t, c.path, nil, true)
			if resp.StatusCode == 200 || strings.Contains(body, "SECRET-HORS-RACINE") || strings.Contains(body, "root:x:") {
				t.Errorf("contenu hors racine servi : %d %.80q", resp.StatusCode, body)
			}
		})
	}
	// un lien qui reste dans la racine est servi
	if resp, body := f.get(t, f.url("interne/maquette.html"), nil, true); resp.StatusCode != 200 || !strings.Contains(body, "MAQUETTE-174") {
		t.Errorf("lien interne à la racine : %d %q", resp.StatusCode, body)
	}
}

func TestFilesLectureSeule174(t *testing.T) {
	f := files174Start(t, "")
	c, _ := w171Session(t, f.port, f.tok, "")
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		resp, _ := w171Do(t, f.port, m, f.url("doc/maquette.html"), "", nil, "x", c)
		if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 204 {
			t.Errorf("%s accepté sur /files : %d", m, resp.StatusCode)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(f.root, "TEST", "alpha", "doc", "maquette.html")); !strings.Contains(string(b), "MAQUETTE-174") {
		t.Error("fichier modifié par une requête d'écriture")
	}
}

func TestFilesControlesHostEtOrigine174(t *testing.T) {
	f := files174Start(t, "")
	c, _ := w171Session(t, f.port, f.tok, "")
	if resp, _ := w171Do(t, f.port, "GET", f.url("doc/maquette.html"), "evil.example", nil, "", c); resp.StatusCode != 403 {
		t.Errorf("Host étranger : %d, attendu 403", resp.StatusCode)
	}
	if resp, _ := w171Do(t, f.port, "GET", f.url("doc/maquette.html"), "", map[string]string{"Sec-Fetch-Site": "cross-site"}, "", c); resp.StatusCode != 403 {
		t.Errorf("Sec-Fetch-Site: cross-site : %d, attendu 403 (comme une page)", resp.StatusCode)
	}
	if resp, _ := w171Do(t, f.port, "GET", f.url("doc/maquette.html"), "", map[string]string{"Sec-Fetch-Site": "same-origin"}, "", c); resp.StatusCode != 200 {
		t.Errorf("Sec-Fetch-Site: same-origin : %d, attendu 200", resp.StatusCode)
	}
}

func TestFilesSessionExigeeEnModeToken174(t *testing.T) {
	f := files174Start(t, "token")
	resp, body := f.get(t, f.url("doc/maquette.html"), nil, false)
	if resp.StatusCode == 200 || strings.Contains(body, "MAQUETTE-174") {
		t.Errorf("mode token sans session : %d (session exigée)", resp.StatusCode)
	}
	if resp, _ := f.get(t, f.url("doc/maquette.html"), nil, true); resp.StatusCode != 200 {
		t.Errorf("mode token avec session : %d", resp.StatusCode)
	}
}

func TestFilesSansSessionEnModeNone174(t *testing.T) {
	f := files174Start(t, "none")
	resp, body := f.get(t, f.url("doc/maquette.html"), nil, false)
	if resp.StatusCode != 200 || !strings.Contains(body, "MAQUETTE-174") {
		t.Errorf("mode none sans session : %d %q (la protection est l'ingress interne)", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("CSP sandbox absente en mode none")
	}
}

// Appariement sous /files (le serveur de maquettes en boucle locale n'est plus annoncé en cluster).
func TestFilesAppariement174(t *testing.T) {
	f := files174Start(t, "none")
	resp, _ := f.get(t, "/files/__claude-launcher__/pair?device=abcdef123456&token=mauvais-jeton", nil, false)
	if resp.StatusCode == 404 || resp.StatusCode == 200 {
		t.Errorf("appariement avec un mauvais jeton : %d (route présente, jeton refusé attendu : 401/403)", resp.StatusCode)
	}
	resp, _ = f.get(t, "/files/__claude-launcher__/pair?device=abcdef123456&token=", nil, false)
	if resp.StatusCode == 200 {
		t.Error("appariement sans jeton accepté")
	}
}
