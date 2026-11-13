package desktop

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Éditeur de configuration par les appels de l'interface : ouverture (modèle
// pour un fichier absent), validation, enregistrement (.bak), conflit,
// relecture à chaud de config.conf.
func TestConfigEditorCalls(t *testing.T) {
	ps := sandbox(t, "", "alpha")
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
	files, _ := a.Dispatch("configFiles", json.RawMessage(`{"path":`+strconv.Quote(ps[0])+`}`))
	if fs := files.([]core.ConfigFile); len(fs) != 4 || fs[3].Kind != core.ConfProjectEnv {
		t.Fatalf("fichiers : %+v", files)
	}
	d := call("configOpen", map[string]any{"kind": "config.conf"})
	if d["exists"] != false || d["fromTemp"] != true || !strings.Contains(d["content"].(string), "GITHUB_DIR") {
		t.Fatalf("config.conf absente : %v", d)
	}
	v := call("configValidate", map[string]any{"kind": "backends.yaml", "content": "backends:\n  x: { type: lxc }\n"})
	if p := v["problems"].([]any); len(p) != 1 || p[0].(map[string]any)["line"].(float64) != 2 {
		t.Errorf("validation : %v", v)
	}
	// enregistrement d'un fichier absent, puis relecture à chaud
	s := call("configSave", map[string]any{"kind": "config.conf", "content": "USAGE_REFRESH_SEC=300\n", "mtime": ""})
	if s["saved"] != true || s["note"] == "" {
		t.Fatalf("enregistrement : %v", s)
	}
	if a.conf().UsageRefreshSec != 300 {
		t.Errorf("config.conf non relue : %d", a.conf().UsageRefreshSec)
	}
	// erreur : refusé ; conflit : signalé avec la date actuelle
	if s := call("configSave", map[string]any{"kind": "config.conf", "content": "A=(\n", "mtime": s["mtime"]}); s["invalid"] != true {
		t.Errorf("invalide accepté : %v", s)
	}
	future := time.Now().Add(3 * time.Second)
	_ = os.Chtimes(core.ConfigPath(), future, future)
	c := call("configSave", map[string]any{"kind": "config.conf", "content": "USAGE_REFRESH_SEC=200\n", "mtime": s["mtime"]})
	if c["conflict"] != true || c["mtime"] == "" {
		t.Fatalf("conflit : %v", c)
	}
	if s := call("configSave", map[string]any{"kind": "config.conf", "content": "USAGE_REFRESH_SEC=200\n", "mtime": c["mtime"]}); s["saved"] != true {
		t.Errorf("écraser : %v", s)
	}
	if b, _ := os.ReadFile(core.ConfigPath() + ".bak"); string(b) != "USAGE_REFRESH_SEC=300\n" {
		t.Errorf(".bak : %q", b)
	}
	// env.yaml du projet : fusion affichée avec env-template.yaml
	_ = os.MkdirAll(ps[0]+"/.claude-launcher", 0o755)
	_ = os.WriteFile(ps[0]+"/.claude-launcher/env-template.yaml", []byte("image: debian:12\n"), 0o644)
	d = call("configOpen", map[string]any{"kind": "project-env", "path": ps[0]})
	if !strings.Contains(d["merged"].(string), "debian:12") || d["tplText"] == "" {
		t.Errorf("env du projet : %v", d)
	}
}
