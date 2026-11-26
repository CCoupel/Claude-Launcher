package desktop

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// Fumée du sous-chemin public (addendum 4, contracts/auth.md §9) : base retirée à l'entrée, remise dans les Location, <base href> injecté.
func TestBaseSmoke(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	ext, err := core.ParsePublicURL("https://coupel.net/claude-qualif")
	if err != nil || ext.Base != "/claude-qualif" || ext.Origin != "https://coupel.net" {
		t.Fatalf("ParsePublicURL : %+v %v", ext, err)
	}
	in, _ := core.ParsePublicURL("https://cl.int")
	auth, _ := core.ParseAuthConf("ccoupel", "", "cl.int")
	w := &webBridge{app: a, subs: map[chan []byte]bool{}, strictHost: true, access: core.AccessProxy, public: &ext, alts: []core.PublicURL{in}, auth: auth, audit: core.NewAudit(t.TempDir())}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	do := func(method, path, host, user string) (int, string, http.Header) {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		req.Host = host
		if user != "" {
			req.Header.Set("X-Forwarded-User", user)
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
	const eh = "coupel.net"
	if c, _, h := do("GET", "/claude-qualif", eh, "ccoupel"); c != 308 || h.Get("Location") != "/claude-qualif/" {
		t.Errorf("base seule : %d %v", c, h.Get("Location"))
	}
	if c, _, _ := do("GET", "/emby", eh, "ccoupel"); c != 404 {
		t.Errorf("hors base : %d", c)
	}
	if c, b, _ := do("GET", "/claude-qualif/", eh, "ccoupel"); c != 200 || !strings.Contains(b, `<base href="/claude-qualif/">`) {
		t.Errorf("index externe : %d, base absente", c)
	}
	if c, b, _ := do("GET", "/", "cl.int", "ccoupel"); c != 200 || !strings.Contains(b, `<base href="/">`) {
		t.Errorf("index interne : %d, base « / » absente", c)
	}
	if c, _, h := do("GET", "/claude-qualif/?t=x", eh, "ccoupel"); c != 303 || h.Get("Location") != "/claude-qualif/" {
		t.Errorf("/?t= : %d %v", c, h.Get("Location"))
	}
	if c, b, _ := do("GET", "/claude-qualif/signed-out", eh, ""); c != 200 || !strings.Contains(b, "/claude-qualif") && strings.Contains(b, `href="/"`) {
		t.Errorf("signed-out sans identité : %d", c)
	}
	if c, _, _ := do("GET", "/claude-qualif/api/events", eh, ""); c != 401 {
		t.Errorf("events sans identité : %d", c)
	}
}
