package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Écriture structurée par les appels de l'interface (#118, LOT-3.1) : modèle,
// changements appliqués avec commentaires conservés (critical), validation
// unique, conflit, avertissements hubs relus, test d'un backend sans outil,
// états. Aucun accès réseau ni vrai moteur.
func TestConfigStructuredCalls(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := a.Dispatch(m, b)
		if err != nil {
			t.Fatalf("%s : %v", m, err)
		}
		out, _ := json.Marshal(res)
		var v map[string]any
		_ = json.Unmarshal(out, &v)
		return v
	}
	set := func(at []string, v any) map[string]any {
		return map[string]any{"op": "set", "at": at, "value": v}
	}
	// fichier absent : modèle vide, pas de problème
	m := call("configModel", map[string]any{"kind": "backends.yaml"})
	if m["exists"] != false || len(m["problems"].([]any)) != 0 || m["model"] == nil {
		t.Fatalf("modèle d'un fichier absent : %v", m)
	}
	if _, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"config.conf"}`)); err == nil {
		t.Fatal("modèle de config.conf (LOT-3.3) accepté")
	}
	// création par changements, puis relecture du modèle
	s := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": "", "changes": []any{
		set([]string{"backends", "srv1"}, map[string]any{"type": "docker", "host": "tcp://10.0.0.1:2375", "shared": []any{"/share/MFS/ROOT"}}),
		set([]string{"pools", "labo"}, map[string]any{"hosts": []any{"srv1", "srv2"}}),
	}})
	if s["saved"] != true || s["note"] == "" || s["mtime"] == "" {
		t.Fatalf("création : %v", s)
	}
	m = call("configModel", map[string]any{"kind": "backends.yaml"})
	bes := m["model"].(map[string]any)["backends"].([]any)
	if len(bes) == 0 || bes[len(bes)-1].(map[string]any)["name"] != "srv1" || m["mtime"] != s["mtime"] {
		t.Fatalf("modèle relu : %v", m)
	}
	// critical : un commentaire ajouté à la main survit à un changement
	path := core.BackendsPath()
	b, _ := os.ReadFile(path)
	withComment := "# ne pas toucher à l'IP de srv1\n" + string(b)
	if err := os.WriteFile(path, []byte(withComment), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	mt := st.ModTime().Format(time.RFC3339Nano)
	s2 := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": mt, "changes": []any{
		set([]string{"defaults", "backend"}, "srv1"),
	}})
	if s2["saved"] != true {
		t.Fatalf("changement : %v", s2)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# ne pas toucher à l'IP de srv1") || !strings.Contains(string(got), "backend: srv1") {
		t.Fatalf("commentaire perdu :\n%s", got)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != withComment {
		t.Fatal("copie .bak absente")
	}
	// invalide : refusé par la validation existante, fichier intact
	before, _ := os.ReadFile(path)
	bad := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		set([]string{"backends", "z"}, map[string]any{"type": "vapeur"}),
	}})
	if bad["invalid"] != true || len(bad["problems"].([]any)) == 0 {
		t.Fatalf("invalide accepté : %v", bad)
	}
	badOp := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		map[string]any{"op": "boum", "at": []string{"x"}},
	}})
	if badOp["invalid"] != true {
		t.Fatalf("opération inconnue : %v", badOp)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("fichier modifié malgré l'erreur")
	}
	// conflit : fichier modifié depuis l'ouverture
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(path, future, future)
	c := call("configApply", map[string]any{"kind": "backends.yaml", "mtime": s2["mtime"], "changes": []any{
		set([]string{"defaults", "image"}, "x:1"),
	}})
	if c["conflict"] != true || c["mtime"] == "" {
		t.Fatalf("conflit : %v", c)
	}
	// avertissements de hubs: recalculés à chaque enregistrement de backends.yaml
	if _, err := a.configSave(core.ConfBackends, "", "backends: {}\nhubs:\n  PC1:\n    pathz: []\n", c["mtime"].(string)); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	n := len(a.hubWarns)
	a.mu.Unlock()
	if n == 0 {
		t.Fatal("faute de frappe dans hubs: non signalée après l'enregistrement")
	}
	st, _ = os.Stat(path)
	if _, err := a.configSave(core.ConfBackends, "", "backends: {}\n", st.ModTime().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	n = len(a.hubWarns)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("avertissement conservé après correction : %v", a.hubWarns)
	}
}

func TestBackendTestAndStatesCalls(t *testing.T) {
	sandbox(t, "", "alpha")
	t.Setenv("PATH", t.TempDir()) // aucun outil : aucun accès réseau possible
	var r recorder
	a := newApp(t, r.emit)
	res, err := a.Dispatch("backendTest", json.RawMessage(`{"backend":{"name":"x","type":"docker","host":"tcp://10.9.9.9:2375","shared":["/share/MFS/ROOT"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	steps := res.(map[string]any)["steps"].([]core.TestStep)
	if len(steps) != 1 || steps[0].OK || steps[0].Label != "outil requis : docker" || steps[0].Fix == "" {
		t.Fatalf("étapes : %+v", steps)
	}
	// forme invalide : une étape d'échec, pas une erreur d'appel
	res, err = a.Dispatch("backendTest", json.RawMessage(`{"backend":{"name":"y","type":"vapeur"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := res.(map[string]any)["steps"].([]core.TestStep); len(st) != 1 || st[0].OK {
		t.Fatalf("type invalide : %+v", st)
	}
	if _, err := a.Dispatch("backendTest", json.RawMessage(`{"backend":"nom"}`)); err == nil {
		t.Fatal("arguments invalides acceptés")
	}
	// états : l'hôte est joignable sans rien calculer ; la charge est un objet
	out, err := a.Dispatch("backendStates", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var v struct {
		States map[string]core.BackendState `json:"states"`
		Load   map[string]map[string]int    `json:"load"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Load == nil {
		t.Fatalf("états : %s %v", b, err)
	}
	if h, ok := v.States["host"]; ok && !h.OK {
		t.Fatalf("host : %+v", h)
	}
}
