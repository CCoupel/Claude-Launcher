package desktop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #163 (LOT-4) — l'interface web est servie par la fenêtre native, dans le même processus, sur la même App.
// Contrat : contracts/desktop-dispatch.md (« LOT-4 : interface web servie par la fenêtre native »).
// Fenêtre simulée = appels directs à App.Dispatch (origine « fenêtre ») ; client web = HTTP sur l'adresse du
// réglage `desktop_web` (origine « web », portée par chaque appel). Ports libres tirés au hasard, jamais de
// vrai moteur, de vrai projet ni de presse-papiers du poste (hooks de test).

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// writeBackends : backends.yaml du bac à sable (le serveur des fichiers est éteint : seul le serveur de
// l'interface est en jeu), complété par extra.
func writeBackends(t *testing.T, extra string) {
	t.Helper()
	y := "defaults:\n  browser_server: { listen: off }\n" + extra
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
}

func webUIOf(t *testing.T, a *App) map[string]any {
	t.Helper()
	b, _ := json.Marshal(a.Snapshot())
	var s map[string]any
	_ = json.Unmarshal(b, &s)
	w, _ := s["webUI"].(map[string]any)
	if w == nil {
		t.Fatalf("instantané sans champ webUI (contrat #163 : {state, listen, msg}) — %d octets", len(b))
	}
	return w
}

func waitWebUI(t *testing.T, a *App, state string, d time.Duration) map[string]any {
	t.Helper()
	end := time.Now().Add(d)
	var w map[string]any
	for time.Now().Before(end) {
		w = webUIOf(t, a)
		if w["state"] == state {
			return w
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("webUI.state = %v après %v, attendu %q (%v)", w["state"], d, state, w)
	return nil
}

// startNative : « fenêtre native » de test — App démarrée (Start lit le réglage desktop_web) ; arrêtée en fin
// de test. Le presse-papiers du poste est remplacé par des crochets de test.
func startNative(t *testing.T) *App {
	t.Helper()
	var r recorder
	a := newApp(t, r.emit)
	a.Start()
	t.Cleanup(a.Stop)
	return a
}

func tokenFile() string { return filepath.Join(core.CacheDir(), "desktop-web-token") }

func webToken(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(tokenFile())
	if err != nil {
		t.Fatalf("jeton du poste absent (%s) : %v", tokenFile(), err)
	}
	return strings.TrimSpace(string(b))
}

func doWeb(t *testing.T, addr, method, path, host, cookie, body, ctype, origin string) (*http.Response, string) {
	t.Helper()
	return doWith(t, "http://"+addr, method, path, host, cookie, body, ctype, origin)
}

func webCall(t *testing.T, addr, tok, method string, args any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"method": method, "args": args})
	resp, body := doWeb(t, addr, "POST", "/api/call", "", tok, string(b), "application/json", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%s : HTTP %d %s", method, resp.StatusCode, body)
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(body), &v)
	return v
}

// ── réglage, démarrage, arrêt, priorité ───────────────────────────────────

// Par défaut (aucun réglage) et `off` : rien n'écoute, webUI.state = off.
func TestDesktopWebOffByDefaultAndExplicit(t *testing.T) {
	sandbox(t, "", "alpha")
	writeBackends(t, "")
	a := startNative(t)
	if w := waitWebUI(t, a, "off", 5*time.Second); w["listen"] != "" && w["listen"] != nil {
		t.Errorf("défaut : webUI = %v, attendu off sans adresse", w)
	}
	p := freePort(t)
	writeBackends(t, "  desktop_web: { listen: off }\n")
	a2 := startNative(t)
	waitWebUI(t, a2, "off", 5*time.Second)
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(p), 300*time.Millisecond); err == nil {
		c.Close()
		t.Errorf("quelque chose écoute sur %d avec desktop_web off", p)
	}
}

// Priorité : hubs.<machine>.<système> > hubs.<machine> > defaults ; « 8770 » = 127.0.0.1:8770.
func TestDesktopWebPriorityAndListenForms(t *testing.T) {
	sandbox(t, "", "alpha")
	pDef, pHub, pOS := freePort(t), freePort(t), freePort(t)
	host := strings.ToLower(core.HubHost())
	for _, tc := range []struct {
		name, yaml string
		port       int
	}{
		{"défaut seul (forme « port »)", fmt.Sprintf("  desktop_web: { listen: \"%d\" }\n", pDef), pDef},
		{"machine > défaut", fmt.Sprintf("  desktop_web: { listen: \"%d\" }\nhubs:\n  %s:\n    desktop_web: { listen: \"127.0.0.1:%d\" }\n", pDef, host, pHub), pHub},
		{"système > machine > défaut", fmt.Sprintf("  desktop_web: { listen: \"%d\" }\nhubs:\n  %s:\n    desktop_web: { listen: \"127.0.0.1:%d\" }\n    %s:\n      desktop_web: { listen: \"127.0.0.1:%d\" }\n", pDef, host, pHub, runtime.GOOS, pOS), pOS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeBackends(t, tc.yaml)
			a := startNative(t)
			w := waitWebUI(t, a, "on", 8*time.Second)
			if want := "127.0.0.1:" + strconv.Itoa(tc.port); w["listen"] != want {
				t.Errorf("webUI.listen = %v, attendu %s", w["listen"], want)
			}
			resp, _ := doWeb(t, "127.0.0.1:"+strconv.Itoa(tc.port), "GET", "/?t="+webToken(t), "", "", "", "", "")
			if resp.StatusCode != 200 {
				t.Errorf("amorçage sur %d : %d", tc.port, resp.StatusCode)
			}
		})
	}
}

// Démarré, arrêté et déplacé à chaud selon le réglage, par « Ce poste » (configApply kind hub) :
// le jeton, lui, reste celui du poste.
func TestDesktopWebHotReloadStartStopMove(t *testing.T) {
	sandbox(t, "", "alpha")
	writeBackends(t, "")
	a := startNative(t)
	waitWebUI(t, a, "off", 5*time.Second)
	p1, p2 := freePort(t), freePort(t)
	setListen := func(v string) {
		t.Helper()
		m, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
		if err != nil {
			t.Fatal(err)
		}
		mtime := m.(map[string]any)["mtime"]
		b, _ := json.Marshal(map[string]any{"kind": "hub", "mtime": mtime, "changes": []any{
			map[string]any{"op": "set", "at": []string{"desktop_web"}, "value": map[string]any{"listen": v}}}})
		res, err := a.Dispatch("configApply", b)
		if err != nil {
			t.Fatal(err)
		}
		if rm, _ := res.(map[string]any); rm == nil || rm["saved"] != true {
			t.Fatalf("configApply desktop_web=%q : %v", v, res)
		}
	}
	reach := func(p int) bool {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(p) + "/?t=" + webToken(t))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	waitReach := func(p int, want bool) {
		t.Helper()
		end := time.Now().Add(8 * time.Second)
		for time.Now().Before(end) {
			if reach(p) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("port %d : joignable = %v attendu %v", p, !want, want)
	}
	setListen("127.0.0.1:" + strconv.Itoa(p1)) // démarrage
	waitWebUI(t, a, "on", 8*time.Second)
	waitReach(p1, true)
	tok1 := webToken(t)
	setListen("127.0.0.1:" + strconv.Itoa(p2)) // déplacement
	waitReach(p2, true)
	waitReach(p1, false)
	if webToken(t) != tok1 {
		t.Error("le jeton a changé au déplacement du serveur")
	}
	setListen("off") // arrêt
	waitWebUI(t, a, "off", 8*time.Second)
	waitReach(p2, false)
}

// Adresse déjà prise : webUI.state = error avec un message, la fenêtre continue ; jamais le jeton.
func TestDesktopWebPortBusyReportsError(t *testing.T) {
	sandbox(t, "", "alpha")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p := ln.Addr().(*net.TCPAddr).Port
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	a := startNative(t)
	w := waitWebUI(t, a, "error", 20*time.Second)
	if msg, _ := w["msg"].(string); msg == "" {
		t.Errorf("webUI en erreur sans message : %v", w)
	}
}

// ── jeton par poste : fichier 0600, conservé après relance, jamais dans l'instantané ─────

func TestDesktopWebTokenPerHostKeptAndPrivate(t *testing.T) {
	sandbox(t, "", "alpha")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	var r recorder
	a := newApp(t, r.emit)
	a.Start()
	waitWebUI(t, a, "on", 8*time.Second)
	tok := webToken(t)
	if len(tok) < 16 {
		t.Fatalf("jeton trop court : %q", tok)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(tokenFile()); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s : %v %v, attendu 0600", tokenFile(), st, err)
		}
	}
	// jamais dans l'instantané, ni dans le modèle « Ce poste »
	snap, _ := json.Marshal(a.Snapshot())
	hub, _ := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
	hb, _ := json.Marshal(hub)
	for what, b := range map[string][]byte{"instantané": snap, "configModel hub": hb} {
		if strings.Contains(string(b), tok) {
			t.Errorf("le jeton figure dans %s", what)
		}
	}
	a.Stop()
	// relance : même jeton, un navigateur déjà ouvert reste valable
	var r2 recorder
	b := newApp(t, r2.emit)
	b.Start()
	t.Cleanup(b.Stop)
	waitWebUI(t, b, "on", 20*time.Second)
	if webToken(t) != tok {
		t.Error("jeton changé après relance : un navigateur déjà ouvert ne serait plus valable")
	}
	if resp, _ := doWeb(t, "127.0.0.1:"+strconv.Itoa(p), "GET", "/?t="+tok, "", "", "", "", ""); resp.StatusCode != 200 {
		t.Errorf("ancien jeton refusé après relance : %d", resp.StatusCode)
	}
}

// ── CRITICAL : protections du mode --web inchangées sur le serveur de la fenêtre ─────

func TestDesktopWebProtectionsLoopback(t *testing.T) {
	sandbox(t, "", "alpha")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	a := startNative(t)
	waitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), webToken(t)
	do := func(method, path, host, cookie, body, ctype, origin string) *http.Response {
		resp, _ := doWeb(t, addr, method, path, host, cookie, body, ctype, origin)
		return resp
	}
	// page d'amorçage : jeton de l'URL accepté ; cookie HttpOnly SameSite=Strict ; en-têtes de sécurité
	resp := do("GET", "/?t="+tok, "", "", "", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("amorçage : %d", resp.StatusCode)
	}
	var ck *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "cl_token" {
			ck = c
		}
	}
	if ck == nil || ck.Value != tok || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie : %+v, attendu cl_token=jeton, HttpOnly, SameSite=Strict", ck)
	}
	for k, v := range map[string]string{"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
		if resp.Header.Get(k) != v {
			t.Errorf("en-tête %s = %q", k, resp.Header.Get(k))
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP : %q", resp.Header.Get("Content-Security-Policy"))
	}
	// sans jeton : refus ; jeton faux : refus
	for _, path := range []string{"/", "/?t=faux", "/index.html"} {
		if r := do("GET", path, "", "", "", "", ""); r.StatusCode != 403 {
			t.Errorf("GET %s sans bon jeton : %d", path, r.StatusCode)
		}
	}
	// appels : cookie seulement (jamais le jeton de l'URL), JSON, même origine
	call := `{"method":"configFiles","args":{}}`
	if r := do("POST", "/api/call?t="+tok, "", "", call, "application/json", ""); r.StatusCode != 403 {
		t.Errorf("/api/call avec jeton en query : %d", r.StatusCode)
	}
	if r := do("GET", "/api/events?t="+tok, "", "", "", "", ""); r.StatusCode != 403 {
		t.Errorf("/api/events avec jeton en query : %d", r.StatusCode)
	}
	if r := do("POST", "/api/call", "", tok, call, "text/plain", ""); r.StatusCode != 415 && r.StatusCode != 403 {
		t.Errorf("/api/call en text/plain : %d, attendu un refus", r.StatusCode)
	}
	if r := do("POST", "/api/call", "", tok, call, "application/json", "http://evil.example"); r.StatusCode != 403 {
		t.Errorf("/api/call d'une autre origine : %d", r.StatusCode)
	}
	if r := do("POST", "/api/call", "", tok, call, "application/json", "http://"+addr); r.StatusCode != 200 {
		t.Errorf("/api/call même origine : %d", r.StatusCode)
	}
	if r := do("POST", "/api/call", "", tok, call, "application/json", ""); r.StatusCode != 200 {
		t.Errorf("/api/call cookie + JSON : %d", r.StatusCode)
	}
	if r := do("POST", "/api/call", "", "", call, "application/json", ""); r.StatusCode != 403 {
		t.Errorf("/api/call sans cookie : %d", r.StatusCode)
	}
	// contrôle de Host sur la boucle locale (parade au DNS rebinding)
	for _, h := range []string{"evil.example", "evil.example:" + strconv.Itoa(p), "192.168.1.5:" + strconv.Itoa(p), "localhost:1"} {
		if r := do("GET", "/?t="+tok, h, "", "", "", ""); r.StatusCode != 403 {
			t.Errorf("Host %q accepté : %d", h, r.StatusCode)
		}
	}
	for _, h := range []string{"localhost:" + strconv.Itoa(p), "127.0.0.1:" + strconv.Itoa(p)} {
		if r := do("GET", "/?t="+tok, h, "", "", "", ""); r.StatusCode != 200 {
			t.Errorf("Host %q refusé : %d", h, r.StatusCode)
		}
	}
}

// Écoute hors boucle locale : le contrôle de Host porte sur l'hôte d'écoute, le nom de la machine et localhost ;
// les autres noms sont refusés ; le jeton reste exigé.
func TestDesktopWebProtectionsNonLoopback(t *testing.T) {
	sandbox(t, "", "alpha")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"0.0.0.0:%d\" }\n", p))
	a := startNative(t)
	waitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), webToken(t)
	machine, _ := os.Hostname()
	sp := strconv.Itoa(p)
	for _, h := range []string{"localhost:" + sp, machine + ":" + sp} {
		if resp, _ := doWeb(t, addr, "GET", "/?t="+tok, h, "", "", "", ""); resp.StatusCode != 200 {
			t.Errorf("Host %q refusé en écoute hors boucle locale : %d", h, resp.StatusCode)
		}
	}
	for _, h := range []string{"evil.example:" + sp, "evil.example", "attaquant.lan:" + sp} {
		if resp, _ := doWeb(t, addr, "GET", "/?t="+tok, h, "", "", "", ""); resp.StatusCode != 403 {
			t.Errorf("Host %q accepté en écoute hors boucle locale : %d (DNS rebinding)", h, resp.StatusCode)
		}
	}
	if resp, _ := doWeb(t, addr, "GET", "/", "localhost:"+sp, "", "", "", ""); resp.StatusCode != 403 {
		t.Errorf("sans jeton : %d", resp.StatusCode)
	}
	if resp, _ := doWeb(t, addr, "POST", "/api/call", "localhost:"+sp, "", `{"method":"configFiles","args":{}}`, "application/json", ""); resp.StatusCode != 403 {
		t.Errorf("appel sans cookie : %d", resp.StatusCode)
	}
	// l'avertissement est aussi dit dans l'instantané (jamais le jeton)
	if w := webUIOf(t, a); strings.Contains(fmt.Sprint(w), tok) {
		t.Errorf("webUI contient le jeton : %v", w)
	}
}

// ── CRITICAL : origine portée par chaque appel (même App, deux clients) ───────────────

// Le presse-papiers du poste n'est JAMAIS servi à un client web, même quand la fenêtre native, dans le même
// processus, y a accès ; quit, setTheme et webUIOpen sont refusés à un client web.
func TestDesktopWebOriginPerCall(t *testing.T) {
	sandbox(t, "", "alpha")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))

	var clip atomic.Value
	clip.Store("secret du poste")
	oldSet, oldGet := clipboardSetHook, clipboardGetHook
	clipboardSetHook = func(s string) error { clip.Store(s); return nil }
	clipboardGetHook = func() (string, error) { return clip.Load().(string), nil }
	var quits, themes atomic.Int32
	oldQuit, oldTheme := quitHook, themeHook
	quitHook = func() { quits.Add(1) }
	themeHook = func(string) { themes.Add(1) }
	t.Cleanup(func() { clipboardSetHook, clipboardGetHook, quitHook, themeHook = oldSet, oldGet, oldQuit, oldTheme })

	a := startNative(t)
	waitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), webToken(t)

	// la fenêtre (même App, même processus) a le presse-papiers
	if res := a.clipboardRead(); res["ok"] != true || res["text"] != "secret du poste" {
		t.Fatalf("fenêtre : clipboardRead = %v (précondition)", res)
	}
	// le client web, non
	read := webCall(t, addr, tok, "clipboardRead", map[string]any{})
	body, _ := json.Marshal(read)
	res, _ := read["result"].(map[string]any)
	if res["ok"] != false || res["unavailable"] != true || strings.Contains(string(body), "secret du poste") {
		t.Errorf("clipboardRead web : %s — le presse-papiers du poste ne doit jamais être servi", body)
	}
	w := webCall(t, addr, tok, "clipboardWrite", map[string]any{"text": "écrit par le navigateur"})
	res, _ = w["result"].(map[string]any)
	if res["ok"] != false || res["unavailable"] != true {
		t.Errorf("clipboardWrite web : %v, attendu unavailable", w)
	}
	if clip.Load().(string) != "secret du poste" {
		t.Errorf("le presse-papiers du poste a été modifié par un client web : %q", clip.Load())
	}
	// et la fenêtre, juste après, l'a toujours (la règle est par appel, pas par processus)
	if res := a.clipboardRead(); res["ok"] != true || res["text"] != "secret du poste" {
		t.Errorf("fenêtre : clipboardRead après un appel web = %v", res)
	}

	// quit, setTheme, webUIOpen : refusés à un client web, sans effet
	for _, m := range []struct {
		name string
		args any
	}{{"quit", map[string]any{"mode": "t"}}, {"quit", map[string]any{}}, {"setTheme", map[string]any{"mode": "dark"}}, {"webUIOpen", map[string]any{}}} {
		v := webCall(t, addr, tok, m.name, m.args)
		res, _ := v["result"].(map[string]any)
		refused := v["error"] != nil || res != nil && res["ok"] == false
		if !refused {
			b, _ := json.Marshal(v)
			t.Errorf("%s accepté par un client web : %s", m.name, b)
		}
	}
	time.Sleep(400 * time.Millisecond) // quit lance sa sortie en tâche de fond
	if quits.Load() != 0 {
		t.Error("un client web a fermé la fenêtre native (quit)")
	}
	if themes.Load() != 0 {
		t.Error("un client web a changé le thème de la fenêtre native")
	}
	select {
	case <-a.quit:
		t.Error("l'App a été arrêtée par un client web")
	default:
	}
	// la fenêtre, elle, garde ses droits : setTheme atteint la fenêtre
	if _, err := a.Dispatch("setTheme", json.RawMessage(`{"mode":"dark"}`)); err != nil || themes.Load() != 1 {
		t.Errorf("fenêtre : setTheme → %v (appels %d)", err, themes.Load())
	}
}

// ── deux clients sur une même App : état commun, évènements aux deux ─────────────────

func TestDesktopWebTwoClientsShareState(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	var r recorder
	a := newApp(t, r.emit) // émetteur de la fenêtre simulée
	a.Start()
	t.Cleanup(a.Stop)
	waitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), webToken(t)

	// flux d'évènements du client web
	req, _ := http.NewRequest("GET", "http://"+addr+"/api/events", nil)
	req.AddCookie(&http.Cookie{Name: "cl_token", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("flux d'évènements : %v %v", resp, err)
	}
	defer resp.Body.Close()
	names := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				var e struct {
					Name string `json:"name"`
				}
				if json.Unmarshal([]byte(l[6:]), &e) == nil && e.Name != "" {
					select {
					case names <- e.Name:
					default:
					}
				}
			}
		}
		close(names)
	}()

	// la fenêtre sélectionne alpha : le web voit la même sélection et reçoit des évènements
	if _, err := a.Dispatch("select", json.RawMessage(`{"path":`+strconv.Quote(ps[0])+`}`)); err != nil {
		t.Fatal(err)
	}
	if got := webCall(t, addr, tok, "init", map[string]any{}); fmt.Sprint(got["result"]) == "" {
		t.Fatal("init web vide")
	} else if b, _ := json.Marshal(got); !strings.Contains(string(b), strconv.Quote(ps[0])[1:len(strconv.Quote(ps[0]))-1]) {
		t.Errorf("le client web ne voit pas la sélection de la fenêtre (%s) : %.300s", ps[0], b)
	}
	// le web sélectionne beta : la fenêtre (même App) le voit
	webCall(t, addr, tok, "select", map[string]any{"path": ps[1]})
	if s := a.Snapshot().Selected; s != ps[1] {
		t.Errorf("sélection de l'App après un select web : %q, attendu %q (état commun)", s, ps[1])
	}
	// évènements émis aux DEUX interfaces : un nom au moins en commun
	seenWeb := map[string]bool{}
	end := time.After(10 * time.Second)
collect:
	for {
		select {
		case n, ok := <-names:
			if !ok {
				break collect
			}
			seenWeb[n] = true
			r.mu.Lock()
			common := false
			for _, e := range r.evs {
				if seenWeb[e["_name"].(string)] {
					common = true
				}
			}
			r.mu.Unlock()
			if common {
				break collect
			}
		case <-end:
			t.Fatalf("aucun évènement commun à la fenêtre et au client web (web : %v)", seenWeb)
		}
	}
}

// Le client web ne tue ni ne remplace le serveur de la fenêtre : toutes les protections de security_lot33 restent
// vraies pour les appels de configuration (chemin hors arbre refusé depuis le web aussi).
func TestDesktopWebCallsKeepDispatchGuards(t *testing.T) {
	sandbox(t, "", "alpha")
	p := freePort(t)
	writeBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	a := startNative(t)
	waitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), webToken(t)
	outside := t.TempDir()
	v := webCall(t, addr, tok, "configApply", map[string]any{"kind": "project-env", "path": outside, "mtime": "", "changes": []any{
		map[string]any{"op": "set", "at": []string{"image"}, "value": "docker.io/library/debian:12"}}})
	if e, _ := v["error"].(string); !strings.Contains(e, "projet inconnu") {
		t.Errorf("configApply hors arbre via le web : %v", v)
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude-launcher")); err == nil {
		t.Error("écriture hors de l'arbre via le web")
	}
	// appel inconnu : erreur propre
	if v := webCall(t, addr, tok, "inconnu", map[string]any{}); v["error"] == nil {
		t.Errorf("appel inconnu accepté : %v", v)
	}
	_ = io.Discard
}
