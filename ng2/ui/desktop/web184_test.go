package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// LOT-4.4 — interface web SANS JETON (mode d'accès « none »). Plan : _work/reports/planner-web-sans-jeton-20261117-124406.md
// (§1 et §6). Écrit depuis le plan (TDD). Le mode « token » reste le défaut et inchangé (tests existants). Ports libres,
// HOME factice, aucun vrai projet.

// w184Start : interface allumée en mode d'accès access (« none », « token » ou "" = non réglé), adresse publique pub.
func w184Start(t *testing.T, access, pub string) (*App, int) {
	t.Helper()
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	blk := fmt.Sprintf("{ enabled: true, listen: \"127.0.0.1:%d\"", p)
	if access != "" {
		blk += ", access: " + access
	}
	if pub != "" {
		blk += fmt.Sprintf(", url: \"%s\"", pub)
	}
	l4WriteBackends(t, "  desktop_web: "+blk+" }\n")
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "on", 8*time.Second)
	return a, p
}

// w184Events : GET /api/events — statut et en-tête de refus seulement (le flux est fermé tout de suite : il ne se termine pas seul).
func w184Events(t *testing.T, p int, host string, hdr map[string]string, cookies ...*http.Cookie) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(p)+"/api/events", nil)
	if host != "" {
		req.Host = host
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("X-Launcher-Refusal")
}

func w184Call(t *testing.T, p int, host string, hdr map[string]string, body string) (*http.Response, string) {
	t.Helper()
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range hdr {
		h[k] = v
	}
	return w171Do(t, p, "POST", "/api/call", host, h, body)
}

// ── champ webUI.access, sources et garde ─────────────────────────────────────

func TestWebAccessFieldDefaultTokenAndNone(t *testing.T) {
	a, _ := w184Start(t, "", "")
	if w := l4WebUIOf(t, a); w["access"] != "token" {
		t.Errorf("sans réglage : webUI.access = %v, attendu token (défaut)", w["access"])
	}
	a2, _ := w184Start(t, "none", "")
	if w := l4WebUIOf(t, a2); w["access"] != "none" {
		t.Errorf("access: none : webUI.access = %v", w["access"])
	}
}

func TestWebAccessEnvOverridesConfigAndUnknownIsAnError(t *testing.T) {
	t.Setenv("CL_WEB_ACCESS", "none")
	a, _ := w184Start(t, "token", "")
	if w := l4WebUIOf(t, a); w["access"] != "none" {
		t.Errorf("CL_WEB_ACCESS=none doit l'emporter sur desktop_web.access: token : %v", w["access"])
	}
	t.Setenv("CL_WEB_ACCESS", "")
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", access: anonyme }\n", p))
	a2 := l4StartNative(t)
	w := l4WaitWebUI(t, a2, "error", 8*time.Second)
	if msg, _ := w["msg"].(string); !strings.Contains(msg, "access") {
		t.Errorf("valeur inconnue : message %q, attendu une erreur de réglage nommant access", msg)
	}
	if d183Reach(p) {
		t.Errorf("l'interface écoute malgré un mode d'accès inconnu")
	}
}

// Garde : « none » exige une écoute en boucle locale, ou une adresse publique en https.
func TestWebAccessNoneGuardRefusesNetworkListenWithoutHTTPSURL(t *testing.T) {
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	// écoute hors boucle locale (allow_lan accepté), sans adresse publique : refus
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"0.0.0.0:%d\", allow_lan: true, access: none }\n", p))
	a := l4StartNative(t)
	w := l4WaitWebUI(t, a, "error", 8*time.Second)
	if msg, _ := w["msg"].(string); !strings.Contains(msg, "accès sans jeton refusé") {
		t.Errorf("message %q, attendu « accès sans jeton refusé : écoute réseau sans adresse publique en https »", msg)
	}
	if d183Reach(p) {
		t.Errorf("l'interface sans jeton écoute sur le réseau sans adresse https")
	}
	// adresse publique http : toujours refusé
	p2 := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"0.0.0.0:%d\", allow_lan: true, access: none, url: \"http://launcher.test\" }\n", p2))
	a2 := l4StartNative(t)
	l4WaitWebUI(t, a2, "error", 8*time.Second)
	// adresse publique https : acceptée
	p3 := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"0.0.0.0:%d\", allow_lan: true, access: none, url: \"https://launcher.test\" }\n", p3))
	a3 := l4StartNative(t)
	l4WaitWebUI(t, a3, "on", 8*time.Second)
	// mode token sur le réseau : inchangé (accepté avec allow_lan)
	p4 := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"0.0.0.0:%d\", allow_lan: true }\n", p4))
	a4 := l4StartNative(t)
	l4WaitWebUI(t, a4, "on", 8*time.Second)
}

// ── aucun jeton, aucun cookie, aucune session ────────────────────────────────

func TestWebAccessNoneHasNoTokenNoCookieNoSession(t *testing.T) {
	t.Setenv("CL_DESKTOP_TOKEN", "tokIgnoredInNoneMode123456")
	a, p := w184Start(t, "none", "")
	_ = a
	if _, err := os.Stat(l4TokenFile()); err == nil {
		t.Errorf("fichier desktop-web-token créé en mode none")
	}
	host := "127.0.0.1:" + strconv.Itoa(p)
	for _, path := range []string{"/", "/index.html"} {
		resp, body := w171Do(t, p, "GET", path, host, nil, "")
		redirectToRoot := path == "/index.html" && resp.StatusCode == http.StatusMovedPermanently && (resp.Header.Get("Location") == "./" || resp.Header.Get("Location") == "/")
		if !redirectToRoot && (resp.StatusCode != 200 || !strings.Contains(strings.ToLower(body), "<html")) { // /index.html : le serveur de fichiers renvoie vers « / » (équivalent)
			t.Errorf("GET %s : %d, attendu la page sans jeton", path, resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Errorf("GET %s : cookie posé en mode none : %v", path, resp.Cookies())
		}
	}
	// ancien lien /?t=… : redirigé vers /, sans cookie (jeton quelconque, y compris CL_DESKTOP_TOKEN ignoré)
	for _, tok := range []string{"nimporte-quoi", "tokIgnoredInNoneMode123456", ""} {
		resp, _ := w171Do(t, p, "GET", "/?t="+tok, host, nil, "")
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" || len(resp.Cookies()) != 0 {
			t.Errorf("/?t=%q : %d Location %q cookies %v, attendu 303 vers / sans cookie", tok, resp.StatusCode, resp.Header.Get("Location"), resp.Cookies())
		}
	}
	// appels sans cookie : acceptés ; un ancien cookie est ignoré
	if resp, _ := w184Call(t, p, host, nil, w171Call); resp.StatusCode != 200 {
		t.Errorf("/api/call sans session : %d, attendu 200", resp.StatusCode)
	}
	old := &http.Cookie{Name: "__Host-cl_session", Value: "ancien-cookie-sans-valeur-0123456789abcdef"}
	if resp, _ := w171Do(t, p, "POST", "/api/call", host, map[string]string{"Content-Type": "application/json"}, w171Call, old); resp.StatusCode != 200 {
		t.Errorf("/api/call avec un ancien cookie : %d, attendu 200 (cookie ignoré)", resp.StatusCode)
	}
	// flux d'évènements sans cookie
	if st, _ := w184Events(t, p, host, nil); st != 200 {
		t.Errorf("/api/events sans session : %d", st)
	}
	// le refus « session » n'est jamais émis
	for _, r := range []*http.Response{
		func() *http.Response { r, _ := w171Do(t, p, "GET", "/api/call", host, nil, ""); return r }(),
		func() *http.Response {
			r, _ := w171Do(t, p, "POST", "/api/call", host, map[string]string{"Content-Type": "text/plain"}, "x")
			return r
		}(),
	} {
		if r.Header.Get("X-Launcher-Refusal") == "session" {
			t.Errorf("refus « session » émis en mode none")
		}
	}
}

// ── protections conservées : hôte, origine, méthode, corps JSON ──────────────

func TestWebAccessNoneKeepsHostOriginMethodAndJSONChecks(t *testing.T) {
	_, p := w184Start(t, "none", "https://launcher.test:8443")
	loc := "127.0.0.1:" + strconv.Itoa(p)
	pub := "launcher.test:8443"
	for _, tc := range []struct {
		name, host string
		hdr        map[string]string
		method     string
		body       string
		status     int
		kind       string
	}{
		{"autre hôte", "evil.test:8443", nil, "POST", w171Call, 403, "host"},
		{"autre origine", pub, map[string]string{"Origin": "https://evil.test:8443"}, "POST", w171Call, 403, "origin"},
		{"origine publique exacte", pub, map[string]string{"Origin": "https://launcher.test:8443"}, "POST", w171Call, 200, ""},
		{"hôte local, sans Origin", loc, nil, "POST", w171Call, 200, ""},
		{"méthode GET sur /api/call", loc, nil, "GET", "", 405, ""},
		{"corps non JSON", loc, map[string]string{"Content-Type": "text/plain"}, "POST", "x", 415, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := map[string]string{"Content-Type": "application/json"}
			for k, v := range tc.hdr {
				h[k] = v
			}
			resp, _ := w171Do(t, p, tc.method, "/api/call", tc.host, h, tc.body)
			if tc.status == 405 || tc.status == 415 { // refus de méthode / de type : tout refus 4xx
				if resp.StatusCode < 400 || resp.StatusCode >= 500 {
					t.Errorf("%d, attendu un refus 4xx", resp.StatusCode)
				}
				return
			}
			if resp.StatusCode != tc.status {
				t.Errorf("%d, attendu %d", resp.StatusCode, tc.status)
			}
			if tc.kind != "" && resp.Header.Get("X-Launcher-Refusal") != tc.kind {
				t.Errorf("X-Launcher-Refusal = %q, attendu %q", resp.Header.Get("X-Launcher-Refusal"), tc.kind)
			}
		})
	}
	// sondes sans session ni contrôle de Host
	for _, path := range []string{"/healthz", "/readyz"} {
		if r, _ := w171Do(t, p, "GET", path, "10.1.2.3:"+strconv.Itoa(p), nil, ""); r.StatusCode != 200 {
			t.Errorf("%s : %d", path, r.StatusCode)
		}
	}
}

// ── Sec-Fetch-Site: cross-site refusé dans les DEUX modes ────────────────────

func TestSecFetchSiteCrossSiteIsRefusedInBothModes(t *testing.T) {
	for _, mode := range []string{"none", "token"} {
		t.Run(mode, func(t *testing.T) {
			_, p := w184Start(t, mode, "")
			host := "127.0.0.1:" + strconv.Itoa(p)
			var cookies []*http.Cookie
			if mode == "token" {
				ck, _ := w171Session(t, p, l4WebToken(t), host)
				if ck == nil {
					t.Fatal("pas de session en mode token")
				}
				cookies = []*http.Cookie{ck}
			}
			do := func(path, method, site, body string) (int, string) {
				if path == "/api/events" {
					h := map[string]string{}
					if site != "" {
						h["Sec-Fetch-Site"] = site
					}
					return w184Events(t, p, host, h, cookies...)
				}
				h := map[string]string{"Content-Type": "application/json"}
				if site != "" {
					h["Sec-Fetch-Site"] = site
				}
				r, _ := w171Do(t, p, method, path, host, h, body, cookies...)
				return r.StatusCode, r.Header.Get("X-Launcher-Refusal")
			}
			for _, path := range []string{"/api/call", "/api/events"} {
				method, body := "POST", w171Call
				if path == "/api/events" {
					method, body = "GET", ""
				}
				if st, kind := do(path, method, "cross-site", body); st != 403 || kind != "origin" {
					t.Errorf("%s cross-site : %d (%q), attendu 403 origin", path, st, kind)
				}
				for _, ok := range []string{"same-origin", "none", ""} {
					if st, _ := do(path, method, ok, body); st != 200 {
						t.Errorf("%s Sec-Fetch-Site=%q : %d, attendu 200", path, ok, st)
					}
				}
			}
		})
	}
}

// ── régénération, ouverture ──────────────────────────────────────────────────

func TestWebAccessNoneRegenRefusedAndOpenWithoutToken(t *testing.T) {
	out := d183FakeOpener(t)
	a, p := w184Start(t, "none", "")
	res, err := a.Dispatch("webUIRegen", nil)
	m, _ := res.(map[string]any)
	if err != nil && m == nil {
		m = map[string]any{"ok": false, "msg": err.Error()}
	}
	if m["ok"] != false || !strings.Contains(fmt.Sprint(m["msg"]), "accès sans jeton") {
		t.Errorf("webUIRegen en mode none : %v, attendu ok=false « accès sans jeton : aucun jeton à régénérer »", m)
	}
	if _, serr := os.Stat(l4TokenFile()); serr == nil {
		t.Errorf("webUIRegen a créé un fichier de jeton")
	}
	if r, err := a.Dispatch("webUIOpen", nil); err != nil || r.(map[string]any)["ok"] != true {
		t.Fatalf("webUIOpen : %v %v", r, err)
	}
	got := d183WaitOpened(t, out)
	if want := "http://127.0.0.1:" + strconv.Itoa(p) + "/"; got != want {
		t.Errorf("adresse ouverte %q, attendu %q (sans ?t=)", got, want)
	}
	// un client web ne change pas le mode d'accès
	b, _ := json.Marshal(map[string]any{"kind": "hub", "mtime": func() any {
		mm, _ := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
		return mm.(map[string]any)["mtime"]
	}(), "changes": []map[string]any{{"op": "set", "at": []string{"desktop_web", "access"}, "value": "none"}}})
	before, _ := os.ReadFile(core.BackendsPath())
	res2, err2 := a.DispatchFrom(originWeb, "configApply", b)
	if mm, _ := res2.(map[string]any); err2 == nil && (mm == nil || mm["invalid"] != true) {
		t.Errorf("configApply desktop_web.access depuis le web accepté : %v", res2)
	}
	if after, _ := os.ReadFile(core.BackendsPath()); string(after) != string(before) {
		t.Errorf("backends.yaml modifié par un client web")
	}
}

// Mode token inchangé : jeton, cookie, session, refus « session ».
func TestWebAccessTokenModeUnchanged(t *testing.T) {
	_, p := w184Start(t, "token", "")
	host := "127.0.0.1:" + strconv.Itoa(p)
	if r, _ := w171Do(t, p, "GET", "/", host, nil, ""); r.StatusCode == 200 {
		t.Errorf("page servie sans jeton en mode token")
	}
	if r, _ := w184Call(t, p, host, nil, w171Call); r.StatusCode != 403 || r.Header.Get("X-Launcher-Refusal") != "session" {
		t.Errorf("appel sans session en mode token : %d %q, attendu 403 session", r.StatusCode, r.Header.Get("X-Launcher-Refusal"))
	}
	if _, err := os.Stat(l4TokenFile()); err != nil {
		t.Errorf("fichier du jeton absent en mode token : %v", err)
	}
}

// ── cycle 1 (audit sécurité LOT-4.4) ─────────────────────────────────────────

// M1 : « none » refusé dès qu'une adresse publique est donnée et n'est pas https, même en boucle locale ;
// la boucle locale SANS adresse publique reste admise, et avec une adresse https aussi.
func TestWebAccessNoneRefusedWithPublicHTTPURLEvenOnLoopback(t *testing.T) {
	for _, tc := range []struct {
		name, pub string
		ok        bool
	}{
		{"boucle locale seule", "", true},
		{"boucle locale + url https", "https://launcher.test", true},
		{"boucle locale + url http", "http://launcher.test", false},
		{"boucle locale + url http avec port", "http://launcher.test:8080", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sandbox(t, "", "alpha")
			p := l4FreePort(t)
			blk := fmt.Sprintf("{ enabled: true, listen: \"127.0.0.1:%d\", access: none", p)
			if tc.pub != "" {
				blk += fmt.Sprintf(", url: \"%s\"", tc.pub)
			}
			l4WriteBackends(t, "  desktop_web: "+blk+" }\n")
			a := l4StartNative(t)
			if tc.ok {
				l4WaitWebUI(t, a, "on", 8*time.Second)
				return
			}
			w := l4WaitWebUI(t, a, "error", 8*time.Second)
			if msg, _ := w["msg"].(string); !strings.Contains(msg, "sans jeton") || !strings.Contains(msg, "https") {
				t.Errorf("message %q, attendu une raison claire (accès sans jeton refusé, adresse publique non https)", msg)
			}
			if d183Reach(p) {
				t.Errorf("l'interface sans jeton écoute malgré une adresse publique en http")
			}
		})
	}
}

// M2 : en mode none, Sec-Fetch-Site: same-site est refusé (seuls same-origin et none sont admis) ; en mode token il est admis.
func TestSecFetchSiteSameSiteRefusedInNoneOnly(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		wantCode int
	}{{"none", 403}, {"token", 200}} {
		t.Run(tc.mode, func(t *testing.T) {
			_, p := w184Start(t, tc.mode, "")
			host := "127.0.0.1:" + strconv.Itoa(p)
			var cookies []*http.Cookie
			if tc.mode == "token" {
				ck, _ := w171Session(t, p, l4WebToken(t), host)
				cookies = []*http.Cookie{ck}
			}
			h := map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-site"}
			r, _ := w171Do(t, p, "POST", "/api/call", host, h, w171Call, cookies...)
			if r.StatusCode != tc.wantCode {
				t.Errorf("/api/call same-site (%s) : %d, attendu %d", tc.mode, r.StatusCode, tc.wantCode)
			}
			if tc.wantCode == 403 && r.Header.Get("X-Launcher-Refusal") != "origin" {
				t.Errorf("refus %q, attendu origin", r.Header.Get("X-Launcher-Refusal"))
			}
			st, kind := w184Events(t, p, host, map[string]string{"Sec-Fetch-Site": "same-site"}, cookies...)
			if st != tc.wantCode || (tc.wantCode == 403 && kind != "origin") {
				t.Errorf("/api/events same-site (%s) : %d %q, attendu %d", tc.mode, st, kind, tc.wantCode)
			}
			// en none, same-origin et none restent admis ; en-tête absent : règles actuelles
			for _, ok := range []string{"same-origin", "none", ""} {
				hh := map[string]string{"Content-Type": "application/json"}
				if ok != "" {
					hh["Sec-Fetch-Site"] = ok
				}
				if r, _ := w171Do(t, p, "POST", "/api/call", host, hh, w171Call, cookies...); r.StatusCode != 200 {
					t.Errorf("%s : Sec-Fetch-Site=%q : %d, attendu 200", tc.mode, ok, r.StatusCode)
				}
			}
		})
	}
}

// F1 : mode token — une navigation de premier niveau depuis un autre site vers /?t=<jeton> (lien cliqué) reste admise :
// le refus de cross-site ne vise que /api/call et /api/events.
func TestTokenModeTopLevelCrossSiteNavigationToBootstrapStillWorks(t *testing.T) {
	_, p := w184Start(t, "token", "")
	host := "127.0.0.1:" + strconv.Itoa(p)
	h := map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Sec-Fetch-User": "?1"}
	resp, _ := w171Do(t, p, "GET", "/?t="+l4WebToken(t), host, h, "")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("navigation cross-site vers /?t= : %d (Location %q), attendu 303 vers /", resp.StatusCode, resp.Header.Get("Location"))
	}
	got := false
	for _, c := range resp.Cookies() {
		got = got || strings.Contains(c.Name, "cl_session")
	}
	if !got {
		t.Errorf("pas de cookie de session posé par la navigation cross-site")
	}
	// … alors que l'API, depuis un autre site, reste refusée
	hh := map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}
	ck := resp.Cookies()
	if r, _ := w171Do(t, p, "POST", "/api/call", host, hh, w171Call, ck...); r.StatusCode != 403 {
		t.Errorf("/api/call cross-site : %d, attendu 403", r.StatusCode)
	}
}

// w184Wait : attend la condition jusqu'à l'échéance (large : machine chargée) ; échec explicite sinon.
func w184Wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(45 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("condition non atteinte en 45 s : %s", what)
}

func w184TokenFileExists() bool { _, err := os.Stat(l4TokenFile()); return err == nil }

// F3 : au passage en none (au démarrage ou à chaud), le fichier desktop-web-token résiduel est supprimé.
func TestWebAccessNoneRemovesStaleTokenFile(t *testing.T) {
	// à chaud : démarré en token (le fichier existe), puis « none » par « Ce poste »
	a, _ := w184Start(t, "token", "")
	w184Wait(t, "fichier du jeton en mode token", w184TokenFileExists)
	m, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"kind": "hub", "mtime": m.(map[string]any)["mtime"], "changes": []map[string]any{
		{"op": "set", "at": []string{"desktop_web", "access"}, "value": "none"}}})
	res, err := a.DispatchFrom(originWindow, "configApply", b)
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Fatalf("configApply access none : %v %v", res, err)
	}
	w184Wait(t, "webUI.access = none", func() bool { return l4WebUIOf(t, a)["access"] == "none" })
	w184Wait(t, "desktop-web-token supprimé après le passage à chaud en none", func() bool { return !w184TokenFileExists() })

	// au démarrage : fichier résiduel posé avant le lancement en none
	sandbox(t, "", "alpha")
	_ = os.MkdirAll(core.CacheDir(), 0o755)
	if err := os.WriteFile(l4TokenFile(), []byte("jeton-residuel-0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", access: none }\n", p))
	a2 := l4StartNative(t)
	l4WaitWebUI(t, a2, "on", 30*time.Second)
	w184Wait(t, "desktop-web-token résiduel supprimé au démarrage en mode none", func() bool { return !w184TokenFileExists() })

	// retour en token : un nouveau jeton est tiré (le fichier réapparaît) ; le réglage est relu jusqu'à son effet
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", access: token }\n", p))
	w184Wait(t, "retour en mode token : jeton recréé", func() bool {
		a2.applyWebUI()
		return l4WebUIOf(t, a2)["access"] == "token" && w184TokenFileExists()
	})
}
