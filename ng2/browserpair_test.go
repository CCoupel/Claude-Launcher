package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isolateCache(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
}

// Jeton tiré une fois et gardé ; appariement accepté seulement avec ce jeton,
// deviceId enregistré puis repris par browserDevice ; hubs.<machine> l'emporte.
func TestBrowserPairing(t *testing.T) {
	isolateCache(t)
	tok := browserToken()
	if len(tok) != 32 || browserToken() != tok {
		t.Fatalf("jeton instable : %q", tok)
	}
	if st, _ := os.Stat(browserTokenPath()); st.Mode().Perm() != 0o600 {
		t.Fatalf("jeton en %v", st.Mode().Perm())
	}
	srv := httptest.NewServer(fileServer{hosts: map[string]bool{"127.0.0.1": true}, roots: func() []string { return nil }, token: browserToken})
	defer srv.Close()
	pair := func(dev, tk string) (int, string) {
		resp, err := http.Get(srv.URL + browserPairPath + "?device=" + url.QueryEscape(dev) + "&token=" + url.QueryEscape(tk))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := pair("bb02d4f4-47aa-4728-976e-4841b526f6ba", "mauvais"); code != 403 || !strings.Contains(body, "autre launcher") {
		t.Fatalf("mauvais jeton : %d %q", code, body)
	}
	if id, _ := browserDevice(backendsFile{}, "msi"); id != "" {
		t.Fatalf("enregistré malgré le mauvais jeton : %q", id)
	}
	if code, _ := pair("<script>", tok); code != 400 {
		t.Fatalf("deviceId invalide : %d", code)
	}
	if code, body := pair("bb02d4f4-47aa-4728-976e-4841b526f6ba", tok); code != 200 || !strings.Contains(body, "✓ navigateur du hub") {
		t.Fatalf("appariement : %d %q", code, body)
	}
	if id, src := browserDevice(backendsFile{}, "msi"); id != "bb02d4f4-47aa-4728-976e-4841b526f6ba" || src != "appariement" {
		t.Fatalf("relu : %q %q", id, src)
	}
	f := parseBackendsYAML(t, "hubs:\n  MSI:\n    browser_device: impose-1234\n")
	if id, src := browserDevice(f, "msi.maison.lan"); id != "impose-1234" || src != "hubs.MSI.browser_device" {
		t.Fatalf("imposé : %q %q", id, src)
	}
}

func TestBrowserHint(t *testing.T) {
	c, _ := resolveBrowserServer(backendsFile{}, "msi")
	h := browserHint(c, "", "", "tok123", "msi", nil)
	for _, w := range []string{"http://localhost:8765<chemin absolu", "jamais en file://", "pas encore identifié", "/__claude-launcher__/pair?device=<son deviceId>&token=tok123", "AskUserQuestion"} {
		if !strings.Contains(h, w) {
			t.Errorf("sans deviceId, manque %q :\n%s", w, h)
		}
	}
	h = browserHint(c, "bb02d4f4", "appariement", "tok123", "msi", nil)
	if !strings.Contains(h, "select_browser avec ce deviceId") || !strings.Contains(h, "deviceId bb02d4f4") || strings.Contains(h, "pas encore identifié") {
		t.Errorf("avec deviceId :\n%s", h)
	}
	off := serverConf{Off: true}
	h = browserHint(off, "", "", "", "msi", []BrowserPath{{"/a", "/a", "linux"}})
	if !strings.Contains(h, `CL_BROWSER_PATH_MAP=[{"container":"/a"`) || strings.Contains(h, "pair?") {
		t.Errorf("serveur off :\n%s", h)
	}
	if strings.Contains(browserHint(c, "x", "s", "t", "m", nil), "\n") {
		t.Error("une seule ligne attendue (environnement tmux)")
	}
}

// Hook ajouté une fois, hooks de l'utilisateur et autres réglages gardés,
// ancienne version de la commande remplacée.
func TestSyncBrowserHook(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	orig := `{"model":"opus","hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo moi"}]},` +
		`{"hooks":[{"type":"command","command":"echo vieux ` + browserHookMark + `"}]}],"Stop":[{"hooks":[{"type":"command","command":"x"}]}]}}`
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := syncBrowserHook(d); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	var doc struct {
		Model string
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	ss := doc.Hooks["SessionStart"]
	if doc.Model != "opus" || len(doc.Hooks["Stop"]) != 1 || len(ss) != 2 || ss[0].Hooks[0].Command != "echo moi" || ss[1].Hooks[0].Command != browserHookCommand {
		t.Fatalf("settings.json :\n%s", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("droits changés : %v", st.Mode().Perm())
	}
	// fichier absent : créé avec le seul hook
	d2 := t.TempDir()
	if err := syncBrowserHook(d2); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(d2, "settings.json")); !strings.Contains(string(b), browserHookMark) {
		t.Fatalf("créé : %s", b)
	}
}

// La commande du hook : affiche la consigne seulement si elle est posée.
func TestBrowserHookCommand(t *testing.T) {
	run := func(env ...string) string {
		t.Helper()
		c := exec.Command("sh", "-c", browserHookCommand)
		c.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if got := run(); got != "" {
		t.Fatalf("hors launcher : %q", got)
	}
	if got := run(browserHintVar + `=consigne "avec" & <signes>`); got != "consigne \"avec\" & <signes>\n" {
		t.Fatalf("consigne : %q", got)
	}
}
