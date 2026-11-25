package desktop

// Tests de spécification #172 / #251 / #252 (v7.0.0) — serveur web en mode d'accès « proxy » (contracts/auth.md §1–§5) :
// identité par requête (X-Forwarded-User), 401 / 403, aucune session ni cookie du launcher, contrôles Host / Origin
// inchangés, init.user, règle R1, journal d'audit, port de santé, refus de démarrer. Le serveur est RunWeb (mode serveur
// du pod) sur un port libre de la boucle locale ; adresses publiques fictives ; HOME et cache factices ; aucun vrai
// projet. Écrits depuis le contrat avant le code. Hypothèses : voir _work/handoff/test-writer-lot2.md.
//
// Règle Windows : aucun filepath sur une URL ou un chemin de pod ; chemins de l'hôte seulement pour des fichiers.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

const (
	px172Ext = "launcher.example.org" // adresse publique principale (CL_WEB_URL)
	px172Int = "launcher.internal"    // autre adresse (CL_WEB_ALT_URLS), seule où admin@local est admis
)

type px172 struct {
	port, health int
	cache        string
}

// px172Env : variables du chart pour le mode proxy (§7) ; chaque test peut en changer avant le démarrage.
func px172Env(t *testing.T) {
	t.Helper()
	t.Setenv("CL_WEB_ACCESS", "proxy")
	t.Setenv("CL_WEB_URL", "https://"+px172Ext)
	t.Setenv("CL_WEB_ALT_URLS", "https://"+px172Int)
	t.Setenv("CL_AUTH_ADMINS", "ccoupel")
	t.Setenv("CL_AUTH_USERS", "alice")
	t.Setenv("CL_AUTH_LOCAL_ADMIN_HOSTS", px172Int)
	t.Setenv("CL_DESKTOP_TOKEN", "")
}

// px172Start : RunWeb sur un port libre ; rend quand /signed-out répond (route exemptée d'identité). Arrêt par « quit ».
func px172Start(t *testing.T, withHealth bool) *px172 {
	t.Helper()
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "")
	s := &px172{port: l4FreePort(t), cache: t.TempDir()}
	t.Setenv(core.CacheDirVar, s.cache)
	if withHealth {
		s.health = l4FreePort(t)
		t.Setenv("CL_HEALTH_ADDR", "127.0.0.1:"+strconv.Itoa(s.health))
	} else {
		t.Setenv("CL_HEALTH_ADDR", "")
	}
	// un jeton d'un ancien mode « token » ne doit pas survivre au démarrage en mode proxy
	_ = os.MkdirAll(core.CacheDir(), 0o755)
	_ = os.WriteFile(l4TokenFile(), []byte("ancien-jeton\n"), 0o600)
	oldQuit, oldRestart, oldEnv := quitHook.Get(), restartApp.Get(), restartEnv.Get()
	done := make(chan error, 1)
	go func() { done <- RunWeb("127.0.0.1:"+strconv.Itoa(s.port), false) }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("RunWeb s'est arrêté au démarrage : %v", err)
		default:
		}
		if r, _ := s.do("GET", "/signed-out", px172Ext, nil, ""); r != nil && r.StatusCode == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RunWeb ne répond pas")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(func() {
		s.call("ccoupel", px172Ext, `{"method":"quit","args":{"mode":"l"}}`)
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("RunWeb ne s'arrête pas après quit")
		}
		quitHook.Set(oldQuit)
		restartApp.Set(oldRestart)
		restartEnv.Set(oldEnv)
		time.Sleep(300 * time.Millisecond)
	})
	return s
}

// do : requête sur le serveur ; hdr : en-têtes (plusieurs valeurs : « a|b » n'est PAS découpé — voir doMulti).
func (s *px172) do(method, path, host string, hdr map[string]string, body string) (*http.Response, string) {
	return s.doMulti(method, path, host, hdr, nil, body)
}

func (s *px172) doMulti(method, path, host string, hdr map[string]string, users []string, body string) (*http.Response, string) {
	req, err := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(s.port)+path, strings.NewReader(body))
	if err != nil {
		return nil, ""
	}
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, u := range users {
		req.Header.Add("X-Forwarded-User", u)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// call : POST /api/call avec l'identité user (vide : en-tête absent) ; rend le JSON décodé et le statut.
func (s *px172) call(user, host, body string) (map[string]any, int) {
	h := map[string]string{"Content-Type": "application/json", "X-Forwarded-For": "192.168.1.20"}
	if user != "" {
		h["X-Forwarded-User"] = user
	}
	resp, b := s.do("POST", "/api/call", host, h, body)
	if resp == nil {
		return nil, 0
	}
	var v map[string]any
	_ = json.Unmarshal([]byte(b), &v)
	return v, resp.StatusCode
}

func (s *px172) auditLines(t *testing.T) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.cache, "audit.jsonl"))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) != nil {
			t.Fatalf("ligne d'audit non JSON : %q", l)
		}
		out = append(out, m)
	}
	return out
}

// Identité : 401 (absente, vide, invalide, multiple, autre en-tête) ; 403 (login inconnu) ; admis sinon.
func TestProxy172IdentityMatrix(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for name, tc := range map[string]struct {
		users   []string
		hdr     map[string]string
		host    string
		status  int
		refusal string
		bodyHas string
	}{
		"absent":                        {nil, nil, px172Ext, 401, "identity", "identité absente"},
		"vide":                          {[]string{""}, nil, px172Ext, 401, "identity", ""},
		"invalide":                      {[]string{"a b"}, nil, px172Ext, 401, "identity", ""},
		"plusieurs en-têtes":            {[]string{"ccoupel", "ccoupel"}, nil, px172Ext, 401, "identity", ""},
		"seulement X-Forwarded-Email":   {nil, map[string]string{"X-Forwarded-Email": "ccoupel@example.org"}, px172Ext, 401, "identity", ""},
		"seulement Preferred-Username":  {nil, map[string]string{"X-Forwarded-Preferred-Username": "ccoupel"}, px172Ext, 401, "identity", ""},
		"seulement X-Auth-Request-User": {nil, map[string]string{"X-Auth-Request-User": "ccoupel"}, px172Ext, 401, "identity", ""},
		"login inconnu":                 {[]string{"mallory"}, nil, px172Ext, 403, "user", "non autorisé"},
		"admin":                         {[]string{"ccoupel"}, nil, px172Ext, 200, "", ""},
		"admin casse ignorée":           {[]string{"CCoupel"}, nil, px172Ext, 200, "", ""},
		"user":                          {[]string{"alice"}, nil, px172Ext, 200, "", ""},
		"admin@local hôte interne":      {[]string{"admin@local"}, nil, px172Int, 200, "", ""},
		"admin@local hôte externe":      {[]string{"admin@local"}, nil, px172Ext, 403, "user", ""},
	} {
		resp, body := s.doMulti("GET", "/", tc.host, tc.hdr, tc.users, "")
		if resp == nil {
			t.Errorf("%s : pas de réponse", name)
			continue
		}
		if resp.StatusCode != tc.status || resp.Header.Get("X-Launcher-Refusal") != tc.refusal {
			t.Errorf("%s : %d (refus %q), attendu %d (%q)", name, resp.StatusCode, resp.Header.Get("X-Launcher-Refusal"), tc.status, tc.refusal)
		}
		if tc.bodyHas != "" && !strings.Contains(body, tc.bodyHas) {
			t.Errorf("%s : corps %q, attendu « %s »", name, body, tc.bodyHas)
		}
	}
}

// Toutes les routes exigent l'identité (sauf /signed-out et la santé) : appels, flux, fichiers, ressources.
func TestProxy172EveryRouteNeedsIdentity(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for _, p := range []string{"/api/events", "/files/", "/files/alpha/", "/app.js", "/index.html", "/anything"} {
		resp, _ := s.do("GET", p, px172Ext, nil, "")
		if resp == nil || resp.StatusCode != 401 || resp.Header.Get("X-Launcher-Refusal") != "identity" {
			t.Errorf("GET %s sans identité : %v", p, resp)
		}
	}
	if v, st := s.call("", px172Ext, `{"method":"init","args":{}}`); st != 401 {
		t.Errorf("POST /api/call sans identité : %d %v", st, v)
	}
	// inconnu : 403 partout
	if _, st := s.call("mallory", px172Ext, `{"method":"init","args":{}}`); st != 403 {
		t.Errorf("POST /api/call login inconnu : %d", st)
	}
}

// /signed-out : page statique servie SANS identité, sans cookie, sans appel /api ; en-têtes de sécurité habituels.
func TestProxy172SignedOutPage(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	resp, body := s.do("GET", "/signed-out", px172Ext, nil, "")
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("/signed-out : %v", resp)
	}
	if len(resp.Cookies()) != 0 {
		t.Errorf("cookie posé par /signed-out : %v", resp.Cookies())
	}
	if !strings.Contains(body, "Se reconnecter") || !strings.Contains(strings.ToLower(body), "href=\"/\"") {
		t.Errorf("page /signed-out sans bouton « Se reconnecter » vers / : %.200s", body)
	}
	if strings.Contains(body, "/api/") || strings.Contains(body, "<script") {
		t.Errorf("page /signed-out : ni appel /api ni script attendus")
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("en-têtes de sécurité absents : %v", resp.Header)
	}
	// seulement GET : un POST sur /signed-out reste soumis à l'identité
	if r, _ := s.do("POST", "/signed-out", px172Ext, nil, ""); r == nil || r.StatusCode != 401 {
		t.Errorf("POST /signed-out sans identité : %v", r)
	}
}

// Aucun jeton, aucune session, aucun cookie du launcher ; « /?t=… » → 303 vers « / » ; ancien fichier de jeton supprimé.
func TestProxy172NoTokenNoCookie(t *testing.T) {
	px172Env(t)
	t.Setenv("CL_DESKTOP_TOKEN", "doit-etre-ignore")
	s := px172Start(t, false)
	h := map[string]string{"X-Forwarded-User": "ccoupel"}
	resp, _ := s.do("GET", "/", px172Ext, h, "")
	if resp == nil || resp.StatusCode != 200 || len(resp.Cookies()) != 0 {
		t.Errorf("GET / : %v, aucun cookie attendu", resp)
	}
	resp, _ = s.do("GET", "/?t=doit-etre-ignore", px172Ext, h, "")
	if resp == nil || resp.StatusCode != 303 || resp.Header.Get("Location") != "/" || len(resp.Cookies()) != 0 {
		t.Errorf("GET /?t=… : %v, attendu 303 vers / sans cookie", resp)
	}
	if _, err := os.Stat(l4TokenFile()); err == nil {
		t.Error("desktop-web-token d'un mode « token » précédent conservé")
	}
}

// Contrôles Host, Origin, Sec-Fetch-Site, JSON : inchangés (règles du mode « none »).
func TestProxy172HostOriginJSONChecksUnchanged(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	id := map[string]string{"X-Forwarded-User": "ccoupel", "Content-Type": "application/json"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range id {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	body := `{"method":"clientInfo","args":{}}`
	check := func(name, host string, h map[string]string, want int) {
		t.Helper()
		resp, _ := s.do("POST", "/api/call", host, h, body)
		if resp == nil || resp.StatusCode != want {
			t.Errorf("%s : %v, attendu %d", name, resp, want)
		}
	}
	check("hôte principal, origine exacte", px172Ext, with(map[string]string{"Origin": "https://" + px172Ext}), 200)
	check("hôte alternatif, son origine", px172Int, with(map[string]string{"Origin": "https://" + px172Int}), 200)
	check("hôte principal, origine d'un autre hôte", px172Ext, with(map[string]string{"Origin": "https://" + px172Int}), 403)
	check("origine étrangère", px172Ext, with(map[string]string{"Origin": "https://evil.example"}), 403)
	check("Host étranger", "evil.example", with(nil), 403)
	check("Sec-Fetch-Site cross-site", px172Ext, with(map[string]string{"Sec-Fetch-Site": "cross-site"}), 403)
	check("Sec-Fetch-Site same-site", px172Ext, with(map[string]string{"Sec-Fetch-Site": "same-site"}), 403)
	check("Sec-Fetch-Site same-origin", px172Ext, with(map[string]string{"Sec-Fetch-Site": "same-origin"}), 200)
	if resp, _ := s.do("POST", "/api/call", px172Ext, with(map[string]string{"Content-Type": "text/plain"}), body); resp == nil || resp.StatusCode == 200 {
		t.Errorf("corps non JSON accepté : %v", resp)
	}
}

// init et clientInfo portent user {login, role, logout} en mode proxy ; clientInfo.remote est toujours vrai.
func TestProxy172UserInInitAndClientInfo(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for name, tc := range map[string]struct {
		hdr, host, login, role string
	}{
		"admin":       {"CCoupel", px172Ext, "ccoupel", "admin"},
		"user":        {"alice", px172Ext, "alice", "user"},
		"admin@local": {"admin@local", px172Int, "admin@local", "admin"},
	} {
		for _, m := range []string{"init", "clientInfo"} {
			v, st := s.call(tc.hdr, tc.host, fmt.Sprintf(`{"method":%q,"args":{}}`, m))
			if st != 200 {
				t.Errorf("%s/%s : HTTP %d", name, m, st)
				continue
			}
			u, _ := d183Find(v, "user").(map[string]any)
			if u == nil || u["login"] != tc.login || u["role"] != tc.role || u["logout"] != "/oauth2/sign_out?rd=%2Fsigned-out" {
				t.Errorf("%s/%s : user = %v, attendu %s/%s avec logout relatif", name, m, u, tc.login, tc.role)
			}
			if m == "clientInfo" && d183Find(v, "remote") != true {
				t.Errorf("%s/clientInfo : remote = %v, attendu true (toujours distant en mode proxy)", name, d183Find(v, "remote"))
			}
		}
	}
}

// Règle R1 : un « user » reçoit {ok:false, forbidden:"admin", msg}, rien n'est écrit ; un admin passe ; les lectures
// (backendSecretGet) et les appels non R1 restent permis au « user ».
func TestProxy172R1OverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for _, m := range []string{"configSave", "configApply", "projectSettings", "projectApply", "execApply", "envTrustAnswer",
		"initProposalAnswer", "backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "backendHostScan", "relaunch"} {
		v, st := s.call("alice", px172Ext, fmt.Sprintf(`{"method":%q,"args":{}}`, m))
		if st != 200 || d183Find(v, "forbidden") != "admin" || d183Find(v, "ok") != false || !strings.Contains(fmt.Sprint(d183Find(v, "msg")), "règle R1") {
			t.Errorf("user / %s : HTTP %d %v, attendu {ok:false, forbidden:admin, msg « … règle R1 »}", m, st, v)
		}
		if d183Find(v, "error") != nil {
			t.Errorf("user / %s : champ error présent (refus sans error attendu) : %v", m, v)
		}
	}
	// admin : jamais « forbidden » (ici une erreur d'arguments ou un échec métier, sans effet sur le poste de test)
	for _, m := range []string{"backendSecretDelete", "envTrustAnswer", "initProposalAnswer"} {
		v, st := s.call("ccoupel", px172Ext, fmt.Sprintf(`{"method":%q,"args":{}}`, m))
		if st == 0 || d183Find(v, "forbidden") != nil {
			t.Errorf("admin / %s : refusé par R1 : HTTP %d %v", m, st, v)
		}
	}
	// admin@local est admin
	if v, _ := s.call("admin@local", px172Int, `{"method":"backendSecretDelete","args":{}}`); d183Find(v, "forbidden") != nil {
		t.Errorf("admin@local refusé par R1 : %v", v)
	}
	// lectures et appels non R1 : permis au « user »
	for _, m := range []string{"backendSecretGet", "refresh", "clientInfo"} {
		v, st := s.call("alice", px172Ext, fmt.Sprintf(`{"method":%q,"args":{}}`, m))
		if st == 0 || d183Find(v, "forbidden") != nil {
			t.Errorf("user / %s : refusé par R1 à tort : HTTP %d %v", m, st, v)
		}
	}
}

// Audit : une entrée par appel (user, role, src, method, ok), refus R1 journalisé (ok false, err forbidden), jamais
// d'argument ; méthodes fréquentes non écrites une à une ; flux : une entrée à l'ouverture ; journal 0600 hors coffre.
func TestProxy172AuditOverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	s.call("ccoupel", px172Ext, `{"method":"refresh","args":{"secret":"valeur-secrete-xyz"}}`)
	s.call("alice", px172Ext, `{"method":"configSave","args":{"content":"valeur-secrete-xyz"}}`)
	for i := 0; i < 5; i++ {
		s.call("alice", px172Ext, `{"method":"termInput","args":{"data":"valeur-secrete-xyz"}}`)
	}
	// flux d'évènements : ouvert puis fermé
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(s.port)+"/api/events", nil)
	req.Host = px172Ext
	req.Header.Set("X-Forwarded-User", "ccoupel")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	cancel()
	time.Sleep(300 * time.Millisecond)

	var refresh, forbidden, events int
	for _, l := range s.auditLines(t) {
		for k := range l {
			switch k {
			case "ts", "user", "role", "src", "method", "project", "ok", "err", "kind", "count", "since":
			default:
				t.Errorf("champ %q hors contrat : %v", k, l)
			}
		}
		if strings.Contains(fmt.Sprint(l), "valeur-secrete-xyz") {
			t.Errorf("argument dans le journal : %v", l)
		}
		switch {
		case l["method"] == "refresh" && l["user"] == "ccoupel" && l["role"] == "admin" && l["ok"] == true:
			refresh++
			if !strings.Contains(fmt.Sprint(l["src"]), "192.168.1.20") {
				t.Errorf("src sans l'adresse transmise (X-Forwarded-For en indication) : %v", l["src"])
			}
		case l["method"] == "configSave" && l["user"] == "alice" && l["role"] == "user" && l["ok"] == false && l["err"] == "forbidden":
			forbidden++
		case l["method"] == "events" && l["user"] == "ccoupel":
			events++
		case l["method"] == "termInput" && l["kind"] != "agg":
			t.Errorf("méthode fréquente écrite individuellement : %v", l)
		}
	}
	if refresh != 1 || forbidden != 1 || events != 1 {
		t.Errorf("refresh=%d refus R1=%d events=%d, attendu 1 chacun : %v", refresh, forbidden, events, s.auditLines(t))
	}
	if st, err := os.Stat(filepath.Join(s.cache, "audit.jsonl")); err != nil {
		t.Errorf("audit.jsonl absent du dossier de cache : %v", err)
	} else if st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("audit.jsonl lisible par d'autres que le propriétaire : %v", st.Mode())
	}
}

// Échec d'écriture du journal : R1 refusées (« journal d'audit inaccessible »), les autres appels continuent.
func TestProxy172AuditFailureRefusesR1(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	s.call("ccoupel", px172Ext, `{"method":"refresh","args":{}}`) // le journal existe
	p := filepath.Join(s.cache, "audit.jsonl")
	_ = os.Remove(p)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	v, st := s.call("ccoupel", px172Ext, `{"method":"backendSecretDelete","args":{}}`)
	if st != 200 || d183Find(v, "ok") != false || !strings.Contains(fmt.Sprint(d183Find(v, "msg")), "journal d'audit inaccessible") {
		t.Errorf("R1 avec journal en échec : HTTP %d %v, attendu {ok:false, msg:« journal d'audit inaccessible »}", st, v)
	}
	if v, st := s.call("ccoupel", px172Ext, `{"method":"refresh","args":{}}`); st != 200 || d183Find(v, "msg") == "journal d'audit inaccessible" {
		t.Errorf("appel non R1 avec journal en échec : HTTP %d %v, attendu qu'il continue", st, v)
	}
}

// Port de santé (CL_HEALTH_ADDR) : /healthz et /readyz seulement (GET, HEAD), sans identité ni contrôle de Host, aucune
// information ; toute autre route : 404 sans contenu ; l'écoute principale n'en sert plus.
func TestProxy172HealthPort(t *testing.T) {
	px172Env(t)
	s := px172Start(t, true)
	hc := &http.Client{Timeout: 5 * time.Second}
	get := func(method, path, host string) (int, string) {
		req, _ := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(s.health)+path, nil)
		if host != "" {
			req.Host = host
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatalf("%s %s : %v", method, path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := get("GET", "/readyz", ""); st == 200 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range []string{"/healthz", "/readyz"} {
		for _, m := range []string{"GET", "HEAD"} {
			if st, body := get(m, p, "10.42.0.7:8082"); st != 200 || (m == "GET" && strings.TrimSpace(body) != "ok") {
				t.Errorf("%s %s (Host = IP du pod) : %d %q", m, p, st, body)
			}
		}
	}
	for _, c := range [][2]string{{"GET", "/"}, {"GET", "/api/events"}, {"POST", "/api/call"}, {"GET", "/files/"}, {"GET", "/signed-out"}, {"GET", "/healthz/x"}, {"POST", "/healthz"}} {
		if st, body := get(c[0], c[1], ""); st != 404 || body != "" {
			t.Errorf("%s %s sur le port de santé : %d %q, attendu 404 sans contenu", c[0], c[1], st, body)
		}
	}
	// l'écoute principale ne sert plus les sondes (elles passent par le port dédié) : l'identité y est exigée
	if r, _ := s.do("GET", "/healthz", px172Ext, nil, ""); r == nil || r.StatusCode == 200 {
		t.Errorf("/healthz servi sur l'écoute principale alors que CL_HEALTH_ADDR est posé : %v", r)
	}
}

// Refus de démarrer (§2) : RunWeb rend l'erreur du contrat (code ≠ 0 côté binaire) sans rien servir.
func TestProxy172StartRefusals(t *testing.T) {
	run := func(addr string) error {
		oldQuit, oldRestart, oldEnv := quitHook.Get(), restartApp.Get(), restartEnv.Get()
		defer func() { quitHook.Set(oldQuit); restartApp.Set(oldRestart); restartEnv.Set(oldEnv) }()
		done := make(chan error, 1)
		go func() { done <- RunWeb(addr, false) }()
		select {
		case err := <-done:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("RunWeb n'a pas refusé de démarrer")
			return nil
		}
	}
	loop := func() string { return "127.0.0.1:" + strconv.Itoa(l4FreePort(t)) }
	for name, tc := range map[string]struct {
		set  map[string]string
		addr func() string
		want string
	}{
		"écoute hors boucle locale": {nil, func() string { return "0.0.0.0:" + strconv.Itoa(l4FreePort(t)) }, "écoute en boucle locale exigée"},
		"adresse publique absente":  {map[string]string{"CL_WEB_URL": ""}, loop, "adresse publique https exigée (CL_WEB_URL)"},
		"adresse publique http":     {map[string]string{"CL_WEB_URL": "http://" + px172Ext}, loop, "adresse publique https exigée (CL_WEB_URL)"},
		"aucun administrateur":      {map[string]string{"CL_AUTH_ADMINS": "", "CL_AUTH_LOCAL_ADMIN_HOSTS": ""}, loop, "aucun administrateur (CL_AUTH_ADMINS ou compte local)"},
		"administrateur invalide":   {map[string]string{"CL_AUTH_ADMINS": "ccoupel,pas valide"}, loop, "pas valide"},
		"utilisateur invalide":      {map[string]string{"CL_AUTH_USERS": "alice,-bad-"}, loop, "-bad-"},
		"adresse alternative http":  {map[string]string{"CL_WEB_ALT_URLS": "http://" + px172Int}, loop, "CL_WEB_ALT_URLS"},
	} {
		t.Run(name, func(t *testing.T) {
			px172Env(t)
			sandbox(t, "", "alpha")
			l4WriteBackends(t, "")
			t.Setenv("CL_HEALTH_ADDR", "")
			for k, v := range tc.set {
				t.Setenv(k, v)
			}
			err := run(tc.addr())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("erreur %v, attendu « %s »", err, tc.want)
			}
		})
	}
}

// desktop_web.access: proxy dans backends.yaml, hors cluster : refus « réservé au launcher de cluster » (fenêtre native
// et hub n'ont pas ce mode) ; l'état de l'interface native signale l'erreur.
func TestProxy172NativeWindowRefusesProxy(t *testing.T) {
	sandbox(t, "", "alpha")
	t.Setenv(core.InClusterVar, "0")
	t.Setenv("CL_WEB_ACCESS", "")
	p := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\", access: proxy, url: \"https://%s\" }\n", p, px172Ext))
	a := l4StartNative(t)
	w := l4WaitWebUI(t, a, "error", 8*time.Second)
	if !strings.Contains(fmt.Sprint(w["msg"]), "réservé au launcher de cluster") {
		t.Errorf("webUI = %v, attendu une erreur « accès proxy : réservé au launcher de cluster »", w)
	}
}
