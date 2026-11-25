package desktop

// Tests de spécification #172 / #251 / #252 (v7.0.0) — serveur web en mode d'accès « proxy » (contracts/auth.md §1–§5) :
// identité par requête (X-Forwarded-User), 401 / 403, aucune session ni cookie du launcher, contrôles Host / Origin
// inchangés, init.user, règle R1, journal d'audit, port de santé, refus de démarrer. Le serveur est RunWeb (mode serveur
// du pod) sur un port libre de la boucle locale ; adresses publiques fictives ; HOME et cache factices ; aucun vrai
// projet. Écrits depuis le contrat avant le code. Hypothèses : voir _work/handoff/test-writer-lot2.md.
//
// Règle Windows : aucun filepath sur une URL ou un chemin de pod ; chemins de l'hôte seulement pour des fichiers.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	stdout       *px172Out
	stop         func()
}

// px172Out : sortie standard de RunWeb (copie de l'audit « audit {json} » comprise), lue ligne à ligne.
type px172Out struct {
	mu    sync.Mutex
	lines []string
}

func (o *px172Out) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
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
	if runtime.GOOS == "windows" {
		t.Skip("arrêt par SIGTERM : propre à Linux/macOS (le mode proxy est celui du pod)")
	}
	// arrêt par SIGTERM (« quit » est refusé en mode proxy, tous rôles)
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
	// sortie standard capturée (copie de l'audit, MAJ-4) ; restaurée à l'arrêt
	pr, pw, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = pw
	s.stdout = &px172Out{}
	scanned := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			s.stdout.mu.Lock()
			s.stdout.lines = append(s.stdout.lines, sc.Text())
			s.stdout.mu.Unlock()
		}
		close(scanned)
	}()
	done := make(chan error, 1)
	go func() { done <- RunWeb("127.0.0.1:"+strconv.Itoa(s.port), false) }()
	var once sync.Once
	s.stop = func() {
		once.Do(func() {
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				_ = p.Signal(syscall.SIGTERM)
			}
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("RunWeb ne s'arrête pas sur SIGTERM")
			}
			os.Stdout = oldOut
			_ = pw.Close()
			<-scanned
			quitHook.Set(oldQuit)
			restartApp.Set(oldRestart)
			restartEnv.Set(oldEnv)
			time.Sleep(300 * time.Millisecond)
		})
	}
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
	t.Cleanup(s.stop)
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

// Règle R1 (addendum 2) : serveur et options globales seulement. Un « user » est refusé sur les écritures de coffre, de
// known_hosts, config.conf / env.yaml / hub, relaunch, login ; il est LIBRE sur ses projets et backends ; un admin passe ;
// quit, webUI*, clipboard*, teamAgentSet, secretReveal sont refusés à tous les rôles et le serveur reste debout.
func TestProxy172R1OverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	r1 := []string{
		`{"method":"backendSecretSet","args":{}}`, `{"method":"backendSecretDelete","args":{}}`, `{"method":"backendKnownHostsSet","args":{}}`,
		`{"method":"relaunch","args":{}}`, `{"method":"login","args":{}}`,
		`{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`, `{"method":"configSave","args":{"kind":"env.yaml","content":"x"}}`,
		`{"method":"configApply","args":{"kind":"hub","changes":[]}}`,
		`{"method":"configSave","args":{"kind":"backends.yaml","content":"backends:\n  k: {type: k8s, volumes: {/a: pvc}}\n"}}`,
		`{"method":"inconnue","args":{}}`,
	}
	for _, body := range r1 {
		v, st := s.call("alice", px172Ext, body)
		if st != 200 || d183Find(v, "forbidden") != "admin" || d183Find(v, "ok") != false || !strings.Contains(fmt.Sprint(d183Find(v, "msg")), "règle R1") {
			t.Errorf("user / %.70s : HTTP %d %v, attendu {ok:false, forbidden:admin, msg « … règle R1 »}", body, st, v)
		}
		if d183Find(v, "error") != nil {
			t.Errorf("user / %.70s : champ error présent (refus sans error attendu) : %v", body, v)
		}
		// admin et admin@local : jamais « forbidden » (échec d'arguments ou métier accepté, aucun effet sur le poste de test)
		if strings.Contains(body, `"relaunch"`) || strings.Contains(body, `"login"`) {
			continue // relaunch redémarrerait le serveur ; login ouvre une session claude : refus testé seulement pour un user
		}
		for _, who := range [][2]string{{"ccoupel", px172Ext}, {"admin@local", px172Int}} {
			if v, st := s.call(who[0], who[1], body); st == 0 || (d183Find(v, "forbidden") != nil && !strings.Contains(body, "inconnue")) {
				t.Errorf("%s / %.70s : refusé par R1 : HTTP %d %v", who[0], body, st, v)
			}
		}
	}
	// libre pour un user : projets, backends hors champs R1, lectures
	for _, body := range []string{
		`{"method":"projectSettings","args":{}}`, `{"method":"projectApply","args":{}}`, `{"method":"execApply","args":{}}`,
		`{"method":"envTrustAnswer","args":{}}`, `{"method":"initProposalAnswer","args":{}}`, `{"method":"backendHostScan","args":{}}`,
		`{"method":"backendTest","args":{}}`, `{"method":"backendSecretGet","args":{}}`, `{"method":"refresh","args":{}}`,
		`{"method":"configSave","args":{"kind":"backends.yaml","content":"defaults:\n  browser_server: { listen: off }\nbackends:\n  s: {type: ssh, host: h, shared: [/a]}\n"}}`, // rubrique globale du bac à sable conservée
		`{"method":"configSave","args":{"kind":"project-env","path":"/inexistant","content":"x"}}`, `{"method":"clientInfo","args":{}}`,
	} {
		if v, st := s.call("alice", px172Ext, body); st != 200 || d183Find(v, "forbidden") != nil {
			t.Errorf("user / %.80s : HTTP %d %v, attendu libre (pas de refus R1)", body, st, v)
		}
	}
	// refusés à tous les rôles en mode proxy ; le serveur reste debout
	for _, m := range []string{"quit", "webUIOpen", "webUIRegen", "clipboardRead", "clipboardWrite", "teamAgentSet", "secretReveal"} {
		for _, who := range [][2]string{{"ccoupel", px172Ext}, {"alice", px172Ext}, {"admin@local", px172Int}} {
			v, st := s.call(who[0], who[1], fmt.Sprintf(`{"method":%q,"args":{}}`, m))
			if st == 200 && d183Find(v, "ok") == true && d183Find(v, "error") == nil {
				t.Errorf("%s / %s accepté en mode proxy : %v", who[0], m, v)
			}
		}
	}
	if r, _ := s.do("GET", "/signed-out", px172Ext, nil, ""); r == nil || r.StatusCode != 200 {
		t.Error("le serveur ne répond plus après quit : refus attendu en mode proxy")
	}
}

// minuteStart : attend, si besoin, le début d'une minute (les compteurs d'audit sont par minute).
func minuteStart() {
	if d := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute)); d < 5*time.Second {
		time.Sleep(d + 200*time.Millisecond)
	}
}

// Audit (§5) : une entrée par appel, refus R1 journalisé (ok false, err forbidden, target = identifiant court, jamais une valeur),
// aucun argument, copie sur la sortie standard préfixée « audit », méthodes fréquentes agrégées, flux : une entrée.
func TestProxy172AuditOverHTTP(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	minuteStart()
	s.call("ccoupel", px172Ext, `{"method":"refresh","args":{"secret":"valeur-secrete-xyz"}}`)
	s.call("alice", px172Ext, `{"method":"configSave","args":{"kind":"backends.yaml","content":"backends:\n  k: {type: k8s, volumes: {/valeur-secrete-xyz: pvc}}\n"}}`)
	s.call("alice", px172Ext, `{"method":"backendSecretSet","args":{"backend":"b1","value":"valeur-secrete-xyz"}}`)
	s.call("ccoupel", px172Ext, `{"method":"configSave","args":{"kind":"config.conf","content":"valeur-secrete-xyz"}}`)
	for i := 0; i < 5; i++ {
		s.call("alice", px172Ext, `{"method":"termInput","args":{"data":"valeur-secrete-xyz"}}`)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(s.port)+"/api/events", nil)
	req.Host = px172Ext
	req.Header.Set("X-Forwarded-User", "ccoupel")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
	cancel()
	time.Sleep(300 * time.Millisecond)
	s.stop() // vide les agrégats
	lines := s.auditLines(t)

	var refresh, forb, cfgOK, events, aggInput int
	for _, l := range lines {
		for k := range l {
			switch k {
			case "ts", "user", "role", "src", "method", "project", "target", "ok", "err", "kind", "count", "since", "phase":
			default:
				t.Errorf("champ %q hors contrat : %v", k, l)
			}
		}
		if strings.Contains(fmt.Sprint(l), "valeur-secrete-xyz") {
			t.Errorf("valeur dans le journal : %v", l)
		}
		if l["phase"] == "intent" { // ligne d'intention d'une méthode R1, écrite avant l'exécution (correctif cycle 1)
			continue
		}
		switch {
		case l["method"] == "refresh" && l["user"] == "ccoupel" && l["role"] == "admin" && l["ok"] == true:
			refresh++
			if _, has := l["target"]; has {
				t.Errorf("target sur un appel non R1 : %v", l)
			}
			if !strings.Contains(fmt.Sprint(l["src"]), "192.168.1.20") {
				t.Errorf("src sans l'adresse transmise (X-Forwarded-For en indication) : %v", l["src"])
			}
		case l["method"] == "configSave" && l["user"] == "alice" && l["ok"] == false && l["err"] == "forbidden":
			forb++
			if l["target"] != "backends.yaml" || l["role"] != "user" {
				t.Errorf("refus R1 de backends.yaml : target %v role %v", l["target"], l["role"])
			}
		case l["method"] == "backendSecretSet" && l["user"] == "alice" && l["ok"] == false && l["err"] == "forbidden":
			forb++
			if t, has := l["target"]; has && t != "b1" {
				// le nom du backend est admis ; aucune valeur
				_ = t
			}
		case l["method"] == "configSave" && l["user"] == "ccoupel":
			cfgOK++
			if l["target"] != "config.conf" {
				t.Errorf("configSave config.conf : target %v", l["target"])
			}
		case l["method"] == "events" && l["user"] == "ccoupel":
			events++
		case l["method"] == "termInput" && l["kind"] == "agg":
			aggInput++
			if l["count"] != float64(5) {
				t.Errorf("agrégat termInput : %v, attendu count 5", l)
			}
		case l["method"] == "termInput":
			t.Errorf("méthode fréquente écrite individuellement : %v", l)
		}
	}
	if refresh != 1 || forb != 2 || cfgOK != 1 || events != 1 || aggInput != 1 {
		t.Errorf("refresh=%d refus R1=%d configSave admin=%d events=%d agrégat termInput=%d, attendu 1, 2, 1, 1, 1 : %v", refresh, forb, cfgOK, events, aggInput, lines)
	}
	if st, err := os.Stat(filepath.Join(s.cache, "audit.jsonl")); err != nil {
		t.Errorf("audit.jsonl absent du dossier de cache : %v", err)
	} else if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("audit.jsonl lisible par d'autres que le propriétaire : %v", st.Mode())
	}
	// copie sur la sortie standard : chaque ligne du fichier, préfixée « audit »
	out := map[string]bool{}
	for _, l := range s.stdout.all() {
		if strings.HasPrefix(l, "audit ") {
			out[strings.TrimPrefix(l, "audit ")] = true
		}
	}
	b, _ := os.ReadFile(filepath.Join(s.cache, "audit.jsonl"))
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !out[l] {
			t.Errorf("ligne du journal absente de la sortie standard (préfixe « audit ») : %s", l)
		}
	}
	if strings.Contains(strings.Join(s.stdout.all(), "\n"), "valeur-secrete-xyz") {
		t.Error("valeur dans la sortie standard")
	}
}

// Au-delà de 60 appels par minute et par utilisateur : agrégés ; les appels R1 ne le sont jamais.
func TestProxy172AuditBurstAggregated(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	minuteStart()
	for i := 0; i < 70; i++ {
		s.call("alice", px172Ext, `{"method":"refresh","args":{}}`)
	}
	for i := 0; i < 70; i++ {
		s.call("ccoupel", px172Ext, `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`)
	}
	s.stop()
	var indiv, aggCount, r1 int
	for _, l := range s.auditLines(t) {
		switch {
		case l["method"] == "refresh" && l["user"] == "alice" && l["kind"] == "agg":
			aggCount += int(l["count"].(float64))
		case l["method"] == "refresh" && l["user"] == "alice":
			indiv++
		case l["method"] == "configSave" && l["user"] == "ccoupel" && l["kind"] != "agg" && l["phase"] != "intent":
			r1++
		}
	}
	if indiv != 60 || aggCount != 10 {
		t.Errorf("refresh : %d entrées individuelles et %d agrégées, attendu 60 et 10", indiv, aggCount)
	}
	if r1 != 70 {
		t.Errorf("configSave (R1) : %d entrées, attendu 70 (jamais agrégé)", r1)
	}
}

// Refus d'identité : agrégat « deny » par minute (login tenté tronqué à 64, code, count).
func TestProxy172AuditDeny(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	minuteStart()
	for i := 0; i < 4; i++ {
		s.do("GET", "/", px172Ext, map[string]string{"X-Forwarded-User": "mallory"}, "")
	}
	for i := 0; i < 2; i++ {
		s.do("GET", "/", px172Ext, nil, "")
	}
	s.do("GET", "/", px172Ext, map[string]string{"X-Forwarded-User": strings.Repeat("x y", 60)}, "")
	s.stop()
	by := map[string]map[string]any{}
	for _, l := range s.auditLines(t) {
		if l["kind"] == "deny" {
			login, _ := l["login"].(string)
			by[login+"/"+fmt.Sprint(l["code"])] = l
			if len([]rune(login)) > 64 {
				t.Errorf("login de %d caractères, attendu tronqué à 64", len([]rune(login)))
			}
		}
	}
	if d := by["mallory/403"]; d == nil || d["count"] != float64(4) {
		t.Errorf("mallory/403 = %v, attendu count 4 : %v", d, by)
	}
	if d := by["/401"]; d == nil || d["count"] != float64(2) {
		t.Errorf("identité absente = %v, attendu count 2 : %v", d, by)
	}
}

// src : adresse de la connexion + X-Forwarded-For tronqué à 256 octets, sans caractère de contrôle ; une ligne reste une ligne.
func TestProxy172AuditSrcTruncatedAndClean(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	hostile := strings.Repeat("a", 1000) + "\t\"},{\"ok\":true" // un en-tête HTTP ne porte pas de caractère de contrôle autre que la tabulation
	s.do("POST", "/api/call", px172Ext, map[string]string{"X-Forwarded-User": "ccoupel", "Content-Type": "application/json", "X-Forwarded-For": hostile}, `{"method":"configSave","args":{"kind":"config.conf","content":"x"}}`)
	s.stop()
	n := 0
	for _, l := range s.auditLines(t) {
		if l["method"] != "configSave" || l["phase"] == "intent" {
			continue
		}
		n++
		src := fmt.Sprint(l["src"])
		if i := strings.Index(src, "X-Forwarded-For: "); i < 0 || len(strings.TrimSuffix(src[i+len("X-Forwarded-For: "):], ")")) > 256 {
			t.Errorf("src = %.120q… (%d octets), attendu X-Forwarded-For tronqué à 256 octets", src, len(src))
		}
		if strings.ContainsAny(src, "\t\x1b\x00\r\n") {
			t.Errorf("caractère de contrôle dans src : %q", src)
		}
		if l["ok"] == true && l["role"] != "admin" {
			t.Errorf("entrée forgée : %v", l)
		}
	}
	if n != 1 {
		t.Errorf("%d entrées configSave, attendu 1", n)
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
		"backend host autorisé":     {map[string]string{"CL_ALLOW_HOST_BACKEND": "1"}, loop, "backend host interdit derrière le proxy d'authentification"},
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

// Source : seule la boucle locale (le sidecar du pod) est un client du launcher ; toute autre adresse est refusée à CHAQUE
// requête (403, X-Launcher-Refusal: source), même avec une identité valide.
func TestProxy172NonLoopbackSourceRefused(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	pub, err := core.ParsePublicURL("https://" + px172Ext)
	if err != nil {
		t.Fatal(err)
	}
	conf, err := core.ParseAuthConf("ccoupel", "", "")
	if err != nil {
		t.Fatal(err)
	}
	w := &webBridge{app: a, access: core.AccessProxy, auth: conf, public: &pub, strictHost: true, subs: map[chan []byte]bool{}, audit: core.NewAudit(t.TempDir())}
	h := w.handler()
	for remote, want := range map[string]int{
		"127.0.0.1:41000": 200, "[::1]:41000": 200, "10.42.0.7:41000": 403, "192.168.1.20:5": 403, "[2001:db8::1]:80": 403,
		"0.0.0.0:80": 403, "8.8.8.8:1": 403, "garbage": 403, "": 403,
	} {
		for i := 0; i < 2; i++ { // à chaque requête
			req := httptest.NewRequest("GET", "/", nil)
			req.Host, req.RemoteAddr = px172Ext, remote
			req.Header.Set("X-Forwarded-User", "ccoupel")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			// toutes les routes sauf santé et /signed-out (contrat §2) : source contrôlée à chaque requête, avant l'identité
			if rec.Code != want || (want == 403 && rec.Header().Get("X-Launcher-Refusal") != "source") {
				t.Errorf("RemoteAddr %q : %d (refus %q), attendu %d source", remote, rec.Code, rec.Header().Get("X-Launcher-Refusal"), want)
			}
		}
	}
	req := httptest.NewRequest("POST", "/api/call", strings.NewReader(`{"method":"clientInfo","args":{}}`))
	req.Host, req.RemoteAddr = px172Ext, "10.42.0.7:41000"
	req.Header.Set("X-Forwarded-User", "ccoupel")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || rec.Header().Get("X-Launcher-Refusal") != "source" {
		t.Errorf("appel depuis une autre source : %d (%q), attendu 403 source", rec.Code, rec.Header().Get("X-Launcher-Refusal"))
	}
}

// /too-many : page statique « Trop de tentatives », sans identité, sans cookie ni script ; /signed-out idem ; seulement GET et HEAD.
func TestProxy172TooManyPage(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for _, m := range []string{"GET", "HEAD"} {
		resp, body := s.do(m, "/too-many", px172Ext, nil, "")
		if resp == nil || resp.StatusCode != 200 || len(resp.Cookies()) != 0 {
			t.Fatalf("%s /too-many : %v", m, resp)
		}
		if m == "GET" {
			if !strings.Contains(body, "Trop de tentatives") || strings.Contains(strings.ToLower(body), "<script") || strings.Contains(body, "/api/") {
				t.Errorf("page /too-many : %.200s", body)
			}
			if resp.Header.Get("X-Frame-Options") != "DENY" {
				t.Errorf("en-têtes de sécurité absents : %v", resp.Header)
			}
		}
	}
	if r, _ := s.do("POST", "/too-many", px172Ext, nil, ""); r == nil || r.StatusCode != 401 {
		t.Errorf("POST /too-many sans identité : %v", r)
	}
}

// user.kind (github | local) et user.session {duration, refresh} d'après CL_AUTH_SESSION_DURATION / _REFRESH.
func TestProxy172UserKindAndSession(t *testing.T) {
	px172Env(t)
	t.Setenv("CL_AUTH_SESSION_DURATION", "8h")
	t.Setenv("CL_AUTH_SESSION_REFRESH", "1h")
	s := px172Start(t, false)
	for name, c := range map[string]struct{ user, host, kind string }{
		"github": {"ccoupel", px172Ext, "github"}, "user": {"alice", px172Ext, "github"}, "local": {"admin@local", px172Int, "local"},
	} {
		for _, m := range []string{"init", "clientInfo"} {
			v, st := s.call(c.user, c.host, fmt.Sprintf(`{"method":%q,"args":{}}`, m))
			u, _ := d183Find(v, "user").(map[string]any)
			if st != 200 || u == nil || u["kind"] != c.kind {
				t.Errorf("%s/%s : HTTP %d user = %v, attendu kind %s", name, m, st, u, c.kind)
				continue
			}
			if sess, _ := u["session"].(map[string]any); sess == nil || sess["duration"] != "8h" || sess["refresh"] != "1h" {
				t.Errorf("%s/%s : user.session = %v, attendu {duration: 8h, refresh: 1h}", name, m, u["session"])
			}
		}
	}
}

// Hôtes : minuscules, point final retiré, port retiré, égalité stricte (m2) — appliqué à Host et à la liste du compte local.
func TestProxy172HostNormalisation(t *testing.T) {
	px172Env(t)
	s := px172Start(t, false)
	for host, want := range map[string]int{
		"LAUNCHER.INTERNAL": 200, "launcher.internal.": 200, "Launcher.Internal.": 200,
		"launcher.internal.evil.com": 403, "evil.launcher.internal": 403, "launcher.internal..": 403,
	} {
		resp, _ := s.doMulti("GET", "/", host, nil, []string{"admin@local"}, "")
		if resp == nil || resp.StatusCode != want {
			t.Errorf("admin@local, Host %q : %v, attendu %d", host, resp, want)
		}
	}
	// l'hôte externe, quelle que soit la forme, n'admet jamais le compte local
	for _, host := range []string{px172Ext, strings.ToUpper(px172Ext), px172Ext + ".", px172Ext + ":443"} {
		if resp, _ := s.doMulti("GET", "/", host, nil, []string{"admin@local"}, ""); resp == nil || resp.StatusCode == 200 {
			t.Errorf("admin@local, Host %q : accepté sur l'hôte externe", host)
		}
	}
}
