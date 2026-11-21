//go:build !windows

package desktop

// #216 (critical) : Dispatch projectMoveInfo / projectMove — compléments boîte noire à projectmove216_test.go : projet
// ouvert refusé sans rien écrire, appels forgés en --web, approbation d'environnement qui SUIT le projet (aucune
// nouvelle question), arbre relu. Fixture de create215 (faux claude, tmux isolé, moteurs de conteneurs en échec immédiat).

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

func world216d(t *testing.T, base string) map[string]string {
	t.Helper()
	w := map[string]string{}
	_ = filepath.Walk(base, func(p string, st os.FileInfo, err error) error {
		if err != nil || st == nil {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		if st.IsDir() {
			w[rel] = "dir"
		} else {
			b, _ := os.ReadFile(p)
			w[rel] = string(b)
		}
		return nil
	})
	return w
}

func sameWorld216(t *testing.T, what string, a, b map[string]string) {
	t.Helper()
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			t.Errorf("%s : %s modifié ou supprimé", what, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			t.Errorf("%s : %s créé", what, k)
		}
	}
}

func TestMoveProjetOuvertRefuse216(t *testing.T) {
	f := containerFix215(t, "")
	src := filepath.Join(f.base, "TEST", "alpha")
	_ = core.WriteAssignment(src, "host")
	if v := dispatchJSON(t, f.a, "open", map[string]any{"path": src}); v["tab"] != src {
		t.Fatalf("open = %v", v)
	}
	f.r.wait(t, 20*time.Second, "ready", isTerm(src, "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	before := world216d(t, f.base)
	info := dispatchJSON(t, f.a, "projectMoveInfo", map[string]any{"path": src})
	if info["open"] != true {
		t.Errorf("open = %v, attendu true", info["open"])
	}
	bl, _ := info["blockers"].([]any)
	found := false
	for _, b := range bl {
		if strings.Contains(b.(string), "ouvert dans ce launcher") {
			found = true
		}
	}
	if !found {
		t.Errorf("blockers = %v, attendu « ouvert dans ce launcher »", bl)
	}
	v := dispatchJSON(t, f.a, "projectMove", map[string]any{"path": src, "base": f.base, "category": "AUTRE", "newCategory": true, "name": "beta"})
	if v["ok"] == true || strings.TrimSpace(fmtMsg216(v["msg"])) == "" {
		t.Errorf("déplacement d'un projet ouvert = %v, attendu un refus avec message", v)
	}
	sameWorld216(t, "projet ouvert", before, world216d(t, f.base))
	if f.a.term(src) == nil {
		t.Error("l'onglet du projet a été fermé par le refus")
	}
	// avec destination : projectMoveInfo dit aussi pourquoi la validation est impossible (même liste)
	d := dispatchJSON(t, f.a, "projectMoveInfo", map[string]any{"path": src, "base": f.base, "category": "AUTRE", "newCategory": true, "name": "beta"})
	if bl2, _ := d["blockers"].([]any); len(bl2) == 0 {
		t.Errorf("projectMoveInfo avec destination sans blockers : %v", d)
	}
}

func fmtMsg216(v any) string { s, _ := v.(string); return s }

func TestMoveAppelsForgesWeb216(t *testing.T) {
	f := containerFix215(t, "")
	src := filepath.Join(f.base, "TEST", "alpha")
	dehors := t.TempDir()
	_ = os.Symlink(dehors, filepath.Join(f.base, "SORTIE"))
	before := world216d(t, f.base)
	w := &webBridge{app: f.a, token: "tok216", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	call := func(args map[string]any) (int, map[string]any) {
		b, _ := json.Marshal(map[string]any{"method": "projectMove", "args": args})
		resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok216", string(b), "application/json", "")
		var v map[string]any
		_ = json.Unmarshal([]byte(body), &v)
		if res, ok := v["result"].(map[string]any); ok {
			v = res
		}
		return resp.StatusCode, v
	}
	for name, args := range map[string]map[string]any{
		"base hors des bases":   {"path": src, "base": "/tmp", "name": "x"},
		"catégorie ..":          {"path": src, "base": f.base, "category": "../..", "name": "x"},
		"catégorie absolue":     {"path": src, "base": f.base, "category": "/etc", "name": "x"},
		"nom ..":                {"path": src, "base": f.base, "category": "AUTRE", "name": "../x"},
		"lien sortant":          {"path": src, "base": f.base, "category": "SORTIE", "newCategory": true, "name": "x"},
		"destination = projet":  {"path": src, "base": f.base, "category": "TEST", "name": "alpha"},
		"projet hors des bases": {"path": "/etc", "base": f.base, "name": "x"},
	} {
		code, v := call(args)
		if code != 200 && code != 400 {
			t.Errorf("%s : HTTP %d", name, code)
			continue
		}
		if v["ok"] == true {
			t.Errorf("%s : accepté : %v", name, v)
		}
	}
	// sans session : refusé comme tout appel
	b, _ := json.Marshal(map[string]any{"method": "projectMove", "args": map[string]any{"path": src, "base": f.base, "category": "AUTRE", "newCategory": true, "name": "beta"}})
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", string(b), "application/json", ""); resp.StatusCode == 200 {
		t.Error("projectMove accepté sans cookie")
	}
	sameWorld216(t, "appels forgés", before, world216d(t, f.base))
	if es, _ := os.ReadDir(dehors); len(es) != 0 {
		t.Errorf("écriture hors des bases : %v", es)
	}
	// un déplacement légitime passe par /api/call avec session
	code, v := call(map[string]any{"path": src, "base": f.base, "category": "AUTRE", "newCategory": true, "name": "beta"})
	if code != 200 || v["ok"] != true || v["path"] != filepath.Join(f.base, "AUTRE", "beta") {
		t.Errorf("déplacement légitime en --web : %d %v", code, v)
	}
}

// L'approbation d'environnement d'un projet cloné SUIT le projet : même état, aucune question à l'ouverture suivante,
// aucune entrée orpheline ; la ligne « approbation » figure dans « ce qui change » du dialogue.
func TestMoveApprobationSuitLeProjet216(t *testing.T) {
	f := containerFix215(t, "")
	// un moteur « vide » : aucun conteneur à supprimer à l'étape ctr (le faux moteur de la fixture, en échec immédiat,
	// ferait annuler le déplacement : « conteneurs illisibles » — comportement voulu, testé en core)
	empty := t.TempDir()
	for _, n := range []string{"podman", "docker"} {
		_ = os.WriteFile(filepath.Join(empty, n), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	t.Setenv("PATH", empty+string(os.PathListSeparator)+os.Getenv("PATH"))
	src := filepath.Join(f.base, "TEST", "alpha")
	writeEnv215(t, src, evil215)
	if ok, err := core.MarkEnvPending(src, "https://github.com/a/b.git", "debian:12"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	info0, _ := core.EnvTrustInfoFor(src)
	if _, err := core.AnswerEnvTrust(src, "accept", info0.SHA); err != nil {
		t.Fatal(err)
	}
	_ = core.WriteAssignment(src, "pod")
	f.a.rescan()
	entBefore, _ := core.EnvTrustEntry(src)

	d := dispatchJSON(t, f.a, "projectMoveInfo", map[string]any{"path": src, "base": f.base, "category": "PRO", "newCategory": true, "name": "depot-renomme"})
	dest, _ := d["dest"].(map[string]any)
	if dest == nil || dest["ok"] != true {
		t.Fatalf("dest = %v", d["dest"])
	}
	hasTrust := false
	for _, im := range dest["impacts"].([]any) {
		if im.(map[string]any)["what"] == "trust" {
			hasTrust = true
		}
	}
	if !hasTrust {
		t.Errorf("impacts sans ligne d'approbation : %v", dest["impacts"])
	}
	v := dispatchJSON(t, f.a, "projectMove", map[string]any{"path": src, "base": f.base, "category": "PRO", "newCategory": true, "name": "depot-renomme"})
	dst := filepath.Join(f.base, "PRO", "depot-renomme")
	if v["ok"] != true || v["path"] != dst {
		t.Fatalf("projectMove = %v", v)
	}
	entAfter, ok := core.EnvTrustEntry(dst)
	if !ok || entAfter != entBefore {
		t.Errorf("entrée après déplacement = %+v (présente=%v), attendu %+v (état et empreinte conservés)", entAfter, ok, entBefore)
	}
	if _, orphan := core.EnvTrustEntry(src); orphan {
		t.Error("entrée orpheline de l'ancien chemin")
	}
	if core.EnvTrust(dst) != core.TrustAccepted || core.EnvTrustChanged(dst) {
		t.Errorf("EnvTrust(destination) = %s changed=%v, attendu accepted sans changement", core.EnvTrust(dst), core.EnvTrustChanged(dst))
	}
	// ouverture en conteneur : aucune question d'approbation
	o := dispatchJSON(t, f.a, "open", map[string]any{"path": dst})
	if o["needTrust"] != nil {
		t.Errorf("question d'approbation après déplacement : %v", o)
	}
	for _, e := range f.r.all() {
		if e["_name"] == "envTrust" {
			t.Errorf("évènement envTrust après déplacement : %v", e)
		}
	}
	// arbre relu, sélection suivie, ancien chemin absent
	paths := f.a.projectPaths()
	foundNew, foundOld := false, false
	for _, p := range paths {
		foundNew = foundNew || p == dst
		foundOld = foundOld || p == src
	}
	if !foundNew || foundOld {
		t.Errorf("arbre : nouveau=%v ancien=%v", foundNew, foundOld)
	}
	if got := f.a.Snapshot().Selected; got != dst {
		t.Errorf("sélection = %q, attendu %q", got, dst)
	}
	if j, _ := os.ReadFile(filepath.Join(os.Getenv("CL_CONFIG_DIR"), "moves.json")); strings.Contains(string(j), src) {
		t.Errorf("journal non vidé : %s", j)
	}
	// un projet « ignored » suit aussi, avec son socle (projet jamais ouvert)
	beta := filepath.Join(f.base, "TEST", "beta")
	_ = os.MkdirAll(beta, 0o755)
	if out, err := exec.Command("git", "-C", beta, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	writeEnv215(t, beta, evil215)
	_, _ = core.MarkEnvPending(beta, "https://github.com/a/c.git", "debian:12")
	info2, _ := core.EnvTrustInfoFor(beta)
	if _, err := core.AnswerEnvTrust(beta, "ignore", info2.SHA); err != nil {
		t.Fatal(err)
	}
	_ = core.WriteAssignment(beta, "pod")
	f.a.rescan()
	beta2 := filepath.Join(f.base, "PRO", "beta-2")
	if v := dispatchJSON(t, f.a, "projectMove", map[string]any{"path": beta, "base": f.base, "category": "PRO", "name": "beta-2"}); v["ok"] != true {
		t.Fatalf("second déplacement = %v", v)
	}
	if e, ok := core.EnvTrustEntry(beta2); !ok || e.State != core.TrustIgnored || e.Image != "debian:12" {
		t.Errorf("projet ignored déplacé : %+v (présente=%v)", e, ok)
	}
	if _, orphan := core.EnvTrustEntry(beta); orphan {
		t.Error("entrée orpheline pour le projet ignored")
	}
}
