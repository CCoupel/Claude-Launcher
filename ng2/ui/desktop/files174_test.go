package desktop

// Tests de spécification #174 (v6.4.0) — route GET /files/<projet>/<chemin relatif> du serveur web (forme unique,
// contrat 8b8e0cb, décision utilisateur 2026-11-18) : projet servi, projet inconnu / ancienne forme à chemin interne /
// racine /files/ en 404, noms réservés en « __ », sortie de la racine DU PROJET par « .. » ou par lien refusée,
// en-têtes CSP sandbox / nosniff / no-store, lecture seule, Host et Sec-Fetch-Site, session en mode token, aucune
// session en mode none, appariement. Exclusions (.env, _work…) et sous-ressources : files174_dest_test.go.
// <projet> = nom du launcher (core.CtrName : <catégorie>-<projet>, celui des StatefulSet).

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

type files174 struct {
	port         int
	tok          string
	alpha, beta  string // chemins des projets (racine de chacun)
	nameA, nameB string // noms du launcher
	outside      string // dossier hors de tout projet
}

func files174Start(t *testing.T, mode string) *files174 {
	t.Helper()
	if mode != "" {
		t.Setenv("CL_WEB_ACCESS", mode)
	}
	var port int
	var tok string
	var app *App
	if mode == "none" { // aucun jeton ni session : seul le démarrage de l'interface (w171Start lit le jeton du poste)
		sandbox(t, "", "alpha", "beta")
		port = l4FreePort(t)
		l4WriteBackends(t, "  desktop_web: { enabled: true, listen: \"127.0.0.1:"+strconv.Itoa(port)+"\" }\n")
		a := l4StartNative(t)
		l4WaitWebUI(t, a, "on", 8*time.Second)
		app = a
	} else {
		app, port, tok = w171Start(t, "")
	}
	root := os.Getenv("CL_ROOTS")
	f := &files174{port: port, tok: tok, alpha: filepath.Join(root, "TEST", "alpha"), beta: filepath.Join(root, "TEST", "beta"), outside: t.TempDir()}
	// w171Start ne crée que « alpha » : beta est un second projet git, CONNU du launcher (sans quoi /files/<beta> = 404)
	_ = os.MkdirAll(f.beta, 0o755)
	if out, err := exec.Command("git", "-C", f.beta, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	app.rescan()
	// beta est un projet connu du launcher (dépôt git relu par l'arbre) : /files/<projet> ne sert que les projets connus
	if _, err := os.Stat(filepath.Join(f.beta, ".git")); err != nil {
		_ = exec.Command("git", "-C", f.beta, "init", "-q").Run()
	}
	app.rescan()
	f.nameA, f.nameB = core.CtrName(f.alpha), core.CtrName(f.beta)
	w := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(f.alpha, "doc", "maquette.html"), "<html>MAQUETTE-174</html>")
	w(filepath.Join(f.alpha, "doc", "notes.md"), "# notes 174")
	w(filepath.Join(f.beta, "doc", "autre.txt"), "FICHIER-DU-PROJET-BETA")
	w(filepath.Join(f.outside, "secret.txt"), "SECRET-HORS-PROJET")
	for link, target := range map[string]string{
		filepath.Join(f.alpha, "sortie"):      f.outside,                     // hors de tout projet
		filepath.Join(f.alpha, "versbeta"):    filepath.Join(f.beta, "doc"),  // autre projet : hors de LA racine du projet
		filepath.Join(f.alpha, "interne"):     filepath.Join(f.alpha, "doc"), // reste dans le projet
		filepath.Join(f.alpha, "lienfichier"): filepath.Join(f.outside, "secret.txt"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Skip("liens symboliques indisponibles : " + err.Error())
		}
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

// url : /files/<projet alpha>/<chemin relatif>
func (f *files174) url(rel string) string { return "/files/" + f.nameA + "/" + rel }

func TestFilesProjetServiEtEnTetes174(t *testing.T) {
	f := files174Start(t, "")
	resp, body := f.get(t, f.url("doc/maquette.html"), nil, true)
	if resp.StatusCode != 200 || !strings.Contains(body, "MAQUETTE-174") {
		t.Fatalf("fichier d'un projet : %d %q", resp.StatusCode, body)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-same-origin") {
		t.Errorf("Content-Security-Policy = %q, attendu sandbox sans allow-same-origin (origine opaque)", csp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("X-Content-Type-Options: nosniff absent")
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		t.Errorf("Cache-Control = %q, attendu no-store", resp.Header.Get("Cache-Control"))
	}
	resp, body = f.get(t, f.url("doc/notes.md"), nil, true)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || !strings.Contains(body, "notes 174") {
		t.Errorf("fichier texte : %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

func TestFilesListeDeDossierDansLeProjet174(t *testing.T) {
	f := files174Start(t, "")
	resp, body := f.get(t, f.url("doc/"), nil, true)
	if resp.StatusCode != 200 || !strings.Contains(body, "maquette.html") || !strings.Contains(body, "notes.md") {
		t.Errorf("liste du dossier : %d %q", resp.StatusCode, body)
	}
	if strings.Contains(body, f.alpha) || strings.Contains(body, os.Getenv("CL_ROOTS")) {
		t.Errorf("le chemin interne apparaît dans la liste : %s", body)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("CSP sandbox absente de la liste de dossier")
	}
	// la racine du projet se liste ; ses sous-dossiers y figurent, pas ceux des autres projets
	resp, body = f.get(t, "/files/"+f.nameA+"/", nil, true)
	if resp.StatusCode != 200 || !strings.Contains(body, "doc") {
		t.Errorf("racine du projet : %d %q", resp.StatusCode, body)
	}
	// dossier sans « / » final : redirection qui ne révèle pas le chemin interne
	resp, _ = f.get(t, f.url("doc"), nil, true)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != 301 || !strings.HasSuffix(loc, "/files/"+f.nameA+"/doc/") || strings.Contains(loc, os.Getenv("CL_ROOTS")) {
		t.Errorf("dossier sans / final : %d → %q", resp.StatusCode, loc)
	}
}

func TestFilesProjetInconnuEtAncienneForme404174(t *testing.T) {
	f := files174Start(t, "")
	for _, c := range []struct{ name, path string }{
		{"projet inconnu", "/files/projet-inconnu/doc/maquette.html"},
		{"projet inconnu, racine", "/files/projet-inconnu/"},
		{"ancienne forme : chemin interne complet", "/files" + f.alpha + "/doc/maquette.html"},
		{"ancienne forme : racine des projets", "/files" + os.Getenv("CL_ROOTS") + "/"},
		{"chemin système", "/files/etc/passwd"},
		{"casse du nom différente d'un projet inconnu", "/files/" + strings.ToUpper(f.nameA) + "x/doc/maquette.html"},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, body := f.get(t, c.path, nil, true)
			if resp.StatusCode != 404 || strings.Contains(body, "MAQUETTE-174") || strings.Contains(body, "root:x:") {
				t.Errorf("%d %.80q, attendu 404 sans contenu", resp.StatusCode, body)
			}
		})
	}
}

func TestFilesRacineSansListe404174(t *testing.T) {
	f := files174Start(t, "")
	for _, p := range []string{"/files/", "/files", "/files//"} {
		resp, body := f.get(t, p, nil, true)
		if resp.StatusCode != 404 {
			t.Errorf("%s : %d, attendu 404", p, resp.StatusCode)
		}
		if strings.Contains(body, f.nameA) || strings.Contains(body, f.nameB) {
			t.Errorf("%s : la racine ne doit révéler aucun nom de projet : %q", p, body)
		}
	}
}

func TestFilesNomsReserves174(t *testing.T) {
	f := files174Start(t, "")
	for _, p := range []string{
		"/files/__claude-launcher__/doc/maquette.html", "/files/__claude-launcher__/", "/files/__claude-launcher__/inconnu",
		"/files/__" + f.nameA + "/doc/maquette.html", "/files/__/x",
	} {
		resp, body := f.get(t, p, nil, true)
		if resp.StatusCode != 404 || strings.Contains(body, "MAQUETTE-174") {
			t.Errorf("%s : %d, attendu 404 (premier segment en « __ » réservé au launcher)", p, resp.StatusCode)
		}
	}
}

func TestFilesRienHorsDeLaRacineDuProjet174(t *testing.T) {
	f := files174Start(t, "")
	secrets := []string{"SECRET-HORS-PROJET", "FICHIER-DU-PROJET-BETA", "root:x:"}
	for _, c := range []struct{ name, path string }{
		{"« .. » vers un autre projet", f.url("../" + f.nameB + "/doc/autre.txt")},
		{"« .. » vers la racine des projets", f.url("../../etc/passwd")},
		{"« .. » en clair", f.url("doc/../../../../../etc/passwd")},
		{"« .. » encodé", "/files/" + f.nameA + "/%2e%2e/%2e%2e/%2e%2e/etc/passwd"},
		{"« .. » double encodage", "/files/" + f.nameA + "/%252e%252e/%252e%252e/etc/passwd"},
		{"« .. » vers un autre projet, encodé", "/files/" + f.nameA + "/%2e%2e/" + f.nameB + "/doc/autre.txt"},
		{"lien sortant (fichier)", f.url("lienfichier")},
		{"lien sortant (dossier hors projets)", f.url("sortie/secret.txt")},
		{"lien sortant (liste)", f.url("sortie/")},
		{"lien vers un AUTRE projet (hors de LA racine du projet)", f.url("versbeta/autre.txt")},
		{"lien vers un autre projet (liste)", f.url("versbeta/")},
		{"fichier du coffre", "/files/" + f.nameA + "/" + os.Getenv("CL_SECRETS_DIR") + "/x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, body := f.get(t, c.path, nil, true)
			if resp.StatusCode == 200 {
				t.Errorf("servi : %d %.80q", resp.StatusCode, body)
			}
			for _, s := range secrets {
				if strings.Contains(body, s) {
					t.Errorf("contenu hors de la racine du projet servi (%s) : %.80q", s, body)
				}
			}
		})
	}
	// un lien qui reste dans la racine du projet est servi
	if resp, body := f.get(t, f.url("interne/maquette.html"), nil, true); resp.StatusCode != 200 || !strings.Contains(body, "MAQUETTE-174") {
		t.Errorf("lien interne au projet : %d %q", resp.StatusCode, body)
	}
	// le projet beta se sert lui-même, mais avec son propre nom
	if resp, body := f.get(t, "/files/"+f.nameB+"/doc/autre.txt", nil, true); resp.StatusCode != 200 || !strings.Contains(body, "FICHIER-DU-PROJET-BETA") {
		t.Errorf("second projet : %d %q", resp.StatusCode, body)
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
	if b, _ := os.ReadFile(filepath.Join(f.alpha, "doc", "maquette.html")); !strings.Contains(string(b), "MAQUETTE-174") {
		t.Error("fichier modifié par une requête d'écriture")
	}
}

func TestFilesControlesHostEtOrigine174(t *testing.T) {
	f := files174Start(t, "")
	c, _ := w171Session(t, f.port, f.tok, "")
	if resp, _ := w171Do(t, f.port, "GET", f.url("doc/maquette.html"), "evil.example", nil, "", c); resp.StatusCode != 403 {
		t.Errorf("Host étranger : %d, attendu 403", resp.StatusCode)
	}
	// sous-ressource / fetch d'un autre site refusé ; même origine accepté ; la navigation de premier niveau est
	// acceptée (contrat) : détail dans files174_dest_test.go
	if resp, _ := w171Do(t, f.port, "GET", f.url("doc/maquette.html"), "", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors"}, "", c); resp.StatusCode != 403 {
		t.Errorf("fetch cross-site : %d, attendu 403", resp.StatusCode)
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
	// l'inconnu reste 404 sans session aussi
	if resp, _ := f.get(t, "/files/projet-inconnu/x", nil, false); resp.StatusCode != 404 {
		t.Errorf("projet inconnu en mode none : %d, attendu 404", resp.StatusCode)
	}
}

// Appariement : chemin réservé /files/__claude-launcher__/pair, jamais résolu comme un projet.
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
