package desktop

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
)

// ── Mode web : la même interface dans le navigateur ───────────────────────
//
// « claude-launcher-ng2 desktop --web » sert l'interface (mêmes fichiers que la
// fenêtre Wails) sur 127.0.0.1, protégée par un jeton aléatoire passé dans
// l'URL puis gardé en cookie. Les appels arrivent en POST /api/call, les
// événements partent en flux SSE (/api/events). Sert aux tests (navigateur
// sans tête, Linux sans WebKitGTK) et d'interface de repli sous Linux quand le
// binaire n'est pas compilé avec Wails.

type webBridge struct {
	app   *App
	token string
	// strictHost : l'écoute est sur la boucle locale — l'en-tête Host doit alors
	// désigner 127.0.0.1, localhost ou [::1] (parade au DNS rebinding) ; sinon
	// (adresse explicite hors boucle locale) tous les noms sont admis.
	strictHost bool
	port       string // port d'écoute, "" : inconnu (tests)
	// hosts : noms admis dans l'en-tête Host (serveur de la fenêtre native, #163) ; nil : règle
	// strictHost du mode --web
	hosts map[string]bool

	mu       sync.Mutex           // jeton et sessions (authentification, appels)
	sessions map[string]time.Time // identifiants de session en mémoire → dernier usage (le cookie porte l'un d'eux, jamais le jeton)

	subMu   sync.Mutex // abonnés SSE : jamais tenu en même temps que mu, jamais pendant une attente
	subs    map[chan []byte]bool
	subSess map[chan []byte]string // session de chaque abonné : fermé avec elle (expiration, éviction, révocation)
	active  atomic.Int32           // gestionnaires de flux vivants (jusqu'à leur vraie sortie, même retirés de subs)
}

const (
	maxSessions = 64 // au-delà, la plus ancienne est évincée
)

// sessionIdle : durée d'inactivité au-delà de laquelle une session expire (variable : tests).
var sessionIdle = 8 * time.Hour

// maxSubscribers : plafond de flux d'évènements ouverts (un navigateur en ouvre un) ; variable : tests.
var maxSubscribers = 32

// sseWriteTimeout : échéance de chaque envoi d'évènement ; au-delà (connexion figée) le flux est fermé.
var sseWriteTimeout = 10 * time.Second

// setToken : nouveau jeton ; sessions et flux ouverts révoqués.
func (w *webBridge) setToken(t string) {
	w.mu.Lock()
	w.token = t
	w.mu.Unlock()
	w.revokeAll()
}

func (w *webBridge) tokenNow() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.token
}

// revokeAll : sessions oubliées, flux d'évènements fermés (les navigateurs doivent se ré-amorcer).
func (w *webBridge) revokeAll() {
	w.mu.Lock()
	w.sessions = nil
	w.mu.Unlock()
	w.closeAll()
}

// cookieName : « cl_session_<port> » — un cookie ne distingue pas les ports d'un même hôte, le
// nom évite les collisions entre deux serveurs de l'interface de la machine.
func (w *webBridge) cookieName(r *http.Request) string {
	port := w.port
	if port == "" {
		if _, p, err := net.SplitHostPort(r.Host); err == nil {
			port = p
		}
	}
	if port == "" {
		return "cl_session"
	}
	return "cl_session_" + port
}

func (w *webBridge) newSession() string {
	id := newToken() + newToken()
	now := time.Now()
	var gone []string
	w.mu.Lock()
	if w.sessions == nil {
		w.sessions = map[string]time.Time{}
	}
	for k, at := range w.sessions { // expirées d'abord
		if now.Sub(at) > sessionIdle {
			delete(w.sessions, k)
			gone = append(gone, k)
		}
	}
	for len(w.sessions) >= maxSessions { // puis la plus ancienne
		oldest, oldAt := "", now
		for k, at := range w.sessions {
			if oldest == "" || at.Before(oldAt) {
				oldest, oldAt = k, at
			}
		}
		delete(w.sessions, oldest)
		gone = append(gone, oldest)
	}
	w.sessions[id] = now
	w.mu.Unlock()
	w.dropSubs(gone...) // les flux des sessions perdues sont fermés (verrous pris l'un après l'autre)
	return id
}

// sessionAlive : la session existe et n'a pas expiré — sans la prolonger (un flux ouvert n'est pas une activité).
func (w *webBridge) sessionAlive(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	at, ok := w.sessions[id]
	return ok && time.Since(at) <= sessionIdle
}

// dropSubs : ferme les flux rattachés aux sessions ids.
func (w *webBridge) dropSubs(ids ...string) {
	if len(ids) == 0 {
		return
	}
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	w.subMu.Lock()
	defer w.subMu.Unlock()
	for ch, id := range w.subSess {
		if gone[id] && w.subs[ch] {
			delete(w.subs, ch)
			delete(w.subSess, ch)
			close(ch)
		}
	}
}

// checkEvery : période de contrôle de la session d'un flux ouvert (courte si l'inactivité tolérée l'est).
func checkEvery() time.Duration {
	d := sessionIdle / 4
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	if d < 20*time.Millisecond {
		d = 20 * time.Millisecond
	}
	return d
}

// validSession : la session existe et n'a pas expiré ; chaque usage la prolonge.
func (w *webBridge) validSession(id string) bool {
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	at, ok := w.sessions[id]
	if !ok {
		return false
	}
	if now.Sub(at) > sessionIdle {
		delete(w.sessions, id)
		return false
	}
	w.sessions[id] = now
	return true
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// emit : évènement à chaque abonné, sans attente — un client dont le tampon est plein est abandonné
// (il se reconnecte) : ni l'émission, ni l'authentification, ni les autres abonnés n'attendent un client lent.
func (w *webBridge) emit(name string, data any) {
	b, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	w.subMu.Lock()
	defer w.subMu.Unlock()
	for ch := range w.subs {
		select {
		case ch <- b:
		default:
			delete(w.subs, ch)
			delete(w.subSess, ch)
			close(ch)
		}
	}
}

// authorized : cookie, ou — pour la page d'amorçage seulement (allowQuery) —
// jeton de l'URL. Les appels (/api/call, /api/events) n'acceptent que le cookie :
// le jeton d'une URL traîne dans l'historique, les journaux et l'en-tête Referer.
func (w *webBridge) authorized(r *http.Request, allowQuery bool) bool {
	if c, err := r.Cookie(w.cookieName(r)); err == nil && c.Value != "" && w.validSession(c.Value) {
		return true
	}
	return allowQuery && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(w.tokenNow())) == 1
}

// sameOrigin : refuse un appel dont l'en-tête Origin, s'il est présent, n'est pas
// celui du serveur (même schéma, hôte et port). Une page d'un autre port de la
// machine — serveur de maquettes, serveur de développement d'un projet — est du
// même « site » : SameSite=Strict ne la retient pas.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || strings.EqualFold(o, "http://"+r.Host)
}

// jsonRequest : le corps est annoncé en JSON. Une requête « simple » (text/plain,
// formulaire) n'a pas de pré-requête CORS : elle part sans contrôle du navigateur.
func jsonRequest(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// hostOK : l'en-tête Host désigne bien la boucle locale (et le port d'écoute).
func (w *webBridge) hostOK(r *http.Request) bool {
	if w.hosts != nil {
		return w.hostAllowed(r)
	}
	if !w.strictHost {
		return true
	}
	h, port, err := net.SplitHostPort(r.Host)
	if err != nil { // sans port : « localhost »
		h, port = strings.Trim(r.Host, "[]"), ""
	}
	switch strings.ToLower(h) {
	case "127.0.0.1", "localhost", "::1":
	default:
		return false
	}
	return port == "" || w.port == "" || port == w.port
}

// secured : contrôle de l'en-tête Host et en-têtes de sécurité de toutes les réponses.
func (w *webBridge) secured(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !w.hostOK(r) {
			http.Error(rw, "hôte refusé", http.StatusForbidden)
			return
		}
		h := rw.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(rw, r)
	})
}

func (w *webBridge) handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(frontendFS, "frontend")
	files := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if !w.authorized(r, true) {
				http.Error(rw, "jeton manquant : ouvrir l'URL affichée au lancement", http.StatusForbidden)
				return
			}
			// le jeton de l'URL est échangé contre un identifiant de session aléatoire, en mémoire,
			// révoqué à l'arrêt du serveur ou à la régénération du jeton : le cookie ne porte jamais le jeton
			http.SetCookie(rw, &http.Cookie{Name: w.cookieName(r), Value: w.newSession(), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}
		rw.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(rw, r)
	})
	mux.HandleFunc("/api/call", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !w.authorized(r, false) || !sameOrigin(r) {
			http.Error(rw, "refusé", http.StatusForbidden)
			return
		}
		if !jsonRequest(r) {
			http.Error(rw, "Content-Type: application/json attendu", http.StatusUnsupportedMediaType)
			return
		}
		var req struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := w.app.DispatchFrom(originWeb, req.Method, req.Args)
		out := map[string]any{"result": res}
		if err != nil {
			out["error"] = err.Error()
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(out)
	})
	mux.HandleFunc("/api/events", func(rw http.ResponseWriter, r *http.Request) {
		if !w.authorized(r, false) || !sameOrigin(r) {
			http.Error(rw, "refusé", http.StatusForbidden)
			return
		}
		rc := http.NewResponseController(rw)
		if _, ok := rw.(http.Flusher); !ok {
			http.Error(rw, "flux impossible", http.StatusInternalServerError)
			return
		}
		// plafond : un gestionnaire compte jusqu'à sa vraie sortie, même si emit l'a retiré des abonnés
		if int(w.active.Add(1)) > maxSubscribers {
			w.active.Add(-1)
			http.Error(rw, "trop de flux d'évènements ouverts", http.StatusServiceUnavailable)
			return
		}
		defer w.active.Add(-1)
		sid := ""
		if c, err := r.Cookie(w.cookieName(r)); err == nil {
			sid = c.Value
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-store")
		ch := make(chan []byte, 4096)
		w.subMu.Lock()
		if w.subSess == nil {
			w.subSess = map[chan []byte]string{}
		}
		w.subs[ch], w.subSess[ch] = true, sid
		w.subMu.Unlock()
		defer func() {
			w.subMu.Lock()
			if w.subs[ch] {
				delete(w.subs, ch)
				delete(w.subSess, ch)
				close(ch)
			}
			w.subMu.Unlock()
		}()
		// chaque envoi a son échéance : une connexion figée fait échouer l'écriture au lieu de bloquer le gestionnaire
		send := func(format string, args ...any) bool {
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			if _, err := fmt.Fprintf(rw, format, args...); err != nil {
				return false
			}
			return rc.Flush() == nil
		}
		if !send(": ok\n\n") {
			return
		}
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		check := time.NewTicker(checkEvery())
		defer check.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-check.C:
				if !w.sessionAlive(sid) { // session expirée, évincée ou révoquée : flux fermé
					return
				}
			case <-ping.C:
				if !send(": ping\n\n") {
					return
				}
			case b, ok := <-ch:
				if !ok {
					return
				}
				if !send("data: %s\n\n", b) {
					return
				}
			}
		}
	})
	return w.secured(mux)
}

// configureListener : port, contrôle de Host selon l'écoute. Boucle locale : 127.0.0.1, localhost,
// [::1] ; toute autre écoute : les noms de desktop_web (hôte d'écoute, nom de la machine, localhost)
// et un avertissement à afficher (« hors boucle locale »).
func (w *webBridge) configureListener(ln net.Listener) (warn string) {
	ta, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return ""
	}
	w.strictHost, w.port = ta.IP.IsLoopback(), strconv.Itoa(ta.Port)
	if !w.strictHost {
		w.hosts = core.DesktopWebConf{Listen: ln.Addr().String()}.AllowedHosts(core.HubHost())
		return core.LANWarning(ln.Addr().String())
	}
	return ""
}

// newHTTPServer : serveur avec ses délais (en-têtes, inactivité) et la taille des en-têtes bornés.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 16}
}

// RunWeb : interface servie sur addr (« 127.0.0.1:0 » : port libre) ; open :
// ouvrir le navigateur par défaut. Bloque jusqu'à la sortie.
func RunWeb(addr string, open bool) error {
	w := &webBridge{token: newToken(), subs: map[chan []byte]bool{}}
	if t := os.Getenv("CL_DESKTOP_TOKEN"); t != "" { // tests
		w.token = t
	}
	a := NewApp(w.emit)
	a.web = true // le presse-papiers de cette machine n'est jamais exposé à un client web
	w.app = a
	// relance après une mise à jour : l'ancien processus vient de libérer le port
	ln, err := listenRetry(addr, 10*time.Second)
	if err != nil {
		return err
	}
	warn := w.configureListener(ln)
	if warn != "" {
		fmt.Println("⚠ " + warn)
	}
	// mode serveur : l'interface est le processus lui-même, toujours « on » ; le commutateur desktop_web.enabled
	// ne s'y applique pas (#183)
	a.webui.mu.Lock()
	a.webui.state = WebUIState{State: "on", Listen: ln.Addr().String(), Lan: !w.strictHost, Warn: warn, Enabled: true, Server: true}
	a.webui.mu.Unlock()
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), w.tokenNow())
	fmt.Println("Claude Launcher (interface web) :", url)
	fmt.Println("Ctrl+C pour arrêter (les sessions des projets continuent dans tmux).")
	srv := newHTTPServer(w.handler())
	done := make(chan struct{})
	quitHook = func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = srv.Close()
		}()
	}
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	// mise à jour installée : même adresse et même jeton pour le nouveau
	// binaire, port libéré avant de le lancer ; l'onglet ouvert se reconnecte
	// (flux d'événements) au lieu d'en ouvrir un second
	restartApp = func() {
		bound := ln.Addr().String()
		_ = srv.Close()
		a.Stop()
		if err := relaunch("desktop", "--web", "--addr", bound, "--no-open"); err != nil {
			fmt.Fprintln(os.Stderr, "relance impossible :", err)
		}
		os.Exit(0)
	}
	restartEnv = []string{"CL_DESKTOP_TOKEN=" + w.token}
	a.Start()
	if open {
		openURL(url)
	}
	<-done
	a.Stop()
	return nil
}

func openURL(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

// listenRetry : net.Listen, réessayé jusqu'à d si l'adresse est encore prise
// (port fixe libéré à l'instant par le processus précédent).
func listenRetry(addr string, d time.Duration) (net.Listener, error) {
	end := time.Now().Add(d)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil || time.Now().After(end) || strings.HasSuffix(addr, ":0") {
			return ln, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}
