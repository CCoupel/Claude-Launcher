package desktop

import (
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
	j, _ := os.ReadFile(filepath.Join(d, "journal.jsonl"))
	if !strings.Contains(string(j), "192.168.1.50") {
		t.Errorf("source absente du journal : %s", j)
	}
	if del := call(originWindow, "", "backendSecretDelete", map[string]any{"backend": "srv", "what": "all"}); del["ok"] != true {
		t.Errorf("suppression : %v", del)
	}
	j, _ = os.ReadFile(filepath.Join(d, "journal.jsonl"))
	if !strings.Contains(string(j), "fenêtre native") {
		t.Errorf("source fenêtre absente : %s", j)
	}
}
