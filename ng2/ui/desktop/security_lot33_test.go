package desktop

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// ── M1 : configApply d'un projet hors de l'arbre ──────────────────────────

func TestConfigApplyProjectEnvRefusesPathsOutsideTree(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	outside := filepath.Join(t.TempDir(), "ailleurs")
	_ = os.MkdirAll(outside, 0o755)
	for _, path := range []string{outside, "../../etc", "/etc", "", filepath.Join(outside, "..", "..")} {
		b, _ := json.Marshal(map[string]any{"kind": "project-env", "path": path, "mtime": "", "changes": []any{
			map[string]any{"op": "set", "at": []string{"image"}, "value": "docker.io/library/debian:12"}}})
		if _, err := a.Dispatch("configApply", b); err == nil || !strings.Contains(err.Error(), "projet inconnu") {
			t.Errorf("configApply project-env %q : %v", path, err)
		}
	}
	for _, p := range []string{filepath.Join(outside, ".claude-launcher"), "/etc/.claude-launcher"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("écriture hors de l'arbre : %s", p)
		}
	}
}

// ── m1 : l'éditeur de texte ne reçoit jamais le jeton ─────────────────────

func TestTextEditorNeverGetsTheToken(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) (map[string]any, string) {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := a.Dispatch(m, b)
		if err != nil {
			t.Fatalf("%s : %v", m, err)
		}
		out, _ := json.Marshal(res)
		var v map[string]any
		_ = json.Unmarshal(out, &v)
		return v, string(out)
	}
	const tok = "ghp_SECRETTOKEN0123456789"
	conf := "# ma config\nGITHUB_TOKEN=\"" + tok + "\"\nTEAM_HIDE_IDLE=45\n"
	if err := os.WriteFile(core.ConfigPath(), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	// ouverture : marqueur, jamais le jeton
	d, raw := call("configOpen", map[string]any{"kind": "config.conf"})
	if strings.Contains(raw, tok) || !strings.Contains(d["content"].(string), `GITHUB_TOKEN="__GARDER__"`) {
		t.Fatalf("configOpen : %s", raw)
	}
	// validation : jamais le jeton dans les messages (fichier invalide ailleurs)
	_, raw = call("configValidate", map[string]any{"kind": "config.conf", "content": "GITHUB_TOKEN=\"" + tok + "\"\nA=(\n"})
	if strings.Contains(raw, tok) {
		t.Fatalf("configValidate : %s", raw)
	}
	// enregistrer sans toucher au jeton : conservé (et le reste modifié)
	content := strings.Replace(d["content"].(string), "TEAM_HIDE_IDLE=45", "TEAM_HIDE_IDLE=60", 1)
	s, raw := call("configSave", map[string]any{"kind": "config.conf", "content": content, "mtime": d["mtime"]})
	if s["saved"] != true || strings.Contains(raw, tok) {
		t.Fatalf("configSave : %s", raw)
	}
	got, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(got), tok) || strings.Contains(string(got), "__GARDER__") || !strings.Contains(string(got), "TEAM_HIDE_IDLE=60") {
		t.Fatalf("jeton non conservé :\n%s", got)
	}
	// remplacer par un nouveau jeton
	d, _ = call("configOpen", map[string]any{"kind": "config.conf"})
	const newTok = "ghp_NOUVEAUJETON9876543210"
	s, raw = call("configSave", map[string]any{"kind": "config.conf", "mtime": d["mtime"],
		"content": strings.Replace(d["content"].(string), "__GARDER__", newTok, 1)})
	if s["saved"] != true || strings.Contains(raw, newTok) {
		t.Fatalf("remplacement : %s", raw)
	}
	if got, _ = os.ReadFile(core.ConfigPath()); !strings.Contains(string(got), newTok) || strings.Contains(string(got), tok) {
		t.Fatalf("jeton non remplacé :\n%s", got)
	}
	// vider
	d, _ = call("configOpen", map[string]any{"kind": "config.conf"})
	s, _ = call("configSave", map[string]any{"kind": "config.conf", "mtime": d["mtime"],
		"content": strings.Replace(d["content"].(string), `"__GARDER__"`, `""`, 1)})
	if got, _ = os.ReadFile(core.ConfigPath()); s["saved"] != true || strings.Contains(string(got), "ghp_") || strings.Contains(string(got), "__GARDER__") {
		t.Fatalf("jeton non vidé : %v\n%s", s, got)
	}
	// marqueur sans jeton enregistré : jamais écrit comme jeton
	d, _ = call("configOpen", map[string]any{"kind": "config.conf"})
	call("configSave", map[string]any{"kind": "config.conf", "mtime": d["mtime"], "content": strings.Replace(d["content"].(string), `GITHUB_TOKEN=""`, `GITHUB_TOKEN="__GARDER__"`, 1)})
	if got, _ = os.ReadFile(core.ConfigPath()); strings.Contains(string(got), "__GARDER__") {
		t.Fatalf("marqueur enregistré :\n%s", got)
	}
}

// ── m6 : backendTest borné ────────────────────────────────────────────────

func TestBackendTestConcurrencyLimited(t *testing.T) {
	sandbox(t, "", "alpha")
	t.Setenv("PATH", t.TempDir()) // aucun outil : réponse immédiate sans réseau
	var r recorder
	a := newApp(t, r.emit)
	// deux places prises : le troisième appel est refusé (après son attente) au lieu de lancer un 3e ssh
	backendTestSem <- struct{}{}
	backendTestSem <- struct{}{}
	defer func() { <-backendTestSem; <-backendTestSem }()
	done := make(chan map[string]any, 1)
	go func() {
		done <- a.backendTest(core.BackendEntry{Name: "x", Type: "docker", Host: "tcp://10.9.9.9:2375"})
	}()
	select {
	case res := <-done:
		st := res["steps"].([]core.TestStep)
		if len(st) != 1 || st[0].OK || st[0].Label != "test refusé" {
			t.Fatalf("étapes : %+v", st)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("l'appel attend sans fin")
	}
}

// ── m5 : serveur web ──────────────────────────────────────────────────────

func TestWebHostCheckHeadersAndTokenOnlyInCookieForCalls(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok123", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	do := func(method, path, host, cookie, body string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if host != "" {
			req.Host = host
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "cl_token", Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	port := srv.Listener.Addr().String()[strings.LastIndex(srv.Listener.Addr().String(), ":")+1:]
	// page d'amorçage : jeton de l'URL accepté, cookie posé, en-têtes de sécurité
	resp, _ := do("GET", "/?t=tok123", "", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("amorçage : %d", resp.StatusCode)
	}
	for k, v := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
		if resp.Header.Get(k) != v {
			t.Errorf("en-tête %s = %q", k, resp.Header.Get(k))
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP : %q", resp.Header.Get("Content-Security-Policy"))
	}
	hasCookie := false
	for _, c := range resp.Cookies() {
		hasCookie = hasCookie || c.Name == "cl_token" && c.Value == "tok123"
	}
	if !hasCookie {
		t.Error("cookie non posé")
	}
	// appels : cookie seulement, jamais le jeton de l'URL
	call := `{"method":"configFiles","args":{}}`
	if resp, _ := do("POST", "/api/call?t=tok123", "", "", call); resp.StatusCode != 403 {
		t.Errorf("/api/call avec jeton en query : %d", resp.StatusCode)
	}
	if resp, _ := do("GET", "/api/events?t=tok123", "", "", ""); resp.StatusCode != 403 {
		t.Errorf("/api/events avec jeton en query : %d", resp.StatusCode)
	}
	if resp, body := do("POST", "/api/call", "", "tok123", call); resp.StatusCode != 200 || !strings.Contains(body, "result") {
		t.Errorf("/api/call avec cookie : %d %s", resp.StatusCode, body)
	}
	if resp, _ := do("POST", "/api/call", "", "mauvais", call); resp.StatusCode != 403 {
		t.Errorf("mauvais cookie : %d", resp.StatusCode)
	}
	// en-tête Host : seule la boucle locale (DNS rebinding)
	for _, host := range []string{"evil.example.com", "evil.example.com:" + port, "127.0.0.1.evil.com:" + port, "10.0.0.5:" + port, "localhost.evil.com", ""} {
		if host == "" {
			continue
		}
		if resp, _ := do("POST", "/api/call", host, "tok123", call); resp.StatusCode != 403 {
			t.Errorf("Host %q accepté : %d", host, resp.StatusCode)
		}
		if resp, _ := do("GET", "/?t=tok123", host, "", ""); resp.StatusCode != 403 {
			t.Errorf("Host %q accepté sur /: %d", host, resp.StatusCode)
		}
	}
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "LOCALHOST:" + port, "[::1]:" + port} {
		if resp, _ := do("POST", "/api/call", host, "tok123", call); resp.StatusCode != 200 {
			t.Errorf("Host %q refusé : %d", host, resp.StatusCode)
		}
	}
	// le port d'écoute connu : un autre port est refusé
	w.port = port
	if resp, _ := do("POST", "/api/call", "127.0.0.1:1", "tok123", call); resp.StatusCode != 403 {
		t.Errorf("mauvais port accepté : %d", resp.StatusCode)
	}
	// adresse explicite hors boucle locale : tous les noms admis
	w.strictHost = false
	if resp, _ := do("POST", "/api/call", "mon-serveur.lan:"+port, "tok123", call); resp.StatusCode != 200 {
		t.Errorf("hôte refusé hors mode strict : %d", resp.StatusCode)
	}
}
