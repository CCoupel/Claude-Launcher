package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Corrections de l'audit sécurité LOT-4 (#163) : M1 (Host hors boucle locale), M2 (session en
// mémoire à la place du jeton dans le cookie, rotation), refus au client web du réglage desktop_web,
// m3 (erreur d'écriture du jeton), m4 (O_EXCL 0600), m6 (plafond de flux SSE).

func scGet(t *testing.T, base, path, host string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+path, nil)
	if host != "" {
		req.Host = host
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := webClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func scCall(t *testing.T, base string, c *http.Cookie, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := webClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// scBootstrap : page d'amorçage avec le jeton ; rend le cookie de session posé.
func scBootstrap(t *testing.T, base, tok string) *http.Cookie {
	t.Helper()
	resp, _ := scGet(t, base, "/?t="+tok, "")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("amorçage : %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "cl_") {
			return c
		}
	}
	t.Fatal("aucun cookie posé par la page d'amorçage")
	return nil
}

// M1 : toute écoute hors boucle locale — y compris celle de « desktop --web » — applique le contrôle
// de Host (hôte d'écoute, nom de la machine, localhost) et signale le risque.
func TestRunWebListenerOutsideLoopbackChecksHost(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Skip("écoute 0.0.0.0 impossible : ", err)
	}
	w := &webBridge{app: a, token: "tokM1", subs: map[chan []byte]bool{}}
	warn := w.configureListener(ln)
	if !strings.Contains(warn, "hors de la boucle locale") {
		t.Errorf("avertissement absent : %q", warn)
	}
	srv := httptest.NewUnstartedServer(w.handler())
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	port := w.port
	base := "http://127.0.0.1:" + port
	if resp, _ := scGet(t, base, "/?t=tokM1", "evil.example:"+port); resp.StatusCode != 403 {
		t.Errorf("Host étranger sur écoute 0.0.0.0 : %d, attendu 403 (DNS rebinding)", resp.StatusCode)
	}
	if resp, _ := scGet(t, base, "/?t=tokM1", "localhost:"+port); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("Host localhost : %d", resp.StatusCode)
	}
	if h, _ := os.Hostname(); h != "" {
		if resp, _ := scGet(t, base, "/?t=tokM1", h+":"+port); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("Host = nom de la machine : %d", resp.StatusCode)
		}
	}
}

// M2 : le cookie porte un identifiant de session, jamais le jeton ; nom avec le port ; l'ancien
// cookie « cl_token = jeton » n'ouvre plus rien.
func TestWebCookieIsASessionNotTheToken(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokM2tokM2tokM2", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	c := scBootstrap(t, srv.URL, "tokM2tokM2tokM2")
	if c.Value == "tokM2tokM2tokM2" || strings.Contains(c.Value, "tokM2") {
		t.Errorf("le cookie porte le jeton : %+v", c)
	}
	if c.Name != "cl_session_"+port || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || len(c.Value) < 32 {
		t.Errorf("cookie : %+v, attendu cl_session_%s, HttpOnly, SameSite=Strict, identifiant aléatoire", c, port)
	}
	call := `{"method":"refresh","args":{}}`
	if st := scCall(t, srv.URL, c, call); st != 200 {
		t.Errorf("session valide : %d", st)
	}
	if st := scCall(t, srv.URL, &http.Cookie{Name: "cl_token", Value: "tokM2tokM2tokM2"}, call); st != 403 {
		t.Errorf("ancien cookie cl_token=jeton : %d, attendu 403", st)
	}
	if st := scCall(t, srv.URL, &http.Cookie{Name: c.Name, Value: "tokM2tokM2tokM2"}, call); st != 403 {
		t.Errorf("le jeton comme valeur de cookie de session : %d, attendu 403", st)
	}
	// session révoquée (régénération) : refusée
	w.setToken("autreTokenTokenToken")
	if st := scCall(t, srv.URL, c, call); st != 403 {
		t.Errorf("ancienne session après rotation : %d, attendu 403", st)
	}
	if resp, _ := scGet(t, srv.URL, "/?t=tokM2tokM2tokM2", ""); resp.StatusCode != 403 {
		t.Errorf("ancien jeton d'URL après rotation : %d, attendu 403", resp.StatusCode)
	}
}

// M2 : session révoquée à l'arrêt du serveur ; jeton renouvelé quand l'adresse d'écoute change ;
// « régénérer le jeton » réservé à la fenêtre.
func TestWebSessionRevokedOnStopAndTokenRotates(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	p1 := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p1)+"\" }\n")
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 5*time.Second)
	tok1, _ := desktopWebToken()
	base := "http://127.0.0.1:" + itoa(p1)
	c := scBootstrap(t, base, tok1)
	call := `{"method":"refresh","args":{}}`
	if st := scCall(t, base, c, call); st != 200 {
		t.Fatalf("session : %d", st)
	}
	// arrêt puis relance sur la même adresse : l'ancienne session ne revit pas, le jeton reste (même adresse)
	l4WriteBackends(t, "  desktop_web: { listen: off }\n")
	a.applyWebUI()
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p1)+"\" }\n")
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 5*time.Second)
	if st := scCall(t, base, c, call); st != 403 {
		t.Errorf("ancienne session après arrêt/relance : %d, attendu 403", st)
	}
	if tok, _ := desktopWebToken(); tok != tok1 {
		t.Error("jeton renouvelé alors que l'adresse n'a pas changé")
	}
	// adresse changée : nouveau jeton
	p2 := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p2)+"\" }\n")
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 5*time.Second)
	if tok2, _ := desktopWebToken(); tok2 == tok1 {
		t.Error("jeton conservé malgré le changement d'adresse d'écoute")
	}
	// régénérer : depuis la fenêtre seulement ; session révoquée, ancien lien mort
	base2 := "http://127.0.0.1:" + itoa(p2)
	tok2, _ := desktopWebToken()
	c2 := scBootstrap(t, base2, tok2)
	if _, err := a.DispatchFrom(originWeb, "webUIRegen", nil); err == nil {
		t.Error("webUIRegen accepté d'un client web")
	}
	if st := scCall(t, base2, c2, call); st != 200 {
		t.Fatalf("session avant régénération : %d", st)
	}
	if res, err := a.DispatchFrom(originWindow, "webUIRegen", nil); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("régénération : %v %v", res, err)
	}
	if st := scCall(t, base2, c2, call); st != 403 {
		t.Errorf("session après régénération : %d, attendu 403", st)
	}
	if tok3, _ := desktopWebToken(); tok3 == tok2 {
		t.Error("jeton inchangé après régénération")
	}
	if resp, _ := scGet(t, base2, "/?t="+tok2, ""); resp.StatusCode != 403 {
		t.Errorf("ancien lien après régénération : %d", resp.StatusCode)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Décision utilisateur : un client web ne modifie pas le réglage desktop_web ; les autres réglages
// restent permis.
func TestWebClientCannotChangeDesktopWeb(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	y := "defaults:\n  browser_server: { listen: off }\n  desktop_web: { listen: off }\n"
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := func() string { st, _ := os.Stat(core.BackendsPath()); return st.ModTime().Format(time.RFC3339Nano) }
	args := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	refused := func(res any, err error) bool {
		if err != nil {
			return true
		}
		m, _ := res.(map[string]any)
		return m["invalid"] == true // refusé par le garde (pas un simple conflit de date)
	}
	// édition de texte : desktop_web changé → refusé ; autre réglage → permis
	cur, _ := os.ReadFile(core.BackendsPath())
	bad := strings.Replace(string(cur), "listen: off }\n", "listen: \"0.0.0.0:8770\", allow_lan: true }\n", 2)
	res, err := a.DispatchFrom(originWeb, "configSave", args(map[string]any{"kind": "backends.yaml", "content": bad, "mtime": mt()}))
	if !refused(res, err) {
		t.Errorf("configSave de desktop_web depuis le web : %v %v", res, err)
	}
	if now, _ := os.ReadFile(core.BackendsPath()); string(now) != y {
		t.Errorf("backends.yaml modifié par un client web : %s", now)
	}
	// modèle structuré : changement de desktop_web (hub) refusé
	res, err = a.DispatchFrom(originWeb, "configApply", args(map[string]any{"kind": "hub", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"desktop_web", "allow_lan"}, "value": true}}}))
	if !refused(res, err) {
		t.Errorf("configApply desktop_web depuis le web : %v %v", res, err)
	}
	if now, _ := os.ReadFile(core.BackendsPath()); string(now) != y {
		t.Errorf("backends.yaml modifié par configApply web : %s", now)
	}
	// un autre réglage reste permis au client web
	res, err = a.DispatchFrom(originWeb, "configApply", args(map[string]any{"kind": "backends.yaml", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"defaults", "image"}, "value": "docker.io/library/debian:12"}}}))
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Errorf("autre réglage depuis le web : %v %v", res, err)
	}
	// la fenêtre, elle, peut modifier desktop_web
	res, err = a.DispatchFrom(originWindow, "configApply", args(map[string]any{"kind": "hub", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"desktop_web", "listen"}, "value": "8771"}}}))
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Errorf("desktop_web depuis la fenêtre : %v %v", res, err)
	}
}

// m3 : si le jeton ne peut pas être conservé, le serveur démarre avec le jeton tiré et le dit dans webUI.msg ;
// m4 : fichier créé en 0600.
func TestWebTokenWriteErrorIsReported(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	if err := os.MkdirAll(desktopWebTokenPath(), 0o755); err != nil { // un dossier à la place du fichier : écriture impossible
		t.Fatal(err)
	}
	p := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p)+"\" }\n")
	a.applyWebUI()
	s := l4WaitWebUI(t, a, "on", 5*time.Second)
	if m, _ := s["msg"].(string); !strings.Contains(m, "jeton") {
		t.Errorf("webUI = %v, attendu un message sur le jeton non conservé", s)
	}
}

// m6 : flux d'évènements plafonnés.
func TestWebSubscribersAreCapped(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokM6tokM6tokM6", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	c := scBootstrap(t, srv.URL, "tokM6tokM6tokM6")
	var open []*http.Response
	defer func() {
		for _, r := range open {
			r.Body.Close()
		}
	}()
	last := 200
	for i := 0; i <= maxSubscribers; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
		req.AddCookie(c)
		resp, err := webClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		last = resp.StatusCode
		if resp.StatusCode == 200 {
			open = append(open, resp)
		} else {
			resp.Body.Close()
		}
	}
	if last != http.StatusServiceUnavailable {
		t.Errorf("flux n° %d : %d, attendu 503 au-delà du plafond", maxSubscribers+1, last)
	}
}

// webUI.lan / webUI.warn : écoute hors boucle locale confirmée par allow_lan ; l'avertissement n'est pas une erreur.
func TestWebUIStateLanAndWarn(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	// #187 : un port choisi puis relâché peut être repris par un autre processus avant l'écoute : en cas de
	// « address already in use », un autre port est tiré (jamais d'échec pour une course sur le port)
	applyOn := func(listenOf func(p int) string, allow string) map[string]any {
		t.Helper()
		for try := 0; try < 8; try++ {
			l4WriteBackends(t, "  desktop_web: { listen: \""+listenOf(l4FreePort(t))+"\""+allow+" }\n")
			a.applyWebUI()
			end := time.Now().Add(5 * time.Second)
			for time.Now().Before(end) {
				w := l4WebUIOf(t, a)
				if w["state"] == "on" {
					return w
				}
				if w["state"] == "error" && strings.Contains(fmt.Sprint(w["msg"]), "address already in use") {
					break // port repris entre-temps : on en tire un autre
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		t.Fatal("aucune écoute possible après 8 ports")
		return nil
	}
	s := applyOn(func(p int) string { return "0.0.0.0:" + itoa(p) }, ", allow_lan: true")
	if s["lan"] != true || s["warn"] == "" || s["msg"] != "" {
		t.Errorf("webUI = %v, attendu lan, warn renseigné, msg vide", s)
	}
	s = applyOn(func(p int) string { return "127.0.0.1:" + itoa(p) }, "")
	if s["lan"] == true || s["warn"] != "" {
		t.Errorf("boucle locale : %v, attendu lan faux, pas d'avertissement", s)
	}
}
