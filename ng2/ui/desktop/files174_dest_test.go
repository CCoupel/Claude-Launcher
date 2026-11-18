package desktop

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"claude-launcher-ng2/core"
)

// /files : une page ou un document d'un autre site est refusé, mais les sous-ressources d'une maquette sandboxée
// (origine opaque : « cross-site ») passent — feuille de style, script, image, police, média — (#174, audit sécurité).
func TestFilesSousRessourcesDeMaquette(t *testing.T) {
	for _, mode := range []string{core.AccessNone, core.AccessToken} {
		ps := sandbox(t, "", "alpha")
		var r recorder
		a := newApp(t, r.emit)
		w := &webBridge{app: a, token: "tok174tok174tok174", subs: map[chan []byte]bool{}, strictHost: true, access: mode}
		srv := httptest.NewServer(w.handler())
		_ = os.MkdirAll(filepath.Join(ps[0], "_work", "mockup"), 0o755)
		_ = os.WriteFile(filepath.Join(ps[0], "_work", "mockup", "m.css"), []byte("a{}"), 0o644)
		_ = os.WriteFile(filepath.Join(ps[0], ".env"), []byte("SECRET=1"), 0o644)
		u := core.FilesPrefix + ps[0]
		var cookie *http.Cookie
		if mode == core.AccessToken {
			cookie = scBootstrap(t, srv.URL, "tok174tok174tok174")
		}
		get := func(p string, hdr map[string]string) int {
			req, _ := http.NewRequest("GET", srv.URL+p, nil)
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			if cookie != nil {
				req.AddCookie(cookie)
			}
			resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			return resp.StatusCode
		}
		css := u + "/_work/mockup/m.css"
		for _, dest := range []string{"style", "script", "image", "font", "audio", "video", "track"} {
			if c := get(css, map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": dest}); c != 200 {
				t.Errorf("%s : sous-ressource %s cross-site : %d, attendu 200", mode, dest, c)
			}
		}
		for _, dest := range []string{"document", "iframe", "frame", "embed", "object", "empty", ""} {
			if c := get(css, map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": dest}); c != http.StatusForbidden {
				t.Errorf("%s : destination %q cross-site : %d, attendu 403", mode, dest, c)
			}
		}
		// un élément sensible reste refusé, quelle que soit la destination
		if c := get(u+"/.env", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": "style"}); c != http.StatusNotFound && c != http.StatusForbidden {
			t.Errorf("%s : .env : %d, attendu 404", mode, c)
		}
		if c := get(u+"/.env", nil); c != http.StatusNotFound {
			t.Errorf("%s : .env même origine : %d, attendu 404", mode, c)
		}
		srv.Close()
	}
}
