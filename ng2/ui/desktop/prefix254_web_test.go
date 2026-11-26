package desktop

// LOT-3 addendum 4 (#254) — le launcher derrière un sous-chemin public (`CL_WEB_URL=https://hôte/claude-qualif`, contracts/auth.md §9) :
// base retirée avant le routeur et remise dans toute URL émise, `<base href>` injecté, 308 de `<base>` vers `<base>/`, 404 hors de la
// base, SSE, /files, /signed-out, /too-many, Origin strict, X-Forwarded-Prefix ignoré. Hôte interne : base vide, inchangé.
// RunWeb réel sur un port libre ; adresses fictives ; aucun vrai projet. Écrits avant le code : RED attendu.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

const px254Base = "/claude-qualif"

func px254Env(t *testing.T) {
	t.Helper()
	px172Env(t)
	t.Setenv("CL_WEB_URL", "https://"+px172Ext+px254Base)
}

func px254Start(t *testing.T) *px172 { return px172Start(t, false) }

func admin254(extra map[string]string) map[string]string {
	h := map[string]string{"X-Forwarded-User": "ccoupel"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// Base : <base href> injecté (hôte externe : le préfixe ; hôte interne : « / »), 308 de <base> vers <base>/, 404 hors de la base.
func TestPrefix254BaseAndRouting(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	resp, body := s.do("GET", px254Base+"/", px172Ext, admin254(nil), "")
	if resp == nil || resp.StatusCode != 200 || !strings.Contains(body, `<base href="`+px254Base+`/">`) {
		t.Fatalf("GET %s/ : %v — base href injecté attendu (%.160s)", px254Base, resp, body)
	}
	if n := strings.Count(body, "<base "); n != 1 {
		t.Errorf("%d balises <base>, attendu 1", n)
	}
	if len(resp.Cookies()) != 0 {
		t.Errorf("cookie posé par le launcher : %v", resp.Cookies())
	}
	// <base> sans « / » final : 308 vers <base>/
	r2, _ := s.do("GET", px254Base, px172Ext, admin254(nil), "")
	if r2 == nil || r2.StatusCode != 308 || r2.Header.Get("Location") != px254Base+"/" {
		t.Errorf("GET %s : %v, attendu 308 vers %s/", px254Base, r2, px254Base)
	}
	// hors de la base, sur l'hôte externe : 404 (jamais l'interface, jamais un appel)
	for _, p := range []string{"/", "/index.html", "/app.js", "/api/call", "/api/events", "/files/", "/signed-out", "/too-many", "/emby/", "/claude-qualifier/", px254Base + "x/", "/other" + px254Base + "/"} {
		if r, _ := s.do("GET", p, px172Ext, admin254(nil), ""); r == nil || r.StatusCode != 404 {
			t.Errorf("GET %s sur l'hôte externe hors de la base : %v, attendu 404", p, r)
		}
	}
	// hôte interne : base vide, inchangé
	ri, bi := s.do("GET", "/", px172Int, admin254(nil), "")
	if ri == nil || ri.StatusCode != 200 || !strings.Contains(bi, `<base href="/">`) {
		t.Errorf("hôte interne : %v — <base href=\"/\"> attendu", ri)
	}
	if r, _ := s.do("GET", px254Base+"/", px172Int, admin254(nil), ""); r != nil && r.StatusCode == 200 {
		t.Error("le sous-chemin de l'hôte externe est servi sur l'hôte interne")
	}
	// les ressources sont servies sous la base
	for _, p := range []string{px254Base + "/app.js", px254Base + "/style.css", px254Base + "/index.html"} {
		if r, _ := s.do("GET", p, px172Ext, admin254(nil), ""); r == nil || r.StatusCode != 200 {
			t.Errorf("GET %s : %v, attendu 200", p, r)
		}
	}
}

// Identité exigée sous la base comme ailleurs ; contrôle de l'hôte et de la base ensemble.
func TestPrefix254IdentityStillRequired(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	for _, p := range []string{px254Base + "/", px254Base + "/api/events", px254Base + "/files/", px254Base + "/app.js"} {
		if r, _ := s.do("GET", p, px172Ext, nil, ""); r == nil || r.StatusCode != 401 || r.Header.Get("X-Launcher-Refusal") != "identity" {
			t.Errorf("GET %s sans identité : %v, attendu 401 identity", p, r)
		}
		if r, _ := s.do("GET", p, px172Ext, map[string]string{"X-Forwarded-User": "mallory"}, ""); r == nil || r.StatusCode != 403 || r.Header.Get("X-Launcher-Refusal") != "user" {
			t.Errorf("GET %s login inconnu : %v, attendu 403 user", p, r)
		}
	}
	// admin@local reste refusé sur l'hôte externe, base comprise
	if r, _ := s.doMulti("GET", px254Base+"/", px172Ext, nil, []string{"admin@local"}, ""); r == nil || r.StatusCode != 403 {
		t.Errorf("admin@local sur l'hôte externe : %v, attendu 403", r)
	}
}

func (s *px172) callAt(user, host, path, body string, extra map[string]string) (map[string]any, int) {
	h := map[string]string{"Content-Type": "application/json", "X-Forwarded-User": user, "X-Forwarded-For": "192.168.1.20"}
	for k, v := range extra {
		h[k] = v
	}
	resp, b := s.do("POST", path, host, h, body)
	if resp == nil {
		return nil, 0
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(b), &v)
	return v, resp.StatusCode
}

// Appels sous la base : user.logout et toute URL émise portent la base ; l'hôte interne garde les chemins d'origine.
func TestPrefix254URLsCarryTheBase(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	for _, m := range []string{"init", "clientInfo"} {
		v, st := s.callAt("ccoupel", px172Ext, px254Base+"/api/call", fmt.Sprintf(`{"method":%q,"args":{}}`, m), nil)
		u, _ := d183Find(v, "user").(map[string]any)
		lo, _ := u["logout"].(string)
		if st != 200 || !strings.HasPrefix(lo, px254Base+"/oauth2/sign_out?rd=") || !strings.Contains(lo, "signed-out") || !strings.Contains(lo, px254Base[1:]) {
			t.Errorf("%s : HTTP %d user.logout = %q, attendu %s/oauth2/sign_out?rd=<base>%%2Fsigned-out", m, st, lo, px254Base)
		}
		if strings.Contains(lo, "rd=%2Fsigned-out") && !strings.Contains(lo, px254Base[1:]) {
			t.Errorf("rd sans la base : %q (« Se déconnecter » reviendrait hors du sous-chemin)", lo)
		}
	}
	v, _ := s.callAt("ccoupel", px172Int, "/api/call", `{"method":"clientInfo","args":{}}`, nil)
	if u, _ := d183Find(v, "user").(map[string]any); u == nil || u["logout"] != "/oauth2/sign_out?rd=%2Fsigned-out" {
		t.Errorf("hôte interne : user.logout = %v, attendu /oauth2/sign_out?rd=%%2Fsigned-out (inchangé)", u["logout"])
	}
	// l'ancien lien d'amorçage /?t= : 303 vers <base>/ (jamais vers « / »)
	r, _ := s.do("GET", px254Base+"/?t=abc", px172Ext, admin254(nil), "")
	if r == nil || r.StatusCode != 303 || r.Header.Get("Location") != px254Base+"/" {
		t.Errorf("GET %s/?t=abc : %v, attendu 303 vers %s/", px254Base, r, px254Base)
	}
	ri, _ := s.do("GET", "/?t=abc", px172Int, admin254(nil), "")
	if ri == nil || ri.StatusCode != 303 || ri.Header.Get("Location") != "/" {
		t.Errorf("hôte interne /?t= : %v, attendu 303 vers /", ri)
	}
	// aucune redirection du launcher ne sort de la base
	for _, p := range []string{px254Base, px254Base + "/?t=x", px254Base + "/index.html"} {
		if r, _ := s.do("GET", p, px172Ext, admin254(nil), ""); r != nil && r.Header.Get("Location") != "" && !strings.HasPrefix(r.Header.Get("Location"), px254Base+"/") {
			t.Errorf("GET %s : Location %q hors de la base", p, r.Header.Get("Location"))
		}
	}
}

// Pages publiques sous la base : /signed-out et /too-many sans identité, liens relatifs à la base, aucun chemin absolu.
func TestPrefix254PublicPages(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	for _, page := range []string{"signed-out", "too-many"} {
		resp, body := s.do("GET", px254Base+"/"+page, px172Ext, nil, "")
		if resp == nil || resp.StatusCode != 200 {
			t.Errorf("GET %s/%s sans identité : %v", px254Base, page, resp)
			continue
		}
		if len(resp.Cookies()) != 0 || strings.Contains(strings.ToLower(body), "<script") {
			t.Errorf("%s : cookie ou script", page)
		}
		// liens : « Se reconnecter » mène à la base, jamais à la racine de l'hôte partagé
		for _, bad := range []string{`href="/"`, `href='/'`, `action="/"`, `href="/oauth2`, `src="/`} {
			if strings.Contains(body, bad) {
				t.Errorf("%s : lien absolu %s (sortirait du sous-chemin)", page, bad)
			}
		}
		if page == "signed-out" && !strings.Contains(body, `href="`+px254Base+`/"`) && !strings.Contains(body, `href="./"`) && !strings.Contains(body, `<base href="`+px254Base+`/">`) {
			t.Errorf("signed-out : aucun lien relatif à la base (%.200s)", body)
		}
		// hors base : 404 (ce sont les routes de la base seulement)
		if r, _ := s.do("GET", "/"+page, px172Ext, nil, ""); r == nil || r.StatusCode != 404 {
			t.Errorf("GET /%s sur l'hôte externe : %v, attendu 404", page, r)
		}
	}
	// hôte interne : inchangé
	for _, page := range []string{"signed-out", "too-many"} {
		if r, _ := s.do("GET", "/"+page, px172Int, nil, ""); r == nil || r.StatusCode != 200 {
			t.Errorf("hôte interne /%s : %v", page, r)
		}
	}
}

// SSE : /api/events sous la base ; flux ouvert, un évènement d'accueil ; hors base 404.
func TestPrefix254SSE(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(s.port)+px254Base+"/api/events", nil)
	req.Host = px172Ext
	req.Header.Set("X-Forwarded-User", "ccoupel")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("flux : HTTP %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	sc := bufio.NewScanner(resp.Body)
	got := false
	for sc.Scan() {
		if strings.Contains(sc.Text(), `"hello"`) || strings.HasPrefix(sc.Text(), "data:") {
			got = true
			break
		}
	}
	if !got {
		t.Error("aucun évènement reçu sous la base")
	}
}

// /files sous la base : identité exigée ; la route du launcher, pas une page de l'hôte partagé.
func TestPrefix254Files(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	if r, _ := s.do("GET", px254Base+"/files/", px172Ext, admin254(nil), ""); r == nil || r.StatusCode == 401 || r.StatusCode == 403 || r.StatusCode >= 500 {
		t.Errorf("GET %s/files/ identifié : %v", px254Base, r)
	}
	if r, _ := s.do("GET", "/files/", px172Ext, admin254(nil), ""); r == nil || r.StatusCode != 404 {
		t.Errorf("GET /files/ hors base : %v, attendu 404", r)
	}
	// traversée : jamais hors de la base ni vers un fichier du launcher
	for _, p := range []string{px254Base + "/files/../../etc/passwd", px254Base + "/files/%2e%2e/%2e%2e/etc/passwd", px254Base + "/%2e%2e/api/call", px254Base + "/../api/call"} {
		if r, b := s.do("GET", p, px172Ext, admin254(nil), ""); r != nil && (r.StatusCode == 200 && strings.Contains(b, "root:")) {
			t.Errorf("GET %s : traversée aboutie", p)
		}
	}
}

// Origin et Sec-Fetch-Site : l'origine attendue est celle de l'adresse publique SANS chemin ; un Origin avec chemin, d'un autre hôte
// ou d'une autre origine est refusé ; X-Forwarded-Prefix (jamais lu) ne change ni la base ni le rendu.
func TestPrefix254OriginAndForwardedPrefix(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	body := `{"method":"clientInfo","args":{}}`
	post := func(h map[string]string) int {
		_, st := s.callAt("ccoupel", px172Ext, px254Base+"/api/call", body, h)
		return st
	}
	for name, tc := range map[string]struct {
		h    map[string]string
		want int
	}{
		"origine exacte (sans chemin)":         {map[string]string{"Origin": "https://" + px172Ext}, 200},
		"sans Origin":                          {nil, 200},
		"Origin avec le chemin":                {map[string]string{"Origin": "https://" + px172Ext + px254Base}, 403},
		"Origin d'un autre hôte":               {map[string]string{"Origin": "https://evil.example"}, 403},
		"Origin interne":                       {map[string]string{"Origin": "https://" + px172Int}, 403},
		"Origin http":                          {map[string]string{"Origin": "http://" + px172Ext}, 403},
		"cross-site":                           {map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		"same-site (autre appli du même site)": {map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		"same-origin":                          {map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://" + px172Ext}, 200},
	} {
		if got := post(tc.h); got != tc.want {
			t.Errorf("%s : HTTP %d, attendu %d", name, got, tc.want)
		}
	}
	// X-Forwarded-Prefix : ignoré (le préfixe vient de CL_WEB_URL, jamais d'un en-tête du client)
	ref := func(h map[string]string) string {
		_, b := s.do("GET", px254Base+"/", px172Ext, admin254(h), "")
		return b
	}
	base := ref(nil)
	for _, hostile := range []string{"/evil", "//evil.example", "/../", "/claude-qualif/../emby", "javascript:alert(1)", "\"><script>", strings.Repeat("/a", 400), ""} {
		got := ref(map[string]string{"X-Forwarded-Prefix": hostile})
		if got != base {
			t.Errorf("X-Forwarded-Prefix %.30q : le rendu change (le préfixe doit venir de CL_WEB_URL seulement)", hostile)
		}
		if strings.Contains(got, "evil") || strings.Contains(got, "<script>alert") {
			t.Errorf("X-Forwarded-Prefix %.30q repris dans la page", hostile)
		}
	}
	// ni Host ni X-Forwarded-Host ne déplacent la base
	if _, b := s.do("GET", px254Base+"/", px172Ext, admin254(map[string]string{"X-Forwarded-Host": "evil.example", "X-Forwarded-Proto": "http", "X-Forwarded-Path": "/evil"}), ""); b != base {
		t.Error("X-Forwarded-Host / -Proto / -Path modifient le rendu")
	}
}

// Les RPC de R1, d'audit et d'identité sont inchangées sous la base ; l'audit n'écrit pas la base dans `project` ni dans `src`.
func TestPrefix254R1AndAuditUnchanged(t *testing.T) {
	px254Env(t)
	s := px254Start(t)
	v, st := s.callAt("alice", px172Ext, px254Base+"/api/call", `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`, nil)
	if st != 200 || d183Find(v, "forbidden") != "admin" {
		t.Errorf("R1 sous la base : HTTP %d %v, attendu forbidden:admin", st, v)
	}
	s.callAt("ccoupel", px172Ext, px254Base+"/api/call", `{"method":"refresh","args":{}}`, nil)
	s.stop()
	n := 0
	for _, l := range s.auditLines(t) {
		if l["method"] == "refresh" && l["phase"] != "intent" {
			n++
			if strings.Contains(fmt.Sprint(l), "claude-qualif") {
				t.Errorf("la base figure dans l'audit : %v", l)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d entrées refresh, attendu 1", n)
	}
}

// Refus de démarrer : CL_WEB_URL dont le chemin est invalide (segments ^[a-z0-9-]+, sans « / » final, « // » ni « .. »).
func TestPrefix254StartRefusalInvalidPath(t *testing.T) {
	run := func(url string) error {
		px254Env(t)
		sandbox(t, "", "alpha")
		l4WriteBackends(t, "")
		t.Setenv("CL_WEB_URL", url)
		t.Setenv("CL_HEALTH_ADDR", "")
		done := make(chan error, 1)
		go func() { done <- RunWeb("127.0.0.1:"+strconv.Itoa(l4FreePort(t)), false) }()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("RunWeb n'a pas refusé de démarrer")
			return nil
		}
	}
	for _, bad := range []string{"https://h.example/Claude", "https://h.example/a//b", "https://h.example/a/../b", "https://h.example/a b", "https://h.example/a?x=1", "https://h.example/a#f", "https://h.example/a_b", "https://h.example//"} {
		t.Run(bad, func(t *testing.T) {
			err := run(bad)
			if err == nil || !strings.Contains(err.Error(), "CL_WEB_URL") && !strings.Contains(err.Error(), "adresse publique") {
				t.Errorf("erreur %v, attendu un refus nommant l'adresse publique", err)
			}
		})
	}
}
