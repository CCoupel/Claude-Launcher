package desktop

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// #171 (LOT-2.2) — interface web derrière un proxy inverse en HTTPS. Contrat : contracts/desktop-web.md
// (« url », « Effets de l'adresse publique », « Routes HTTP », « Flux d'évènements »). Écrit depuis le contrat
// (TDD). Aucun en-tête X-Forwarded-*, Forwarded ni X-Real-IP n'est lu : le schéma et l'hôte publics viennent
// seulement de `desktop_web.url`. Ports libres, projets factices, pas de vrai moteur.

// w171Start : interface web allumée sur un port libre, avec l'adresse publique pub (vide : sans). Rend l'App,
// le port d'écoute et le jeton.
func w171Start(t *testing.T, pub string) (*App, int, string) {
	t.Helper()
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	blk := fmt.Sprintf("{ enabled: true, listen: \"127.0.0.1:%d\"", p)
	if pub != "" {
		blk += fmt.Sprintf(", url: \"%s\"", pub)
	}
	l4WriteBackends(t, "  desktop_web: "+blk+" }\n")
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "on", 8*time.Second)
	return a, p, l4WebToken(t)
}

// w171Do : requête HTTP en clair vers le serveur local, avec les en-têtes choisis.
func w171Do(t *testing.T, port int, method, path, host string, hdr map[string]string, body string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(port)+path, strings.NewReader(body))
	if host != "" {
		req.Host = host
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// w171Session : page d'amorçage (jeton de l'URL) sur l'hôte host ; rend le cookie de session posé (nil si refusé).
func w171Session(t *testing.T, port int, tok, host string) (*http.Cookie, *http.Response) {
	t.Helper()
	resp, _ := w171Do(t, port, "GET", "/?t="+tok, host, nil, "")
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "cl_session_") {
			return c, resp
		}
	}
	return nil, resp
}

const w171Call = `{"method":"refresh","args":{}}`

// ── CRITICAL : matrice de l'en-tête Host ─────────────────────────────────────

func TestPublicURLHostMatrix(t *testing.T) {
	type row struct {
		host string
		hdr  map[string]string
		want int
	}
	run := func(t *testing.T, pub string, rowsOf func(p int) []row) {
		_, p, tok := w171Start(t, pub)
		for _, r := range rowsOf(p) {
			resp, _ := w171Do(t, p, "GET", "/?t="+tok, r.host, r.hdr, "")
			if resp.StatusCode != r.want {
				t.Errorf("Host %q en-têtes %v : %d, attendu %d", r.host, r.hdr, resp.StatusCode, r.want)
			}
		}
	}
	ps := strconv.Itoa
	t.Run("url avec port", func(t *testing.T) {
		run(t, "https://launcher.test:8443", func(p int) []row {
			return []row{
				{"127.0.0.1:" + ps(p), nil, 200},     // boucle locale : toujours admise
				{"localhost:" + ps(p), nil, 200},     // idem
				{"launcher.test:8443", nil, 200},     // hôte de url, port de url
				{"launcher.test:" + ps(p), nil, 403}, // hôte public sur le port d'écoute : pas le port de url
				{"launcher.test:9999", nil, 403},     // mauvais port
				{"evil.test:8443", nil, 403},         // autre hôte, port de url
				{"evil.test:" + ps(p), nil, 403},     // autre hôte, port d'écoute
				{"127.0.0.1:" + ps(p+1), nil, 403},   // boucle locale sur un autre port
			}
		})
	})
	t.Run("url sans port", func(t *testing.T) {
		run(t, "https://launcher.test", func(p int) []row {
			return []row{
				{"launcher.test", nil, 200}, // sans port quand url n'en a pas
				{"127.0.0.1:" + ps(p), nil, 200},
				{"evil.test", nil, 403},
				{"launcher.test.evil.test", nil, 403}, // pas une simple correspondance de suffixe
			}
		})
	})
	t.Run("sans url (v6.2.0)", func(t *testing.T) {
		run(t, "", func(p int) []row {
			return []row{
				{"127.0.0.1:" + ps(p), nil, 200},
				{"launcher.test:8443", nil, 403},
				{"launcher.test", nil, 403},
			}
		})
	})
	t.Run("X-Forwarded-* et Forwarded ignorés", func(t *testing.T) {
		run(t, "https://launcher.test:8443", func(p int) []row {
			return []row{
				// un hôte étranger ne devient pas admis parce qu'un en-tête le dit
				{"evil.test:8443", map[string]string{"X-Forwarded-Host": "launcher.test:8443"}, 403},
				{"evil.test:8443", map[string]string{"Forwarded": "host=launcher.test:8443;proto=https"}, 403},
				{"evil.test:" + ps(p), map[string]string{"X-Forwarded-Host": "127.0.0.1:" + ps(p), "X-Real-IP": "127.0.0.1"}, 403},
				// et un hôte admis ne devient pas refusé
				{"127.0.0.1:" + ps(p), map[string]string{"X-Forwarded-Host": "evil.test", "X-Forwarded-Proto": "http", "X-Forwarded-For": "10.1.2.3"}, 200},
			}
		})
	})
}

// ── CRITICAL : matrice de l'origine ──────────────────────────────────────────

func TestPublicURLOriginMatrix(t *testing.T) {
	_, p, tok := w171Start(t, "https://launcher.test:8443")
	ps := strconv.Itoa(p)
	ck, _ := w171Session(t, p, tok, "launcher.test:8443")
	if ck == nil {
		t.Fatal("pas de session sur l'hôte public")
	}
	for _, r := range []struct {
		name, host, origin string
		hdr                map[string]string
		want               int
	}{
		{"hôte public, origine de url exactement", "launcher.test:8443", "https://launcher.test:8443", nil, 200},
		{"hôte public, sans Origin", "launcher.test:8443", "", nil, 200},
		{"hôte public, schéma http", "launcher.test:8443", "http://launcher.test:8443", nil, 403},
		{"hôte public, autre port", "launcher.test:8443", "https://launcher.test", nil, 403},
		{"hôte public, autre hôte", "launcher.test:8443", "https://evil.test:8443", nil, 403},
		{"hôte public, origine de la boucle locale", "launcher.test:8443", "http://127.0.0.1:" + ps, nil, 403},
		{"hôte public, Origin null", "launcher.test:8443", "null", nil, 403},
		{"boucle locale, http://<Host>", "127.0.0.1:" + ps, "http://127.0.0.1:" + ps, nil, 200},
		{"boucle locale, origine publique", "127.0.0.1:" + ps, "https://launcher.test:8443", nil, 403},
		{"X-Forwarded-Proto/Host ne changent pas l'origine attendue", "launcher.test:8443", "http://launcher.test:8443",
			map[string]string{"X-Forwarded-Proto": "http", "X-Forwarded-Host": "launcher.test:8443"}, 403},
		{"X-Forwarded-Proto: http ne casse pas l'origine exacte", "launcher.test:8443", "https://launcher.test:8443",
			map[string]string{"X-Forwarded-Proto": "http"}, 200},
	} {
		t.Run(r.name, func(t *testing.T) {
			h := map[string]string{"Content-Type": "application/json"}
			if r.origin != "" {
				h["Origin"] = r.origin
			}
			for k, v := range r.hdr {
				h[k] = v
			}
			resp, _ := w171Do(t, p, "POST", "/api/call", r.host, h, w171Call, ck)
			if resp.StatusCode != r.want {
				t.Errorf("%d, attendu %d", resp.StatusCode, r.want)
			}
		})
	}
}

// ── CRITICAL : cookie de session et `Secure` ─────────────────────────────────

func TestPublicURLCookieSecure(t *testing.T) {
	for _, tc := range []struct {
		name, pub string
		secure    bool
	}{
		{"url https : Secure", "https://launcher.test:8443", true},
		{"url http : pas de Secure", "http://launcher.test:8080", false},
		{"sans url : pas de Secure (v6.2.0)", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, tok := w171Start(t, tc.pub)
			hosts := []string{"127.0.0.1:" + strconv.Itoa(p)}
			if tc.pub != "" {
				hosts = append(hosts, strings.TrimPrefix(strings.TrimPrefix(tc.pub, "https://"), "http://"))
			}
			for _, h := range hosts { // « sur toute réponse » : l'hôte local aussi
				ck, resp := w171Session(t, p, tok, h)
				if ck == nil {
					t.Fatalf("Host %s : pas de cookie (%d)", h, resp.StatusCode)
				}
				if ck.Secure != tc.secure || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
					t.Errorf("Host %s : %+v, attendu Secure=%v HttpOnly SameSite=Strict", h, ck, tc.secure)
				}
				if strings.Contains(ck.Value, tok) {
					t.Errorf("le cookie porte le jeton")
				}
			}
		})
	}
}

// Conséquence du cookie Secure : un navigateur n'envoie pas un cookie Secure en HTTP, l'accès direct au port du
// pod n'a donc pas de session — et sans cookie, l'appel est refusé. (Le client Go traite la boucle locale comme
// un contexte sûr : l'absence du cookie est simulée.)
func TestPublicURLHTTPDirectWithoutCookieIsRefused(t *testing.T) {
	_, p, tok := w171Start(t, "https://launcher.test:8443")
	ck, _ := w171Session(t, p, tok, "")
	if ck == nil || !ck.Secure {
		t.Fatalf("cookie Secure attendu : %+v", ck)
	}
	h := map[string]string{"Content-Type": "application/json"}
	if r, _ := w171Do(t, p, "POST", "/api/call", "", h, w171Call); r.StatusCode != 403 {
		t.Errorf("appel sans cookie : %d, attendu 403", r.StatusCode)
	}
	if r, _ := w171Do(t, p, "GET", "/api/events", "", nil, ""); r.StatusCode != 403 {
		t.Errorf("flux sans cookie : %d, attendu 403", r.StatusCode)
	}
}

// ── routes de sonde, flux d'évènements ───────────────────────────────────────

func TestHealthzReadyzNoSessionNoHostCheck(t *testing.T) {
	_, p, _ := w171Start(t, "https://launcher.test:8443")
	for _, path := range []string{"/healthz", "/readyz"} {
		var resp *http.Response
		var body string
		end := time.Now().Add(8 * time.Second)
		for { // readyz : 503 avant le chargement initial
			resp, body = w171Do(t, p, "GET", path, "10.1.2.3:"+strconv.Itoa(p), nil, "") // adresse du pod, Host quelconque
			if resp.StatusCode == 200 || time.Now().After(end) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if resp.StatusCode != 200 || strings.TrimSpace(body) != "ok" {
			t.Errorf("%s : %d %q, attendu 200 « ok »", path, resp.StatusCode, body)
		}
		if len(resp.Cookies()) != 0 {
			t.Errorf("%s pose un cookie : %v", path, resp.Cookies())
		}
		if strings.Contains(strings.ToLower(body), "version") || strings.Contains(body, "alpha") {
			t.Errorf("%s rend une information : %q", path, body)
		}
	}
}

func TestEventsStreamProxyHeaders(t *testing.T) {
	_, p, tok := w171Start(t, "")
	ck, _ := w171Session(t, p, tok, "")
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(p)+"/api/events", nil)
	req.AddCookie(ck)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("flux : %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, attendu no", got)
	}
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-store") || !strings.Contains(cc, "no-transform") {
		t.Errorf("Cache-Control = %q, attendu no-store et no-transform", cc)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	line := make(chan string, 1)
	go func() { s, _ := bufio.NewReader(resp.Body).ReadString('\n'); line <- s }()
	select {
	case s := <-line:
		if s == "" {
			t.Error("flux vide")
		}
	case <-time.After(5 * time.Second):
		t.Error("aucune donnée du flux dans les 5 s (l'état initial doit partir à la connexion)")
	}
}

// ── validation et sources de l'adresse publique, lien d'amorçage ─────────────

func TestPublicURLValidationAndSources(t *testing.T) {
	for _, bad := range []string{
		"https://launcher.test/chemin", "https://launcher.test/?x=1", "https://launcher.test/#f",
		"https://user:pw@launcher.test", "ftp://launcher.test", "launcher.test", "https://", "https://launcher.test:99999",
	} {
		t.Run(bad, func(t *testing.T) {
			sandbox(t, "", "alpha")
			p := l4FreePort(t)
			l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", url: \"%s\" }\n", p, bad))
			a := l4StartNative(t)
			w := l4WaitWebUI(t, a, "error", 8*time.Second)
			if msg, _ := w["msg"].(string); msg == "" {
				t.Errorf("erreur sans raison dans webUI.msg : %v", w)
			}
			if d183Reach(p) {
				t.Errorf("l'interface écoute malgré une adresse publique invalide")
			}
		})
	}
	t.Run("valeurs valides", func(t *testing.T) {
		for _, ok := range []string{"http://launcher.test", "https://launcher.test:8443", "https://Launcher.Test"} {
			_, p, _ := w171Start(t, ok)
			_ = p
		}
	})
	t.Run("CL_WEB_URL l'emporte sur desktop_web.url", func(t *testing.T) {
		t.Setenv("CL_WEB_URL", "https://env.test")
		a, p, tok := w171Start(t, "https://cfg.test")
		if w := l4WebUIOf(t, a); w["url"] != "https://env.test" {
			t.Errorf("webUI.url = %v, attendu https://env.test", w["url"])
		}
		if r, _ := w171Do(t, p, "GET", "/?t="+tok, "env.test", nil, ""); r.StatusCode != 200 {
			t.Errorf("hôte de CL_WEB_URL : %d", r.StatusCode)
		}
		if r, _ := w171Do(t, p, "GET", "/?t="+tok, "cfg.test", nil, ""); r.StatusCode != 403 {
			t.Errorf("hôte du réglage écrasé par CL_WEB_URL : %d, attendu 403", r.StatusCode)
		}
	})
	t.Run("webUI.url dans l'instantané", func(t *testing.T) {
		a, _, _ := w171Start(t, "https://launcher.test:8443")
		if w := l4WebUIOf(t, a); w["url"] != "https://launcher.test:8443" {
			t.Errorf("webUI.url = %v", w["url"])
		}
	})
}

func TestBootstrapLinkUsesPublicURL(t *testing.T) {
	out := d183FakeOpener(t)
	a, _, tok := w171Start(t, "https://launcher.test:8443")
	res, err := a.Dispatch("webUIOpen", nil)
	if m, _ := res.(map[string]any); err != nil || m["ok"] != true {
		t.Fatalf("webUIOpen : %v %v", res, err)
	}
	if got, want := d183WaitOpened(t, out), "https://launcher.test:8443/?t="+tok; got != want {
		t.Errorf("lien d'amorçage %q, attendu %q (adresse publique)", got, want)
	}
}

// ── CRITICAL : proxy inverse HTTPS monté dans le test ────────────────────────

func TestReverseProxyHTTPSEndToEnd(t *testing.T) {
	sandbox(t, "", "alpha")
	// le proxy d'abord : son port fait partie de l'adresse publique
	px := httptest.NewUnstartedServer(nil)
	pxPort := px.Listener.Addr().(*net.TCPAddr).Port
	pub := fmt.Sprintf("https://launcher.test:%d", pxPort)
	p := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", url: \"%s\" }\n", p, pub))
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "on", 8*time.Second)
	tok := l4WebToken(t)

	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(p))
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	orig := rp.Director
	rp.Director = func(r *http.Request) {
		host := r.Host
		orig(r)
		r.Host = host
		r.Header.Set("X-Forwarded-Proto", "http") // trompeurs : jamais lus
		r.Header.Set("X-Forwarded-Host", "evil.test")
	}
	px.Config.Handler = rp
	px.StartTLS()
	t.Cleanup(px.Close)

	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // certificat de test du proxy
		DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, px.Listener.Addr().String()) // launcher.test → proxy
		},
	}}
	base := pub

	// amorçage par le lien public : cookie Secure accepté par le jar en HTTPS
	resp, err := cl.Get(base + "/?t=" + tok)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("amorçage via le proxy : %v %v", resp, err)
	}
	resp.Body.Close()
	u, _ := url.Parse(base)
	var sess *http.Cookie
	for _, c := range jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "cl_session_") {
			sess = c
		}
	}
	if sess == nil {
		t.Fatal("aucune session gardée par le client derrière le proxy HTTPS")
	}
	call := func(origin string) int {
		req, _ := http.NewRequest("POST", base+"/api/call", strings.NewReader(w171Call))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		r, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if st := call(pub); st != 200 {
		t.Errorf("appel avec l'origine publique : %d", st)
	}
	if st := call(""); st != 200 {
		t.Errorf("appel sans Origin : %d", st)
	}
	if st := call("https://evil.test:" + strconv.Itoa(pxPort)); st != 403 {
		t.Errorf("appel d'une autre origine derrière le proxy : %d, attendu 403", st)
	}
	// un autre nom d'hôte passé par le proxy : refusé
	req, _ := http.NewRequest("GET", base+"/?t="+tok, nil)
	req.Host = "evil.test:" + strconv.Itoa(pxPort)
	if r, err := cl.Do(req); err != nil || r.StatusCode != 403 {
		t.Errorf("autre hôte derrière le proxy : %v %v, attendu 403", r, err)
	} else {
		r.Body.Close()
	}
	// sondes par le proxy (Host public) : sans session
	if r, err := cl.Get(base + "/healthz"); err != nil || r.StatusCode != 200 {
		t.Errorf("/healthz par le proxy : %v %v", r, err)
	} else {
		r.Body.Close()
	}

	// flux d'évènements par le proxy, puis coupure et reconnexion
	events := func() (*http.Response, error) {
		r, _ := http.NewRequest("GET", base+"/api/events", nil)
		return cl.Do(r)
	}
	sr, err := events()
	if err != nil || sr.StatusCode != 200 {
		t.Fatalf("flux par le proxy : %v %v", sr, err)
	}
	if sr.Header.Get("X-Accel-Buffering") != "no" || !strings.Contains(sr.Header.Get("Cache-Control"), "no-transform") {
		t.Errorf("en-têtes du flux perdus : %v", sr.Header)
	}
	rd := bufio.NewReader(sr.Body)
	got := make(chan error, 1)
	go func() { _, err := rd.ReadString('\n'); got <- err }()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("lecture du flux : %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("aucune donnée du flux à travers le proxy (tampon du proxy ?)")
	}
	px.CloseClientConnections() // coupure
	end := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, sr.Body); end <- err }()
	select {
	case <-end:
	case <-time.After(5 * time.Second):
		t.Fatal("le flux ne s'est pas terminé à la coupure")
	}
	sr.Body.Close()
	sr2, err := events() // reconnexion : même session
	if err != nil || sr2.StatusCode != 200 {
		t.Fatalf("reconnexion du flux : %v %v", sr2, err)
	}
	sr2.Body.Close()
	if st := call(pub); st != 200 {
		t.Errorf("appel après reconnexion : %d", st)
	}

	// session perdue (jeton régénéré) : 401 ou 403 — l'interface s'arrête alors et affiche « session expirée »
	if res, err := a.Dispatch("webUIRegen", nil); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("webUIRegen : %v %v", res, err)
	}
	if st := call(pub); st != 401 && st != 403 {
		t.Errorf("appel avec la session révoquée : %d, attendu 401 ou 403", st)
	}
	if r, err := events(); err != nil || (r.StatusCode != 401 && r.StatusCode != 403) {
		t.Errorf("flux avec la session révoquée : %v %v, attendu 401 ou 403", r, err)
	} else {
		r.Body.Close()
	}
}

// Priorité de l'adresse publique en mode serveur : --url > CL_WEB_URL > desktop_web.url (contrat). Run(args)
// réel (« --web » sur un port libre, sans navigateur), arrêt par « quit » ; l'hôte admis dit quelle source gagne.
func TestRunWebURLPrecedenceFlagEnvConfig(t *testing.T) {
	oldQuit, oldRestart, oldEnv, oldFlag := quitHook, restartApp, restartEnv, webURLFlag
	defer func() { quitHook, restartApp, restartEnv, webURLFlag = oldQuit, oldRestart, oldEnv, oldFlag }()
	for _, tc := range []struct {
		name         string
		flag, envURL string
		cfg          string
		win          string // hôte attendu admis
		lose         []string
	}{
		{"--url l'emporte", "https://flag.test", "https://env.test", "https://cfg.test", "flag.test", []string{"env.test", "cfg.test"}},
		{"CL_WEB_URL sans --url", "", "https://env.test", "https://cfg.test", "env.test", []string{"cfg.test", "flag.test"}},
		{"desktop_web.url seul", "", "", "https://cfg.test", "cfg.test", []string{"env.test", "flag.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sandbox(t, "", "alpha")
			p := l4FreePort(t)
			l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: false, listen: off, url: \"%s\" }\n", tc.cfg))
			t.Setenv("CL_DESKTOP_TOKEN", "tokURLPrecedence1234567")
			t.Setenv("CL_WEB_URL", tc.envURL)
			webURLFlag = ""
			args := []string{"--web", "--no-open", "--addr", "127.0.0.1:" + strconv.Itoa(p)}
			if tc.flag != "" {
				args = append(args, "--url", tc.flag)
			}
			// la sortie de Run (adresse d'amorçage) est jetée
			pr, pw, _ := os.Pipe()
			oldOut := os.Stdout
			os.Stdout = pw
			go func() { _, _ = io.Copy(io.Discard, pr) }()
			done := make(chan error, 1)
			go func() { done <- Run(args) }()
			end := time.Now().Add(15 * time.Second)
			for !d183Reach(p) && time.Now().Before(end) {
				time.Sleep(50 * time.Millisecond)
			}
			os.Stdout = oldOut
			tok := "tokURLPrecedence1234567"
			ck, resp := w171Session(t, p, tok, tc.win)
			if ck == nil {
				t.Fatalf("hôte %s refusé (%d) : la source attendue n'a pas gagné", tc.win, resp.StatusCode)
			}
			for _, h := range tc.lose {
				if r, _ := w171Do(t, p, "GET", "/?t="+tok, h, nil, ""); r.StatusCode != 403 {
					t.Errorf("hôte %s (source écrasée) : %d, attendu 403", h, r.StatusCode)
				}
			}
			if !ck.Secure {
				t.Errorf("adresse publique en https : cookie sans Secure : %+v", ck)
			}
			w171Do(t, p, "POST", "/api/call", tc.win, map[string]string{"Content-Type": "application/json"}, `{"method":"quit","args":{"mode":"l"}}`, ck)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Run : %v", err)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("Run ne s'arrête pas après quit")
			}
			time.Sleep(300 * time.Millisecond)
		})
	}
}

// --url invalide : erreur claire à la ligne de commande ou interface non servie (jamais une adresse acceptée).
func TestRunUnknownOptionMentionsURL(t *testing.T) {
	err := Run([]string{"--web", "--bogus"})
	if err == nil || !strings.Contains(err.Error(), "--url") {
		t.Errorf("l'aide d'option doit citer --url : %v", err)
	}
}
