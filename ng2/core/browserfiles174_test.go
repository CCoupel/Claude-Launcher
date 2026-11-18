//go:build !windows

package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Mode serveur dans un cluster (#174) : chaque session reçoit la correspondance de SON projet vers
// <CL_WEB_URL>/files/<projet> ; l'appariement est sous /files/__claude-launcher__/pair ; hors cluster, rien ne change.
func TestBrowserEnvEnClusterParProjet(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_IN_CLUSTER", "1")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local/")
	proj := "/share/ROOT/PROJETS/QUALIF/factice-1"
	name := CtrName(proj)
	env := map[string]string{}
	for _, v := range BrowserEnvVarsFor(proj) {
		env[v.K] = v.V
	}
	var ps []BrowserPath
	if err := json.Unmarshal([]byte(env[browserPathVar]), &ps); err != nil || len(ps) != 1 ||
		ps[0].Container != proj || ps[0].Host != "https://launcher-qualif.local/files/"+name || ps[0].OS != "url" {
		t.Fatalf("CL_BROWSER_PATH_MAP = %s (%v)", env[browserPathVar], err)
	}
	hint := env[browserHintVar]
	for _, want := range []string{"https://launcher-qualif.local/files/" + name + "/<chemin du fichier relatif à la racine du projet " + proj,
		"https://launcher-qualif.local/files/__claude-launcher__/pair?device=<"} {
		if !strings.Contains(hint, want) {
			t.Errorf("CL_BROWSER_HINT sans %q : %s", want, hint)
		}
	}
	if strings.Contains(hint, "8765") || strings.Contains(hint, "localhost") || strings.Contains(hint, "/files/"+name+"/__claude-launcher__") {
		t.Errorf("hint : serveur local annoncé ou appariement sous le projet : %s", hint)
	}
	// sans projet (environnement commun) : aucune URL — un fichier hors d'un projet n'en a pas
	for _, v := range BrowserEnvVars() {
		if v.K == browserPathVar && v.V != "[]" {
			t.Errorf("sans projet, en cluster : %s, attendu []", v.V)
		}
	}
}

func TestBrowserEnvHorsClusterInchange(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_IN_CLUSTER", "0")
	t.Setenv("CL_WEB_URL", "https://launcher-qualif.local")
	if ServerFilesBase() != "" {
		t.Error("ServerFilesBase hors cluster doit être vide")
	}
	for _, v := range BrowserEnvVarsFor("/share/ROOT/PROJETS/p") {
		if strings.Contains(v.V, "/files") {
			t.Errorf("%s annonce /files hors cluster : %s", v.K, v.V)
		}
	}
}

func TestServerFilesBase(t *testing.T) {
	t.Setenv("CL_IN_CLUSTER", "1")
	for in, want := range map[string]string{"https://x.local": "https://x.local/files", "http://10.0.0.1:8080/": "http://10.0.0.1:8080/files", "": "", "ftp://x": "", "x.local": ""} {
		t.Setenv("CL_WEB_URL", in)
		if got := ServerFilesBase(); got != want {
			t.Errorf("CL_WEB_URL=%q : %q, attendu %q", in, got, want)
		}
	}
}

// NewFilesHandler : forme unique /files/<projet>/<relatif>, aucun chemin interne dans une URL ni une liste.
func TestFilesHandlerFormeProjet(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "PROJETS", "QUALIF", "factice-1")
	for rel, c := range map[string]string{"doc/m.html": "maq", "doc/sous/x.txt": "x", "doc/.env": "SECRET", "_work/mockup/a.html": "a", "_work/reports/r.md": "r"} {
		p := filepath.Join(proj, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(c), 0o644)
	}
	name := CtrName(proj)
	t.Setenv("CL_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	h := NewFilesHandler(func(n string) (string, bool) {
		if n == name {
			return proj, true
		}
		return "", false
	})
	do := func(p string) (int, string, http.Header) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		b, _ := io.ReadAll(w.Result().Body)
		return w.Code, string(b), w.Header()
	}
	if c, b, _ := do("/files/" + name + "/doc/m.html"); c != 200 || b != "maq" {
		t.Errorf("fichier : %d %q", c, b)
	}
	// 404 : racine, projet inconnu, ancienne forme (chemin interne), noms réservés, appariement hors « pair »
	for _, p := range []string{"/files", "/files/", "/files//", "/files/inconnu/doc/m.html", "/files" + proj + "/doc/m.html", "/files/__x/doc",
		"/files/__claude-launcher__", "/files/__claude-launcher__/autre", "/files/" + name + "/../" + name + "/doc/.env", "/files/" + name + "/doc/.env",
		"/files/" + name + "/_work/reports/r.md"} {
		if c, b, _ := do(p); c != 404 {
			t.Errorf("%s : %d %.40q (404 attendu)", p, c, b)
		}
	}
	// redirection et liste : jamais le chemin interne
	c, _, hd := do("/files/" + name + "/doc")
	if c != 301 || hd.Get("Location") != "/files/"+name+"/doc/" {
		t.Errorf("dossier sans / : %d %q", c, hd.Get("Location"))
	}
	if c, b, _ := do("/files/" + name + "/"); c != 200 || strings.Contains(b, proj) || strings.Contains(b, root) || !strings.Contains(b, "doc") || !strings.Contains(b, "_work") {
		t.Errorf("liste du projet : %d %.200q", c, b)
	}
	if _, b, _ := do("/files/" + name + "/doc/"); strings.Contains(b, proj) || !strings.Contains(b, "m.html") || strings.Contains(b, ".env") {
		t.Errorf("liste de doc : %.200q", b)
	}
	// racine du projet : pas de lien « ../ » ; sous-dossier : « ../ » relatif
	if _, b, _ := do("/files/" + name + "/"); strings.Contains(b, "../") {
		t.Errorf("« ../ » à la racine du projet : %.200q", b)
	}
	if _, b, _ := do("/files/" + name + "/doc/sous/"); !strings.Contains(b, `href="../"`) {
		t.Errorf("« ../ » absent d'un sous-dossier : %.200q", b)
	}
	// appariement : réservé, jeton du launcher
	tok := BrowserPairToken()
	if c, b, _ := do("/files/__claude-launcher__/pair?device=abcdef123456&token=" + tok); c != 200 || !strings.Contains(b, "abcdef123456") {
		t.Errorf("appariement : %d %.100q", c, b)
	}
	if c, _, _ := do("/files/__claude-launcher__/pair?device=abcdef123456&token=faux"); c != 403 {
		t.Errorf("appariement, jeton faux : %d, attendu 403", c)
	}
}
