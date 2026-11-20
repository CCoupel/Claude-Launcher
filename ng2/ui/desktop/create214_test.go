package desktop

// #214 (critical) : projectBases, projectCategories, categoryCreate, newInfo étendu et `create` contraint
// (base + catégorie + nom) — fenêtre native (Dispatch) et --web (/api/call), appels forgés compris.
// Bases factices : sandbox() pose CL_ROOTS = <tmp>/PROJETS (projets dans PROJETS/TEST/<nom>).

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func base214(t *testing.T) string {
	t.Helper()
	return filepath.Dir(filepath.Dir(sandbox(t, "", "alpha")[0])) // <tmp>/PROJETS
}

func snapDir214(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error { out = append(out, p); return nil })
	return out
}

func refus214(t *testing.T, v map[string]any, field string) {
	t.Helper()
	if v["ok"] != false {
		t.Fatalf("attendu un refus {ok:false, field:%q}, reçu %v", field, v)
	}
	if v["field"] != field {
		t.Errorf("champ = %v, attendu %q (%v)", v["field"], field, v["msg"])
	}
	if m, _ := v["msg"].(string); strings.TrimSpace(m) == "" {
		t.Errorf("message vide : %v", v)
	}
}

func TestProjectBases214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "projectBases", map[string]any{})
	bs, _ := v["bases"].([]any)
	if len(bs) != 1 {
		t.Fatalf("bases = %v", v)
	}
	b0 := bs[0].(map[string]any)
	if b0["path"] != base || b0["missing"] != false {
		t.Errorf("base = %v, attendu {path:%s, missing:false}", b0, base)
	}
	if v["default"] != base {
		t.Errorf("default = %v, attendu %s (seule base accessible)", v["default"], base)
	}
}

func TestProjectBasesPlusieursEtManquante214(t *testing.T) {
	base := base214(t)
	autre := filepath.Join(t.TempDir(), "AUTRE")
	_ = os.MkdirAll(autre, 0o755)
	perdue := filepath.Join(t.TempDir(), "perdue")
	t.Setenv("CL_ROOTS", strings.Join([]string{base, autre, perdue}, string(os.PathListSeparator)))
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "projectBases", map[string]any{})
	bs, _ := v["bases"].([]any)
	if len(bs) != 3 {
		t.Fatalf("bases = %v", v)
	}
	for i, want := range []struct {
		p       string
		missing bool
	}{{base, false}, {autre, false}, {perdue, true}} { // ordre de config.conf conservé
		b := bs[i].(map[string]any)
		if b["path"] != want.p || b["missing"] != want.missing {
			t.Errorf("base %d = %v, attendu %v", i, b, want)
		}
	}
	// plusieurs bases accessibles : la base du projet sélectionné (alpha, sous la première)
	if v["default"] != base {
		t.Errorf("default = %v, attendu %s (base du projet sélectionné)", v["default"], base)
	}
}

func TestProjectCategoriesEtCategoryCreate214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	// « TEST » existe (contient le projet alpha) : c'est une catégorie à 1 projet
	v := dispatchJSON(t, a, "projectCategories", map[string]any{"base": base})
	cats, _ := v["categories"].([]any)
	found := false
	for _, c := range cats {
		m := c.(map[string]any)
		if m["path"] == "TEST" {
			found = true
			if n, _ := m["projects"].(float64); n != 1 {
				t.Errorf("TEST : %v projets, attendu 1", m["projects"])
			}
		}
		if m["path"] == "TEST/alpha" {
			t.Error("un projet est listé comme catégorie")
		}
	}
	if !found {
		t.Errorf("TEST absent : %v", v)
	}
	// création : nouvelle, puis doublon = fusion
	c := dispatchJSON(t, a, "categoryCreate", map[string]any{"base": base, "category": "INFRA/K8S"})
	if c["ok"] != true || c["path"] != "INFRA/K8S" || c["existed"] != false {
		t.Errorf("categoryCreate = %v", c)
	}
	if st, err := os.Stat(filepath.Join(base, "INFRA", "K8S")); err != nil || !st.IsDir() {
		t.Errorf("dossier non créé : %v", err)
	}
	c = dispatchJSON(t, a, "categoryCreate", map[string]any{"base": base, "category": "INFRA/K8S"})
	if c["ok"] != true || c["existed"] != true {
		t.Errorf("doublon = %v, attendu ok et existed:true (fusion)", c)
	}
	// une catégorie vide est listée avec 0 projet
	v = dispatchJSON(t, a, "projectCategories", map[string]any{"base": base})
	vide := false
	for _, x := range v["categories"].([]any) {
		if m := x.(map[string]any); m["path"] == "INFRA/K8S" && m["projects"] == float64(0) {
			vide = true
		}
	}
	if !vide {
		t.Errorf("catégorie vide non listée : %v", v)
	}
}

func TestRefusDeChemin214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	avant := snapDir214(t, filepath.Dir(base))
	dehors := t.TempDir()
	_ = os.Symlink(dehors, filepath.Join(base, "SORTIE"))
	avant = snapDir214(t, filepath.Dir(base))

	for _, c := range []struct {
		method string
		args   map[string]any
		field  string
	}{
		{"projectCategories", map[string]any{"base": "/etc"}, "base"},
		{"projectCategories", map[string]any{"base": ""}, "base"},
		{"projectCategories", map[string]any{"base": filepath.Join(base, "TEST")}, "base"}, // ancien appel : une catégorie n'est pas une base
		{"categoryCreate", map[string]any{"base": "/etc", "category": "x"}, "base"},
		{"categoryCreate", map[string]any{"base": base, "category": "../evil"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": "/abs"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": `a\b`}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": "SORTIE/x"}, "category"},
		{"categoryCreate", map[string]any{"base": base, "category": ""}, "category"},
		// create forgé : base hors des bases, ancienne forme, traversées, absolu, lien sortant
		{"create", map[string]any{"base": "/tmp", "name": "x"}, "base"},
		{"create", map[string]any{"base": filepath.Join(base, "TEST"), "name": "x"}, "base"},
		{"create", map[string]any{"base": base, "category": "../..", "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "/etc", "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": `..\x`, "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "SORTIE", "newCategory": true, "name": "x"}, "category"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "../x"}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "a/b"}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": ""}, "name"},
		{"create", map[string]any{"base": base, "category": "TEST", "name": "alpha"}, "name"},        // existe déjà
		{"create", map[string]any{"base": base, "category": "INEXISTANTE", "name": "x"}, "category"}, // sans newCategory
	} {
		refus214(t, dispatchJSON(t, a, c.method, c.args), c.field)
	}
	if apres := snapDir214(t, filepath.Dir(base)); len(apres) != len(avant) {
		t.Errorf("écriture sur le disque malgré les refus :\navant=%v\napres=%v", avant, apres)
	}
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors des bases : %v", es)
	}
}

// Mode --web : un appel forgé hors base est refusé comme dans la fenêtre native, et n'écrit rien.
func TestRefusDeCheminWeb214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok214", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	avant := snapDir214(t, filepath.Dir(base))
	forges := []string{
		`{"method":"create","args":{"base":"/tmp","name":"evil"}}`,
		`{"method":"create","args":{"base":` + quote214(base) + `,"category":"../../..","name":"evil"}}`,
		`{"method":"create","args":{"base":` + quote214(base) + `,"category":"TEST","name":"../../evil"}}`,
		`{"method":"categoryCreate","args":{"base":` + quote214(base) + `,"category":"../evil"}}`,
		`{"method":"projectCategories","args":{"base":"/"}}`,
	}
	for _, call := range forges {
		resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok214", call, "application/json", "")
		if resp.StatusCode != 200 {
			t.Fatalf("%s : %d %s", call, resp.StatusCode, body)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(body), &v)
		if res, ok := v["result"].(map[string]any); ok {
			v = res
		}
		if v["ok"] != false || v["field"] == nil {
			t.Errorf("%s → %s : refus {ok:false, field} attendu", call, body)
		}
	}
	// sans session : refusé comme tout appel
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", forges[0], "application/json", ""); resp.StatusCode == 200 {
		t.Error("create accepté sans cookie")
	}
	if apres := snapDir214(t, filepath.Dir(base)); len(apres) != len(avant) {
		t.Errorf("écriture sur le disque malgré les refus : %v", apres)
	}
	// les nouvelles méthodes passent par /api/call avec session
	_, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok214", `{"method":"projectBases","args":{}}`, "application/json", "")
	if !strings.Contains(body, `"bases"`) || !strings.Contains(body, `"default"`) {
		t.Errorf("projectBases en --web : %s", body)
	}
}

func quote214(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestNewInfoBasesEtRoots214(t *testing.T) {
	base := base214(t)
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "newInfo", map[string]any{"path": ""})
	bs, _ := v["bases"].([]any)
	if len(bs) != 1 || bs[0].(map[string]any)["path"] != base {
		t.Errorf("newInfo.bases = %v", v["bases"])
	}
	if v["default"] != base {
		t.Errorf("newInfo.default = %v, attendu %s", v["default"], base)
	}
	roots, _ := v["roots"].([]any) // alias conservé (retiré en v7)
	if len(roots) != 1 || roots[0] != base {
		t.Errorf("newInfo.roots = %v, attendu [%s]", v["roots"], base)
	}
	// jamais de valeur secrète dans newInfo
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "GITHUB_TOKEN") {
		t.Error("newInfo expose un jeton")
	}
}
