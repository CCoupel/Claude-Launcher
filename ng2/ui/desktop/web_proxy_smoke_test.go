package desktop

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// Fumée du mode proxy (v7.0.0) : identité, rôles, R1, audit, /signed-out. La matrice complète est celle du test-writer.
func TestProxySmoke(t *testing.T) {
	sandbox(t, "", "alpha")
	dir := t.TempDir()
	var r recorder
	a := newApp(t, r.emit)
	pub, _ := core.ParsePublicURL("https://cl.example.org")
	alt, _ := core.ParsePublicURL("https://cl.int")
	auth, err := core.ParseAuthConf("Alice", "bob", "cl.int")
	if err != nil {
		t.Fatal(err)
	}
	w := &webBridge{app: a, subs: map[chan []byte]bool{}, strictHost: true, access: core.AccessProxy, public: &pub, alts: []core.PublicURL{alt}, auth: auth, audit: core.NewAudit(dir)}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	do := func(method, path, user, host string, body string) (int, string, http.Header) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Host = host
		if user != "" {
			req.Header.Set("X-Forwarded-User", user)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return resp.StatusCode, b.String(), resp.Header
	}
	call := func(user, host, method string) map[string]any {
		code, body, _ := do("POST", "/api/call", user, host, `{"method":"`+method+`","args":{}}`)
		if code != 200 {
			t.Fatalf("%s %s : %d %s", user, method, code, body)
		}
		var out struct {
			Result map[string]any `json:"result"`
		}
		_ = json.Unmarshal([]byte(body), &out)
		return out.Result
	}
	const pubHost = "cl.example.org"
	if c, _, h := do("GET", "/", "", pubHost, ""); c != 401 || h.Get("X-Launcher-Refusal") != "identity" {
		t.Errorf("sans identité : %d %v", c, h)
	}
	if c, _, h := do("GET", "/", "mallory", pubHost, ""); c != 403 || h.Get("X-Launcher-Refusal") != "user" {
		t.Errorf("login inconnu : %d %v", c, h)
	}
	if c, _, _ := do("GET", "/", "ALICE", pubHost, ""); c != 200 {
		t.Errorf("admin (casse) : %d", c)
	}
	if c, _, _ := do("GET", "/signed-out", "", pubHost, ""); c != 200 {
		t.Errorf("/signed-out sans identité : %d", c)
	}
	if c, _, _ := do("GET", "/?t=x", "alice", pubHost, ""); c != 303 {
		t.Errorf("/?t= : %d", c)
	}
	if c, _, h := do("GET", "/", "admin@local", pubHost, ""); c != 403 || h.Get("X-Launcher-Refusal") != "user" {
		t.Errorf("admin@local sur l'hôte externe : %d", c)
	}
	if c, _, _ := do("GET", "/", "admin@local", "cl.int", ""); c != 200 {
		t.Errorf("admin@local sur l'hôte interne : %d", c)
	}
	if res := call("bob", pubHost, "configSave"); res["forbidden"] != "admin" || res["ok"] != false {
		t.Errorf("R1 user : %v", res)
	}
	if res := call("alice", pubHost, "clientInfo"); res["remote"] != true || res["user"].(map[string]any)["role"] != "admin" {
		t.Errorf("clientInfo : %v", res)
	}
	if res := call("alice", pubHost, "backendSecretGet"); res["forbidden"] != nil {
		t.Errorf("backendSecretGet doit rester permis : %v", res)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(b), `"user":"bob"`) || !strings.Contains(string(b), `"err":"forbidden"`) {
		t.Errorf("audit : %s", b)
	}
}
