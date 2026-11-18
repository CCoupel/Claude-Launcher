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
		// navigation de premier niveau (extension de navigateur, lien d'un autre site) : acceptée, même cross-site / same-site
		for _, site := range []string{"cross-site", "same-site", "none", "same-origin"} {
			if c := get(css, map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate"}); c != 200 {
				t.Errorf("%s : navigation %s : %d, attendu 200", mode, site, c)
			}
		}
		// cadres, objets, fetch/XHR, document hors navigation, en-têtes absents : refusés d'un autre site
		for _, h := range []map[string]string{
			{"Sec-Fetch-Dest": "iframe", "Sec-Fetch-Mode": "navigate"}, {"Sec-Fetch-Dest": "frame", "Sec-Fetch-Mode": "navigate"},
			{"Sec-Fetch-Dest": "embed", "Sec-Fetch-Mode": "navigate"}, {"Sec-Fetch-Dest": "object", "Sec-Fetch-Mode": "navigate"},
			{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "cors"}, {"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "no-cors"},
			{"Sec-Fetch-Dest": "document"}, {"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors"}, {"Sec-Fetch-Mode": "navigate"}, {},
			{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "websocket"}, {"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "nested-navigate"},
			{"Sec-Fetch-Dest": "iframe", "Sec-Fetch-Mode": "nested-navigate"}, {"Sec-Fetch-Dest": "iframe", "Sec-Fetch-Mode": "cors"},
			{"Sec-Fetch-Dest": "iframe", "Sec-Fetch-Mode": "websocket"}, {"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "navigate"},
		} {
			h["Sec-Fetch-Site"] = "cross-site"
			if c := get(css, h); c != http.StatusForbidden {
				t.Errorf("%s : cross-site %v : %d, attendu 403", mode, h, c)
			}
		}
		// un élément sensible reste refusé, quelle que soit la destination ou la navigation
		for _, h := range []map[string]string{{"Sec-Fetch-Dest": "style"}, {"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate"}} {
			h["Sec-Fetch-Site"] = "cross-site"
			if c := get(u+"/.env", h); c != http.StatusNotFound && c != http.StatusForbidden {
				t.Errorf("%s : .env %v : %d, attendu 404", mode, h, c)
			}
		}
		if c := get(u+"/.env", nil); c != http.StatusNotFound {
			t.Errorf("%s : .env même origine : %d, attendu 404", mode, c)
		}
		srv.Close()
	}
}
