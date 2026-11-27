package desktop

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #255 (test-writer) — isolement des identités, de bout en bout par RunWeb (mode proxy, comme le pod) : deux utilisateurs
// et l'admin ; API GitHub factice ; aucun vrai projet. Contrat : contracts/user-backends.md §2 et §9.
// Complète les tests du dev (spaces255_dev_test.go, sans App commune) : ici l'App de l'admin est présente.

func sp255Gh(t *testing.T, ids map[string]int64) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := strings.TrimPrefix(r.URL.Path, "/")
		if id, ok := ids[l]; ok {
			fmt.Fprintf(w, `{"id":%d,"login":%q}`, id, l)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	old := core.GithubUsersURL
	core.GithubUsersURL = srv.URL + "/"
	t.Cleanup(func() { core.GithubUsersURL = old })
}

func sp255Server(t *testing.T) *px172 {
	t.Helper()
	sp255Gh(t, map[string]int64{"alice": 1111, "bob": 2222})
	px172Env(t)
	t.Setenv("CL_AUTH_USERS", "alice,bob")
	return px172Start(t, false)
}

func sp255Call(t *testing.T, s *px172, user, method, args string) map[string]any {
	t.Helper()
	v, st := s.call(user, px172Ext, `{"method":"`+method+`","args":`+args+`}`)
	if st != 200 || v == nil {
		t.Fatalf("%s %s : statut %d", user, method, st)
	}
	return v
}

func sp255Result(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	r, _ := v["result"].(map[string]any)
	if r == nil {
		t.Fatalf("pas de résultat : %v", v)
	}
	return r
}

// L'admin garde le comportement v7.0.0 : son arbre de projets, ses fichiers de configuration, pas d'espace vide.
func TestSpaces255AdminComportementV700Inchange(t *testing.T) {
	s := sp255Server(t)
	r := sp255Result(t, sp255Call(t, s, "ccoupel", "init", `{}`))
	rows, _ := r["state"].(map[string]any)["rows"].([]any)
	if !strings.Contains(fmt.Sprint(rows), "alpha") {
		t.Errorf("l'admin ne voit plus ses projets : %v", rows)
	}
	if sp, ok := r["space"].(map[string]any); ok && sp["empty"] == true {
		t.Errorf("l'admin dans un espace vide : %v", sp)
	}
	files, _ := sp255Call(t, s, "ccoupel", "configFiles", `{}`)["result"].([]any)
	if len(files) < 2 {
		t.Errorf("l'admin n'a plus tous ses fichiers de configuration : %v", files)
	}
	// compte local admin@local : même espace
	loc, st := s.call("admin@local", px172Int, `{"method":"init","args":{}}`)
	if st != 200 || !strings.Contains(fmt.Sprint(loc), "alpha") {
		t.Errorf("admin@local : %d %v", st, loc)
	}
}

// userList : réservée à l'admin, sans identifiant numérique.
func TestSpaces255UserListAdminSeulement(t *testing.T) {
	s := sp255Server(t)
	sp255Call(t, s, "alice", "init", `{}`) // alice est connue de users/index.json
	sp255Call(t, s, "bob", "init", `{}`)
	out := sp255Call(t, s, "ccoupel", "userList", `{}`)
	if out["error"] != nil || !strings.Contains(fmt.Sprint(out["result"]), "alice") || !strings.Contains(fmt.Sprint(out["result"]), "bob") {
		t.Fatalf("userList admin : %v", out)
	}
	if str := fmt.Sprint(out); strings.Contains(str, "1111") || strings.Contains(str, "2222") || strings.Contains(str, "github-") {
		t.Errorf("userList expose un identifiant : %v", out)
	}
	for _, u := range []string{"alice", "bob"} {
		// chemin R1 général (revue LOT-2) : {ok:false, forbidden:admin}, jamais une liste
		if r, _ := sp255Call(t, s, u, "userList", `{}`)["result"].(map[string]any); r["forbidden"] != "admin" || r["users"] != nil {
			t.Errorf("userList accepté pour %s : %v", u, r)
		}
	}
}

// Réglages d'interface, fichiers de configuration : strictement par identité.
func TestSpaces255EtatDInterfaceNonCroise(t *testing.T) {
	s := sp255Server(t)
	sp255Call(t, s, "alice", "setPrefs", `{"fontSize":"alice-17"}`)
	for _, u := range []string{"bob", "ccoupel"} {
		if str := fmt.Sprint(sp255Call(t, s, u, "init", `{}`)); strings.Contains(str, "alice-17") {
			t.Errorf("les préférences d'alice apparaissent chez %s", u)
		}
	}
	if str := fmt.Sprint(sp255Call(t, s, "alice", "init", `{}`)); !strings.Contains(str, "alice-17") {
		t.Error("alice ne retrouve pas ses préférences")
	}
	// l'éditeur de configuration d'un utilisateur ne propose que son backends.yaml, jamais un fichier de l'admin
	files, _ := sp255Call(t, s, "alice", "configFiles", `{}`)["result"].([]any)
	if len(files) != 1 {
		t.Errorf("fichiers de configuration d'alice : %v", files)
	}
	if str := fmt.Sprint(files); strings.Contains(str, core.Ng2ConfigDir()) || strings.Contains(str, "github-") {
		t.Errorf("chemin du serveur ou identifiant exposé : %v", files)
	}
	// ce qu'alice enregistre n'est lu ni par bob ni par l'admin
	cfg := `{"kind":"backends.yaml","content":"backends:\n  marqueur-alice:\n    type: k8s\n    namespace: ns\n    kubeconfig: true\n"}`
	if o := sp255Call(t, s, "alice", "configSave", cfg); o["error"] != nil {
		t.Fatalf("configSave alice : %v", o)
	}
	if str := fmt.Sprint(sp255Call(t, s, "bob", "configOpen", `{"kind":"backends.yaml"}`)); strings.Contains(str, "marqueur-alice") {
		t.Error("bob lit le backends.yaml d'alice")
	}
	if b, _ := os.ReadFile(core.BackendsPath()); strings.Contains(string(b), "marqueur-alice") {
		t.Error("le backends.yaml de l'admin a reçu la déclaration d'alice")
	}
	if _, err := os.Stat(filepath.Join(core.UsersDir(), "github-1111", "backends.yaml")); err != nil {
		t.Errorf("backends.yaml d'alice absent de son espace : %v", err)
	}
}

// /files : ni l'un ni l'autre utilisateur ne lit la racine de l'admin.
func TestSpaces255FilesRefuseAuxUtilisateurs(t *testing.T) {
	s := sp255Server(t)
	hdr := func(u string) map[string]string {
		return map[string]string{"X-Forwarded-User": u, "X-Forwarded-For": "192.168.1.20"}
	}
	for _, u := range []string{"alice", "bob"} {
		for _, p := range []string{"/files/alpha/", "/files/alpha/.git/config", "/files/", "/files/../"} {
			if r, _ := s.do("GET", p, px172Ext, hdr(u), ""); r == nil || r.StatusCode != http.StatusNotFound {
				code := 0
				if r != nil {
					code = r.StatusCode
				}
				t.Errorf("%s GET %s : %d, 404 attendu", u, p, code)
			}
		}
	}
}

// Flux d'évènements : ceux de l'App de l'admin ne sortent jamais vers un utilisateur ; l'utilisateur sans backend a un état vide.
func TestSpaces255UtilisateurSansBackendEtatVide(t *testing.T) {
	s := sp255Server(t)
	r := sp255Result(t, sp255Call(t, s, "bob", "init", `{}`))
	if sp, _ := r["space"].(map[string]any); sp == nil || sp["empty"] != true || sp["admin"] != false {
		t.Errorf("espace de bob : %v", r["space"])
	}
	if rows, _ := r["state"].(map[string]any)["rows"].([]any); len(rows) != 0 {
		t.Errorf("bob voit des projets : %v", rows)
	}
	// ni terminal ni action de projet : refusés
	for _, m := range []string{"open", "termInput", "create", "refresh", "backendTest", "quit"} {
		if o := sp255Call(t, s, "bob", m, `{}`); o["error"] == nil {
			t.Errorf("%s accepté pour un utilisateur sans backend", m)
		}
	}
}
