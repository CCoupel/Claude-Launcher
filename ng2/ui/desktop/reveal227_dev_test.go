//go:build !windows

package desktop

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

const fakeTok227 = "ghp_FAKE_TEST_revealsecret227"

func revealFix227(t *testing.T) *App {
	sandbox(t, "", "alpha")
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.ConfigPath(), []byte("GITHUB_TOKEN="+fakeTok227+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var r recorder
	return newApp(t, r.emit)
}

func call227(t *testing.T, a *App, method string, args any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(method, b)
	if err != nil {
		t.Fatalf("%s : %v", method, err)
	}
	o, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(o, &v)
	return v
}

// Fenêtre native : liste fermée (GITHUB_TOKEN seulement), valeur rendue, journal SANS valeur ; configModel : `reveal` vrai, valeur
// absente du modèle.
func TestSecretRevealNative227(t *testing.T) {
	a := revealFix227(t)
	r := call227(t, a, "secretReveal", map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"})
	if r["ok"] != true || r["value"] != fakeTok227 {
		t.Fatalf("révélation : %v", r)
	}
	for _, k := range []string{"CLAUDE_OPTIONS", "USAGE_REFRESH_SEC", "INCONNUE"} {
		if r := call227(t, a, "secretReveal", map[string]any{"kind": "config.conf", "key": k}); r["ok"] != false {
			t.Errorf("%s : clé non secrète révélée : %v", k, r)
		}
	}
	if r := call227(t, a, "secretReveal", map[string]any{"kind": "backends.yaml", "key": "GITHUB_TOKEN"}); r["ok"] != false {
		t.Errorf("autre fichier : %v", r)
	}
	j, _ := os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl"))
	if !strings.Contains(string(j), "GITHUB_TOKEN") || strings.Contains(string(j), fakeTok227) || strings.Contains(string(j), "FAKE_TEST") {
		t.Errorf("journal : clé attendue, jamais la valeur :\n%s", j)
	}
	m := call227(t, a, "configModel", map[string]any{"kind": "config.conf"})
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), fakeTok227) {
		t.Fatal("la valeur est dans configModel")
	}
	for _, it := range m["model"].(map[string]any)["settings"].([]any) {
		s := it.(map[string]any)
		if s["key"] == "GITHUB_TOKEN" && s["reveal"] != true {
			t.Errorf("reveal = %v, attendu true (fenêtre native)", s["reveal"])
		}
	}
}

// Accès web DISTANT : refusé (valeur jamais rendue, rien de journalisé), œil grisé (reveal faux) ; accès local : admis.
func TestSecretRevealWebDistantRefuse227(t *testing.T) {
	a := revealFix227(t)
	args, _ := json.Marshal(map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"})
	res, _ := a.DispatchWeb("1.2.3.4", true, "sid", "secretReveal", args)
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), fakeTok227) || !strings.Contains(string(b), msgRevealRemote) {
		t.Errorf("distant : %s", b)
	}
	if j, _ := os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl")); strings.Contains(string(j), "révélé") {
		t.Errorf("refus journalisé comme révélation : %s", j)
	}
	cm, _ := a.DispatchWeb("1.2.3.4", true, "sid", "configModel", json.RawMessage(`{"kind":"config.conf"}`))
	raw, _ := json.Marshal(cm)
	if strings.Contains(string(raw), fakeTok227) || strings.Contains(string(raw), `"reveal":true`) {
		t.Errorf("configModel distant : valeur ou reveal vrai : %s", raw)
	}
	loc, _ := a.DispatchWeb("127.0.0.1", false, "sid", "secretReveal", args)
	lb, _ := json.Marshal(loc)
	if !strings.Contains(string(lb), fakeTok227) {
		t.Errorf("accès local refusé : %s", lb)
	}
	// via DispatchFrom(originWeb) sans information de provenance : fail-closed
	if r2, _ := a.DispatchFrom(originWeb, "secretReveal", args); strings.Contains(mustJSON227(r2), fakeTok227) {
		t.Error("origine web sans provenance : la valeur est sortie")
	}
}

func mustJSON227(v any) string { b, _ := json.Marshal(v); return string(b) }

// Plafond : 10 révélations par minute et par session ; une autre session n'est pas concernée.
func TestSecretRevealDebit227(t *testing.T) {
	a := revealFix227(t)
	args, _ := json.Marshal(map[string]any{"kind": "config.conf", "key": "GITHUB_TOKEN"})
	ok := 0
	for i := 0; i < 15; i++ {
		r, _ := a.DispatchWeb("127.0.0.1", false, "A", "secretReveal", args)
		if r.(map[string]any)["ok"] == true {
			ok++
		}
	}
	if ok != 10 {
		t.Errorf("révélations admises = %d, attendu 10", ok)
	}
	if r, _ := a.DispatchWeb("127.0.0.1", false, "B", "secretReveal", args); r.(map[string]any)["ok"] != true {
		t.Error("une autre session est bloquée par le plafond de la première")
	}
}

// remoteAccess : boucle locale directe seulement ; adresse publique, hôte autre, source autre, proxy → distant.
func TestRemoteAccess227(t *testing.T) {
	w := &webBridge{}
	mk := func(host, remote string, hdr map[string]string) bool {
		r := httptest.NewRequest("POST", "http://"+host+"/api/call", nil)
		r.Host, r.RemoteAddr = host, remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return w.remoteAccess(r)
	}
	for name, c := range map[string]struct {
		host, remote string
		hdr          map[string]string
		want         bool
	}{
		"local":       {"127.0.0.1:8765", "127.0.0.1:5555", nil, false},
		"localhost":   {"localhost:8765", "[::1]:5555", nil, false},
		"hôte public": {"launcher.example.org", "127.0.0.1:5555", nil, true},
		"source LAN":  {"127.0.0.1:8765", "192.168.1.9:5555", nil, true},
		"proxy":       {"127.0.0.1:8765", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "8.8.8.8"}, true},
		"hôte LAN":    {"192.168.1.5:8765", "192.168.1.9:5555", nil, true},
	} {
		if got := mk(c.host, c.remote, c.hdr); got != c.want {
			t.Errorf("%s : remote = %v, attendu %v", name, got, c.want)
		}
	}
	pub, _ := core.ParsePublicURL("https://launcher.example.org")
	w.public = &pub
	if !mk("launcher.example.org", "127.0.0.1:5555", nil) {
		t.Error("adresse publique : distant attendu")
	}
}
