package desktop

// Tests de spécification #252 (v7.0.0, addendum 2) — règle R1 recentrée « serveur et options globales » (contracts/auth.md §4) :
// un test énumère les `case` du dispatch (échec fermé : une méthode non classée est R1), matrice méthode × rôle × mode,
// R1 par fichier de configuration (backends.yaml : seulement si un champ R1 change), refus du mode proxy, champ `access`.
// Les appels « autorisés » n'utilisent que des méthodes sans effet de bord avec des arguments vides ; sandbox : HOME factice.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// dispatchMethods : toutes les méthodes citées par un `case` d'un `switch method` du dispatch, et les clés de secretMethods.
func dispatchMethods(t *testing.T) map[string]string {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("%s : %v", f, err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SwitchStmt:
				if id, ok := x.Tag.(*ast.Ident); ok && id.Name == "method" {
					for _, c := range x.Body.List {
						for _, e := range c.(*ast.CaseClause).List {
							if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.STRING {
								s, _ := strconv.Unquote(bl.Value)
								out[s] = f + ":" + fset.Position(bl.Pos()).String()
							}
						}
					}
				}
			case *ast.ValueSpec:
				if len(x.Names) == 1 && x.Names[0].Name == "secretMethods" && len(x.Values) == 1 {
					if cl, ok := x.Values[0].(*ast.CompositeLit); ok {
						for _, e := range cl.Elts {
							if kv, ok := e.(*ast.KeyValueExpr); ok {
								if bl, ok := kv.Key.(*ast.BasicLit); ok {
									s, _ := strconv.Unquote(bl.Value)
									out[s] = f + " (secretMethods)"
								}
							}
						}
					}
				}
			}
			return true
		})
	}
	if len(out) < 40 {
		t.Fatalf("seulement %d méthodes trouvées dans le dispatch : analyse du source cassée ?", len(out))
	}
	return out
}

// Chaque méthode du dispatch est classée (contracts/auth.md §4, « échec fermé ») : sinon le test échoue et nomme la méthode
// à classer (R1 par défaut tant qu'elle ne l'est pas).
func TestR1172EveryDispatchMethodIsClassified(t *testing.T) {
	var missing []string
	for m, where := range dispatchMethods(t) {
		if _, known := core.ClassOf(m); !known {
			missing = append(missing, m+" ("+where+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("méthodes du dispatch non classées (traitées comme R1) — les classer dans core.methodClass et contracts/auth.md §4 :\n  %s",
			strings.Join(missing, "\n  "))
	}
	// à l'inverse : une méthode classée que le dispatch ne connaît plus est un reste à retirer
	disp := dispatchMethods(t)
	var stale []string
	for _, m := range core.ClassifiedMethods() {
		if _, ok := disp[m]; !ok {
			stale = append(stale, m)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("méthodes classées absentes du dispatch (restes ou noms faux) : %v", stale)
	}
}

func r1App(t *testing.T) *App {
	t.Helper()
	sandbox(t, "", "alpha")
	var r recorder
	return newApp(t, r.emit)
}

func r1Call(a *App, access string, id *core.Identity, method, args string) (map[string]any, error) {
	res, err := a.DispatchWebAs(webCall{Src: "10.42.0.7", Remote: access == core.AccessProxy, Sid: "s", Access: access, User: id}, method, json.RawMessage(args))
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, err
}

func forbidden(m map[string]any) bool {
	return m != nil && m["forbidden"] == "admin" && m["ok"] == false && strings.Contains(fmt.Sprint(m["msg"]), "règle R1") && m["error"] == nil
}

var (
	r1Admin = &core.Identity{Login: "ccoupel", Role: core.RoleAdmin}
	r1User  = &core.Identity{Login: "alice", Role: core.RoleUser}
	r1Local = &core.Identity{Login: core.LocalAdminLogin, Role: core.RoleAdmin}
)

// R1 : refusée à un user, à une identité absente et au mode « none » ; jamais au refus d'un autre type (pas de `error`).
func TestR1172ServerMethodsRefusedToUserAndNone(t *testing.T) {
	a := r1App(t)
	r1 := []string{"backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "relaunch", "login"}
	cfg := []string{`{"kind":"config.conf","content":"x"}`, `{"kind":"env.yaml","content":"x"}`, `{"kind":"hub","content":"x"}`}
	type client struct {
		name   string
		access string
		id     *core.Identity
	}
	for _, c := range []client{{"proxy user", core.AccessProxy, r1User}, {"proxy sans identité", core.AccessProxy, nil}, {"none", core.AccessNone, nil}} {
		for _, m := range r1 {
			if res, err := r1Call(a, c.access, c.id, m, `{}`); err != nil || !forbidden(res) {
				t.Errorf("%s / %s : %v %v, attendu {ok:false, forbidden:admin, msg « … règle R1 »}", c.name, m, res, err)
			}
		}
		for _, m := range []string{"configSave", "configApply"} {
			for _, args := range cfg {
				if res, err := r1Call(a, c.access, c.id, m, args); err != nil || !forbidden(res) {
					t.Errorf("%s / %s %s : %v %v, attendu refusé par R1", c.name, m, args, res, err)
				}
			}
		}
	}
	// backends.yaml : refusé quand un champ R1 change
	for name, content := range map[string]string{
		"volumes":            "backends:\n  k: {type: k8s, volumes: {/a: pvc}}\n",
		"namespace":          "backends:\n  k: {type: k8s, namespace: kube-system}\n",
		"context":            "backends:\n  k: {type: k8s, context: admin}\n",
		"type host":          "backends:\n  h: {type: host}\n",
		"serviceAccountName": "backends:\n  k: {type: k8s, pod: {serviceAccountName: admin}}\n",
		"rubrique globale":   "defaults:\n  browser_server: { listen: off }\nbackends:\n  s: {type: ssh, host: h}\n",
		"YAML illisible":     "backends: [\n",
	} {
		args, _ := json.Marshal(map[string]string{"kind": "backends.yaml", "content": content})
		for _, m := range []string{"configSave"} {
			if res, err := r1Call(a, core.AccessProxy, r1User, m, string(args)); err != nil || !forbidden(res) {
				t.Errorf("user / backends.yaml (%s) : %v %v, attendu refusé par R1", name, res, err)
			}
		}
	}
}

// Un user est LIBRE sur ses projets et backends : backends.yaml hors champs R1, project-env, projets, lectures, terminaux.
func TestR1172UserFreeOnProjectsAndBackends(t *testing.T) {
	a := r1App(t)
	free := map[string]string{
		"configSave project-env":  "configSave|" + `{"kind":"project-env","path":"/inexistant/projet","content":"x"}`,
		"configApply project-env": "configApply|" + `{"kind":"project-env","path":"/inexistant/projet","changes":[]}`,
		"backends.yaml hors R1":   "configSave|" + `{"kind":"backends.yaml","content":"backends:\n  s: {type: ssh, host: h, shared: [/a]}\n"}`,
		"backends.yaml image":     "configSave|" + `{"kind":"backends.yaml","content":"backends:\n  k: {type: k8s, image: img:2}\n"}`,
		"projectSettings":         "projectSettings|{}",
		"projectApply":            "projectApply|{}",
		"execApply":               "execApply|{}",
		"envTrustAnswer":          "envTrustAnswer|{}",
		"initProposalAnswer":      "initProposalAnswer|{}",
		"backendHostScan":         "backendHostScan|{}",
		"backendTest":             "backendTest|{}",
		"backendSecretGet":        "backendSecretGet|{}",
		"refresh":                 "refresh|{}",
		"select":                  "select|{}",
		"setPrefs":                "setPrefs|{}",
		"init":                    "init|{}",
		"clientInfo":              "clientInfo|{}",
		"configFiles":             "configFiles|{}",
		"termResize":              "termResize|{}",
		"termFocus":               "termFocus|{}",
		"cardsFor":                "cardsFor|{}",
		"backendStates":           "backendStates|{}",
		"launcherInfo":            "launcherInfo|{}",
	}
	for name, c := range free {
		method, args, _ := strings.Cut(c, "|")
		for _, cl := range []struct {
			n      string
			access string
			id     *core.Identity
		}{{"proxy user", core.AccessProxy, r1User}, {"proxy admin", core.AccessProxy, r1Admin}, {"proxy admin@local", core.AccessProxy, r1Local}, {"none", core.AccessNone, nil}, {"token", core.AccessToken, nil}} {
			if res, _ := r1Call(a, cl.access, cl.id, method, args); forbidden(res) {
				t.Errorf("%s / %s : refusé par R1 alors que la méthode est libre (user libre sur projets et backends)", cl.n, name)
			}
		}
	}
}

// R1 permise à un administrateur (proxy, admin@local) et au propriétaire (mode jeton) : jamais « forbidden ».
func TestR1172ServerMethodsAllowedToAdminAndToken(t *testing.T) {
	a := r1App(t)
	for _, cl := range []struct {
		n      string
		access string
		id     *core.Identity
	}{{"proxy admin", core.AccessProxy, r1Admin}, {"proxy admin@local", core.AccessProxy, r1Local}, {"token", core.AccessToken, nil}} {
		for _, c := range []string{"backendSecretDelete|{}", "backendKnownHostsSet|{}", "backendSecretSet|{}",
			"configSave|" + `{"kind":"config.conf","content":"x"}`, "configApply|" + `{"kind":"hub","changes":[]}`,
			"configSave|" + `{"kind":"backends.yaml","content":"backends:\n  k: {type: k8s, volumes: {/a: pvc}}\n"}`} {
			method, args, _ := strings.Cut(c, "|")
			if res, _ := r1Call(a, cl.access, cl.id, method, args); forbidden(res) {
				t.Errorf("%s / %s %.40s : refusé par R1 à tort", cl.n, method, args)
			}
		}
	}
}

// Méthode inconnue = R1 (échec fermé) : refusée à un user ; à un admin, une erreur de méthode (pas un refus R1).
func TestR1172UnknownMethodFailsClosed(t *testing.T) {
	a := r1App(t)
	for _, m := range []string{"methodeInconnue", "configsave", "CONFIGSAVE", "relaunch2", ""} {
		if res, err := r1Call(a, core.AccessProxy, r1User, m, `{}`); err != nil || !forbidden(res) {
			t.Errorf("user / %q : %v %v, attendu refusé par R1 (échec fermé)", m, res, err)
		}
		if res, _ := r1Call(a, core.AccessNone, nil, m, `{}`); !forbidden(res) {
			t.Errorf("none / %q : %v, attendu refusé par R1", m, res)
		}
		if res, _ := r1Call(a, core.AccessProxy, r1Admin, m, `{}`); forbidden(res) {
			t.Errorf("admin / %q : refusé par R1 (une méthode inconnue échoue autrement pour un admin)", m)
		}
	}
}

// Refusé en mode proxy, tous rôles (admin compris) : quit, webUIOpen, webUIRegen, clipboard*, teamAgentSet, secretReveal.
func TestR1172ProxyDeniedForEveryRole(t *testing.T) {
	a := r1App(t)
	for _, m := range []string{"quit", "webUIOpen", "webUIRegen", "clipboardRead", "clipboardWrite", "teamAgentSet", "secretReveal"} {
		for _, id := range []*core.Identity{r1Admin, r1Local, r1User} {
			res, err := r1Call(a, core.AccessProxy, id, m, `{}`)
			if err == nil && res["ok"] == true {
				t.Errorf("proxy %s / %s : accepté (%v)", id.Login, m, res)
			}
		}
	}
}

// access (init et clientInfo) : présent dans tous les modes du serveur web, absent de la fenêtre native.
func TestR1172AccessObject(t *testing.T) {
	a := r1App(t)
	for _, m := range []string{"init", "clientInfo"} {
		for name, c := range map[string]struct {
			access string
			id     *core.Identity
			role   string
			denyR1 bool
		}{
			"proxy admin": {core.AccessProxy, r1Admin, "admin", false}, "proxy user": {core.AccessProxy, r1User, "user", true},
			"none": {core.AccessNone, nil, "", true}, "token": {core.AccessToken, nil, "", false},
		} {
			res, err := r1Call(a, c.access, c.id, m, `{}`)
			if err != nil {
				t.Fatalf("%s / %s : %v", name, m, err)
			}
			acc, _ := d183Find(res, "access").(map[string]any)
			if acc == nil || acc["mode"] != c.access {
				t.Errorf("%s / %s : access = %v, attendu mode %s", name, m, acc, c.access)
				continue
			}
			if r, _ := acc["role"].(string); r != c.role {
				t.Errorf("%s / %s : role %q, attendu %q", name, m, r, c.role)
			}
			deny, _ := acc["deny"].([]any)
			has := func(s string) bool {
				for _, d := range deny {
					if d == s {
						return true
					}
				}
				return false
			}
			if has("relaunch") != c.denyR1 || has("backendSecretSet") != c.denyR1 || has("configSave:config.conf") != c.denyR1 {
				t.Errorf("%s / %s : deny = %v, R1 refusées attendues : %v", name, m, deny, c.denyR1)
			}
			if has("configSave:project-env") || has("projectSettings") || has("create") || has("backendTest") {
				t.Errorf("%s / %s : deny contient une méthode libre : %v", name, m, deny)
			}
			if acc["backendsAdminFields"] != c.denyR1 {
				t.Errorf("%s / %s : backendsAdminFields = %v, attendu %v", name, m, acc["backendsAdminFields"], c.denyR1)
			}
			if c.access == core.AccessProxy && !has("quit") {
				t.Errorf("%s / %s : quit absent de deny (refusé en proxy)", name, m)
			}
		}
	}
	// fenêtre native : aucun champ access
	res, err := a.DispatchFrom(originWindow, "init", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["access"] != nil {
		t.Errorf("fenêtre native : access = %v, attendu absent", m["access"])
	}
}
