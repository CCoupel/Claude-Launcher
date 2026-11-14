package desktop

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Rubriques Launcher, Conteneurs et Ce poste (#149) par les appels de
// l'interface : modèle de config.conf (jeton jamais renvoyé), écriture d'une clé
// en 0600 relue à chaud, valeurs refusées, conflit, env.yaml du launcher, hub ;
// jamais un chemin fourni par le client ; sans attendre a.mu.
func TestLauncherSettingsCalls(t *testing.T) {
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
	raw := func(m string, args map[string]any) string {
		b, _ := json.Marshal(args)
		res, _ := a.Dispatch(m, b)
		out, _ := json.Marshal(res)
		return string(out)
	}
	// config.conf absent : valeurs par défaut ; « path » du client ignoré
	m := call("configModel", map[string]any{"kind": "config.conf", "path": "/etc"})
	mm := m["model"].(map[string]any)
	if m["exists"] != false || m["path"] != core.ConfigPath() || len(mm["settings"].([]any)) < 15 || len(mm["groups"].([]any)) == 0 {
		t.Fatalf("modèle : %v", m)
	}
	// création à l'enregistrement (absent), 0600, relue à chaud
	set := func(k string, v any) map[string]any {
		return map[string]any{"op": "set", "at": []string{k}, "value": v}
	}
	s := call("configApply", map[string]any{"kind": "config.conf", "mtime": "", "changes": []any{set("TEAM_HIDE_IDLE", 45), set("GITHUB_TOKEN", "ghp_TOPSECRET99")}})
	if s["saved"] != true || s["note"] == "" || s["mtime"] == "" {
		t.Fatalf("création : %v", s)
	}
	if st, err := os.Stat(core.ConfigPath()); err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 { // Windows : pas de droits Unix
		t.Fatalf("droits : %v %v", st, err)
	}
	if a.conf().TeamHideIdle != 45 {
		t.Errorf("config.conf non relue à chaud : %d", a.conf().TeamHideIdle)
	}
	b, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(b), "TEAM_HIDE_IDLE=45") || !strings.Contains(string(b), "# claude-launcher — configuration utilisateur") ||
		!strings.Contains(string(b), `GITHUB_TOKEN="ghp_TOPSECRET99"`) {
		t.Fatalf("fichier créé :\n%s", b)
	}
	// le jeton n'est dans AUCUNE réponse
	for _, resp := range []string{raw("configModel", map[string]any{"kind": "config.conf"}), mustJSON(t, s),
		raw("configApply", map[string]any{"kind": "config.conf", "mtime": s["mtime"], "changes": []any{set("TEAM_HIDE_IDLE", 46)}})} {
		if strings.Contains(resp, "TOPSECRET") {
			t.Fatalf("jeton dans une réponse : %s", resp)
		}
	}
	m = call("configModel", map[string]any{"kind": "config.conf"})
	var tok map[string]any
	for _, x := range m["model"].(map[string]any)["settings"].([]any) {
		if x.(map[string]any)["key"] == "GITHUB_TOKEN" {
			tok = x.(map[string]any)
		}
	}
	if tok["set"] != true || tok["value"] != nil {
		t.Fatalf("jeton : %v", tok)
	}
	// valeurs refusées : rien d'écrit
	before, _ := os.ReadFile(core.ConfigPath())
	for _, bad := range []any{set("CLAUDE_OPTIONS", "$(rm -rf ~)"), set("TEAM_HIDE_IDLE", -3), set("PATH", "/x")} {
		r := call("configApply", map[string]any{"kind": "config.conf", "mtime": m["mtime"], "changes": []any{bad}})
		if r["invalid"] != true || len(r["problems"].([]any)) == 0 {
			t.Errorf("accepté : %v → %v", bad, r)
		}
	}
	if after, _ := os.ReadFile(core.ConfigPath()); string(after) != string(before) {
		t.Fatal("fichier modifié malgré les refus")
	}
	// conflit
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(core.ConfigPath(), future, future)
	if c := call("configApply", map[string]any{"kind": "config.conf", "mtime": m["mtime"], "changes": []any{set("TEAM_HIDE_IDLE", 50)}}); c["conflict"] != true || c["mtime"] == "" {
		t.Fatalf("conflit : %v", c)
	}
	// env.yaml du launcher
	e := call("configModel", map[string]any{"kind": "env.yaml"})
	if e["exists"] != false || e["model"].(map[string]any)["packages"] == nil {
		t.Fatalf("env.yaml du launcher : %v", e)
	}
	es := call("configApply", map[string]any{"kind": "env.yaml", "mtime": "", "changes": []any{set("packages", []any{"ripgrep"})}})
	if es["saved"] != true {
		t.Fatalf("env.yaml : %v", es)
	}
	if bad := call("configApply", map[string]any{"kind": "env.yaml", "mtime": es["mtime"], "changes": []any{set("image", "x")}}); bad["invalid"] != true {
		t.Fatalf("clé réservée aux projets acceptée : %v", bad)
	}
	// Ce poste : section de la machine dans backends.yaml, taille des caractères
	host := core.HubHost()
	_ = os.WriteFile(core.BackendsPath(), []byte("# postes\nhubs:\n  "+strings.ToUpper(host)+":\n    hub_cache: /x   # cache\n"), 0o644)
	_, _ = a.Dispatch("setPrefs", json.RawMessage(`{"termFontSize":15}`))
	h := call("configModel", map[string]any{"kind": "hub"})
	hm := h["model"].(map[string]any)
	if hm["present"] != true || hm["goos"] == "" || hm["termFontSize"].(float64) != 15 || hm["settings"].(map[string]any)["hub_cache"] != "/x" {
		t.Fatalf("hub : %v", h)
	}
	hs := call("configApply", map[string]any{"kind": "hub", "mtime": h["mtime"], "changes": []any{
		map[string]any{"op": "set", "at": []string{"hub_cache"}, "value": "/y"},
		map[string]any{"op": "set", "at": []string{"paths"}, "value": []any{map[string]any{"local": "/a", "remote": "/share/a"}}}}})
	if hs["saved"] != true {
		t.Fatalf("hub : %v", hs)
	}
	got, _ := os.ReadFile(core.BackendsPath())
	if !strings.Contains(string(got), "# postes") || !strings.Contains(string(got), "hub_cache: /y") || !strings.Contains(string(got), "# cache") || !strings.Contains(string(got), "/share/a") {
		t.Fatalf("backends.yaml :\n%s", got)
	}
	if bad := call("configApply", map[string]any{"kind": "hub", "mtime": hs["mtime"], "changes": []any{set("backends", 1)}}); bad["invalid"] != true {
		t.Fatalf("réglage inconnu accepté : %v", bad)
	}
	// sans attendre a.mu
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		a.mu.Lock()
		close(held)
		<-release
		a.mu.Unlock()
	}()
	<-held
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	for name, args := range map[string]map[string]any{
		"configModel": {"kind": "config.conf"}, "configApply": {"kind": "env.yaml", "mtime": "", "changes": []any{}},
	} {
		done := make(chan struct{})
		go func() { call(name, args); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s attend a.mu", name)
		}
	}
	close(release)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMaskProblems(t *testing.T) {
	ps := maskProblems([]core.ConfigProblem{{Message: "ligne 3 : ghp_X1234567 inattendu"}, {Message: "rien"}}, "ghp_X1234567")
	if strings.Contains(ps[0].Message, "ghp_X1234567") || !strings.Contains(ps[0].Message, "***") || ps[1].Message != "rien" {
		t.Fatalf("%+v", ps)
	}
	if got := maskProblems([]core.ConfigProblem{{Message: "a"}}, ""); got[0].Message != "a" {
		t.Fatal("sans jeton")
	}
}
