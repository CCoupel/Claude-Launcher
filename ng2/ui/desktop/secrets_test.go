package desktop

import (
	"claude-launcher-ng2/core"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Appels backendSecret* : même règle pour la fenêtre et le web (provisoire v6.4.0), clé privée jamais renvoyée,
// source journalisée.
func TestBackendSecretCalls(t *testing.T) {
	d := filepath.Join(t.TempDir(), "secrets")
	t.Setenv("CL_SECRETS_DIR", d)
	t.Setenv("CL_ROOT_BASE", "")
	t.Setenv(core.CacheDirVar, filepath.Join(t.TempDir(), "cache"))
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	_ = os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  srv: { type: ssh, host: 192.168.1.210 }\n"), 0o644)
	var r recorder
	a := newApp(t, r.emit)
	call := func(from origin, src, m string, args any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(args)
		res, err := a.DispatchFromAddr(from, src, m, raw)
		if err != nil {
			t.Fatalf("%s : %v", m, err)
		}
		b, _ := json.Marshal(res)
		if strings.Contains(string(b), "PRIVATE KEY") {
			t.Fatalf("%s renvoie une clé privée : %s", m, b)
		}
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out
	}
	set := call(originWeb, "192.168.1.50", "backendSecretSet", map[string]any{"backend": "srv", "generate": true})
	if set["ok"] != true || !strings.HasPrefix(set["fingerprint"].(string), "SHA256:") || set["publicKey"] == "" {
		t.Fatalf("génération : %v", set)
	}
	g := call(originWindow, "", "backendSecretGet", map[string]any{"backend": "srv"})
	if g["hasKey"] != true || g["publicKey"] != set["publicKey"] {
		t.Errorf("get : %v", g)
	}
	if bad := call(originWeb, "x", "backendSecretSet", map[string]any{"backend": "srv", "privateKey": "x", "publicKey": "y"}); bad["ok"] != false {
		t.Errorf("clé invalide acceptée : %v", bad)
	}
	j, _ := os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl"))
	if !strings.Contains(string(j), "192.168.1.50") {
		t.Errorf("source absente du journal : %s", j)
	}
	if del := call(originWindow, "", "backendSecretDelete", map[string]any{"backend": "srv", "what": "all"}); del["ok"] != true {
		t.Errorf("suppression : %v", del)
	}
	j, _ = os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl"))
	if !strings.Contains(string(j), "fenêtre native") {
		t.Errorf("source fenêtre absente : %s", j)
	}
}

// Le chemin du coffre n'est montré qu'à la fenêtre native ; le relevé d'empreinte exige un backend ssh déclaré.
func TestBackendSecretDirEtScanDeclare(t *testing.T) {
	d := filepath.Join(t.TempDir(), "secrets")
	t.Setenv("CL_SECRETS_DIR", d)
	t.Setenv("CL_ROOT_BASE", "")
	t.Setenv(core.CacheDirVar, filepath.Join(t.TempDir(), "cache"))
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	_ = os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("backends:\n  srv: { type: ssh, host: 192.168.1.210 }\n"), 0o644)
	var r recorder
	a := newApp(t, r.emit)
	get := func(from origin) map[string]any {
		raw, _ := json.Marshal(map[string]any{"backend": "srv"})
		res, err := a.DispatchFromAddr(from, "10.0.0.1", "backendSecretGet", raw)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	if get(originWindow)["dir"] == "" || get(originWindow)["dir"] == nil {
		t.Error("la fenêtre native doit recevoir le dossier du coffre")
	}
	if v := get(originWeb)["dir"]; v != nil && v != "" {
		t.Errorf("le client web reçoit le dossier du coffre : %v", v)
	}
	raw, _ := json.Marshal(map[string]any{"backend": "inconnu", "host": "192.168.1.5"})
	res, _ := a.DispatchFromAddr(originWeb, "x", "backendHostScan", raw)
	if m, _ := res.(map[string]any); m["ok"] != false {
		t.Errorf("relevé pour un backend non déclaré : %v", res)
	}
}
