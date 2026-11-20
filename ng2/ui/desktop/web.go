package desktop

import (
	"claude-launcher-ng2/internal/tun"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	// public : adresse publique derrière un proxy inverse (#171) ; nil : aucune. Son nom d'hôte est admis dans
	// Host (sur son port), son origine exacte dans Origin, https y rend le cookie Secure. Aucun en-tête
	// X-Forwarded-*, Forwarded ni X-Real-IP n'est lu : le schéma et l'hôte publics ne viennent que de là.
	public *core.PublicURL
	// access : « none » — aucun jeton, aucune session, aucun cookie (LOT-4.4) ; "" ou « token » : jeton, session, cookie
	access string

	mu        sync.Mutex           // jeton et sessions (authentification, appels)
	lastStamp time.Time            // dernière date attribuée à une session (strictement croissante : voir stamp)
	sessions  map[string]time.Time // identifiants de session en mémoire → dernier usage (le cookie porte l'un d'eux, jamais le jeton)

	subMu   sync.Mutex // abonnés SSE : jamais tenu en même temps que mu, jamais pendant une attente
	subs    map[chan []byte]bool
	subSess map[chan []byte]string // session de chaque abonné : fermé avec elle (expiration, éviction, révocation)
	active  atomic.Int32           // gestionnaires de flux vivants (jusqu'à leur vraie sortie, même retirés de subs)
}

const (
	maxSessions = 64 // au-delà, la plus ancienne est évincée
)

// sessionIdle : durée d'inactivité au-delà de laquelle une session expire (variable : tests).
var sessionIdle = tun.Of(8 * time.Hour)

// maxSubscribers : plafond de flux d'évènements ouverts (un navigateur en ouvre un) ; variable : tests.
var maxSubscribers = tun.Of(32)

// sseWriteTimeout : échéance de chaque envoi d'évènement ; au-delà (connexion figée) le flux est fermé.
var sseWriteTimeout = tun.Of(10 * time.Second)

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
	// adresse publique en https : préfixe __Host- (le navigateur exige Secure, Path=/ et aucun Domain : un
	// cookie posé par un sous-domaine voisin ne peut pas le remplacer)
	prefix := ""
	if w.public != nil && w.public.HTTPS() {
		prefix = "__Host-"
	}
	port := w.port
	if port == "" {
		if _, p, err := net.SplitHostPort(r.Host); err == nil {
			port = p
		}
	}
	if port == "" {
		return prefix + "cl_session"
	}
	return prefix + "cl_session_" + port
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
		if now.Sub(at) > sessionIdle.Get() {
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
	w.sessions[id] = w.stamp(now)
	w.mu.Unlock()
	w.dropSubs(gone...) // les flux des sessions perdues sont fermés (verrous pris l'un après l'autre)
	return id
}

// stamp : date d'usage d'une session, STRICTEMENT croissante (w.mu tenu). L'horloge de Windows a une résolution de
// l'ordre de la milliseconde (et plus) : des sessions amorcées dans le même pas portaient la même date, et l'éviction
// de « la plus ancienne » choisissait au hasard parmi elles — une session récente partait, l'ancienne restait, son
// flux d'évènements restait ouvert (#224).
func (w *webBridge) stamp(now time.Time) time.Time {
	if !now.After(w.lastStamp) {
		now = w.lastStamp.Add(time.Nanosecond)
	}
	w.lastStamp = now
	return now
}

// sessionAlive : la session existe et n'a pas expiré — sans la prolonger (un flux ouvert n'est pas une activité).
func (w *webBridge) sessionAlive(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	at, ok := w.sessions[id]
	return ok && time.Since(at) <= sessionIdle.Get()
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
	d := sessionIdle.Get() / 4
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
	if now.Sub(at) > sessionIdle.Get() {
		delete(w.sessions, id)
		return false
	}
	w.sessions[id] = w.stamp(now)
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
	if w.access == core.AccessNone { // aucun jeton : l'accès repose sur l'écoute, l'Ingress et les contrôles Host / origine / JSON
		return true
	}
	if c, err := r.Cookie(w.cookieName(r)); err == nil && c.Value != "" && w.validSession(c.Value) {
		return true
	}
	return allowQuery && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(w.tokenNow())) == 1
}

// sameOrigin : refuse un appel dont l'en-tête Origin, s'il est présent, n'est pas
// celui du serveur (même schéma, hôte et port). Une page d'un autre port de la
// machine — serveur de maquettes, serveur de développement d'un projet — est du
// même « site » : SameSite=Strict ne la retient pas.
func (w *webBridge) sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site": // le navigateur déclare une requête d'un autre site : refusée (les deux modes)
		return false
	case "same-site": // un autre sous-domaine du même site : refusé en mode « none » (aucun jeton ne le distingue)
		if w.access == core.AccessNone {
			return false
		}
	} // same-origin, none (navigation de l'utilisateur) ou absent : règles habituelles (Origin, corps JSON)
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	if w.public != nil && w.public.MatchesHost(r.Host) { // hôte public : l'origine de l'adresse publique, exactement
		return strings.EqualFold(o, w.public.Origin)
	}
	return strings.EqualFold(o, "http://"+r.Host)
}

// jsonRequest : le corps est annoncé en JSON. Une requête « simple » (text/plain,
// formulaire) n'a pas de pré-requête CORS : elle part sans contrôle du navigateur.
func jsonRequest(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// hostOK : l'en-tête Host désigne bien la boucle locale (et le port d'écoute).
func (w *webBridge) hostOK(r *http.Request) bool {
	if w.public != nil && w.public.MatchesHost(r.Host) { // nom de l'adresse publique, sur son port
		return true
	}
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
		// sondes du cluster : sans session, SANS contrôle de Host (elles appellent l'adresse du pod), et sans aucune
		// information (ni version, ni nom, ni état des projets)
		if (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			rw.Header().Set("Cache-Control", "no-store")
			rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if r.URL.Path == "/readyz" && w.app != nil && !w.app.ready.Load() {
				rw.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(rw, "starting\n")
				return
			}
			_, _ = io.WriteString(rw, "ok\n")
			return
		}
		if !w.hostOK(r) {
			refuse(rw, "host", "hôte refusé")
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

// refuse : 403 dont la cause se lit dans l'en-tête X-Launcher-Refusal (host, origin, session, method) et dans le
// corps : l'interface distingue une session perdue d'un refus d'hôte ou d'origine (#171).
func refuse(rw http.ResponseWriter, kind, msg string) {
	rw.Header().Set("X-Launcher-Refusal", kind)
	http.Error(rw, msg, http.StatusForbidden)
}

func (w *webBridge) handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(frontendFS, "frontend")
	files := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			_, hasToken := r.URL.Query()["t"]
			if w.access == core.AccessNone { // aucun jeton : « /?t=… » (ancien lien) aboutit sur l'interface, sans cookie
				if hasToken {
					rw.Header().Set("Cache-Control", "no-store")
					http.Redirect(rw, r, "/", http.StatusSeeOther)
					return
				}
			} else if !w.authorized(r, true) {
				refuse(rw, "session", "jeton manquant ou invalide : ouvrir l'URL affichée au lancement")
				return
			}
			if hasToken && w.access != core.AccessNone {
				// le jeton de l'URL est échangé contre un identifiant de session aléatoire, en mémoire, révoqué à
				// l'arrêt du serveur ou à la régénération du jeton : le cookie ne porte jamais le jeton. Aucune page
				// n'est servie sur une URL qui porte le jeton (historique, journaux, Referer) : 303 vers « / ».
				if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(w.tokenNow())) == 1 {
					http.SetCookie(rw, &http.Cookie{Name: w.cookieName(r), Value: w.newSession(), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
						Secure: w.public != nil && w.public.HTTPS()}) // https public : le navigateur ne renvoie le cookie qu'en HTTPS
				}
				rw.Header().Set("Cache-Control", "no-store")
				http.Redirect(rw, r, "/", http.StatusSeeOther)
				return
			}
		}
		rw.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(rw, r)
	})
	// fichiers des projets (#174) : même serveur que celui des maquettes, sous /files ; sans session en mode « none » (la
	// protection est l'ingress interne), session exigée en mode jeton ; l'appariement du navigateur a son propre jeton
	filesH := core.NewFilesHandler(w.app.projectRootByName)
	filesRoute := func(rw http.ResponseWriter, r *http.Request) {
		// Sec-Fetch-Site comme pour l'interface (contrat « Contrôles ») : une page ou un document venu d'un autre site
		// est refusé (même site : refusé aussi en mode « none », aucun jeton ne le distingue) ; navigation de
		// l'utilisateur (none) et même origine passent. Exception : les SOUS-RESSOURCES d'une maquette (feuille de
		// style, script, image, police, média) — une maquette est servie en origine opaque (CSP sandbox), ses
		// ressources relatives partent donc « cross-site ». Sans danger : nosniff (un script ou une feuille de style ne
		// se charge qu'avec un type MIME qui convient), aucune lecture possible sans CORS (jamais d'en-tête
		// Access-Control-*), et les éléments sensibles ne sont jamais servis.
		if site := r.Header.Get("Sec-Fetch-Site"); (site == "cross-site" || site == "same-site" && w.access == core.AccessNone) &&
			!filesSubresource(r.Header.Get("Sec-Fetch-Dest")) && !filesNavigation(r.Header.Get("Sec-Fetch-Dest"), r.Header.Get("Sec-Fetch-Mode")) {
			refuse(rw, "origin", "origine refusée")
			return
		}
		if w.access != core.AccessNone && !strings.HasPrefix(r.URL.Path, core.FilesPrefix+"/__claude-launcher__") && !w.authorized(r, false) {
			refuse(rw, "session", "session absente ou expirée : rouvrir le lien d'accès")
			return
		}
		filesH.ServeHTTP(rw, r)
	}
	mux.HandleFunc("/api/call", func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method != http.MethodPost:
			refuse(rw, "method", "méthode refusée")
			return
		case !w.authorized(r, false):
			refuse(rw, "session", "session absente ou expirée : rouvrir le lien d'accès")
			return
		case !w.sameOrigin(r):
			refuse(rw, "origin", "origine refusée")
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
		res, err := w.app.DispatchFromAddr(originWeb, requestSource(r), req.Method, req.Args)
		out := map[string]any{"result": res}
		if err != nil {
			out["error"] = err.Error()
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(out)
	})
	mux.HandleFunc("/api/events", func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case !w.authorized(r, false):
			refuse(rw, "session", "session absente ou expirée : rouvrir le lien d'accès")
			return
		case !w.sameOrigin(r):
			refuse(rw, "origin", "origine refusée")
			return
		}
		rc := http.NewResponseController(rw)
		if _, ok := rw.(http.Flusher); !ok {
			http.Error(rw, "flux impossible", http.StatusInternalServerError)
			return
		}
		// plafond : un gestionnaire compte jusqu'à sa vraie sortie, même si emit l'a retiré des abonnés
		if int(w.active.Add(1)) > maxSubscribers.Get() {
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
		rw.Header().Set("Cache-Control", "no-store, no-transform") // un proxy ne met ni en tampon ni ne transforme le flux
		rw.Header().Set("X-Accel-Buffering", "no")
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
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout.Get()))
			if _, err := fmt.Fprintf(rw, format, args...); err != nil {
				return false
			}
			return rc.Flush() == nil
		}
		if !send(": ok\n\n") {
			return
		}
		// évènement d'accueil (#198) : version et démarrage du serveur dès l'ouverture du flux, pour que le client
		// détecte une mise à jour ou un redémarrage même si son `init` a échoué pendant que le serveur démarrait
		if hello, err := json.Marshal(map[string]any{"name": "hello", "data": map[string]any{"server": serverInfo()}}); err == nil {
			if !send("data: %s\n\n", hello) {
				return
			}
		}
		ping := time.NewTicker(ssePingEvery.Get())
		defer ping.Stop()
		check := time.NewTicker(checkEvery())
		defer check.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-check.C:
				if w.access != core.AccessNone && !w.sessionAlive(sid) { // session expirée, évincée ou révoquée : flux fermé
					return
				}
			case <-ping.C:
				// commentaire (proxys, tampons) + vrai évènement : le client le voit en JS et détecte un flux à moitié ouvert
				if !send(": ping\n\ndata: {\"name\":\"ping\"}\n\n") {
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
	// /files, /files/, /files//… : traités ICI, avant le routeur (qui redirigerait « /files » et nettoierait « // » en 307) :
	// racine et formes inconnues répondent 404 sans rien dire (#174)
	return w.secured(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == core.FilesPrefix || strings.HasPrefix(r.URL.Path, core.FilesPrefix+"/") {
			filesRoute(rw, r)
			return
		}
		mux.ServeHTTP(rw, r)
	}))
}

// ssePingEvery : cadence du ping du flux d'évènements (#198) ; remplacée dans les tests.
var ssePingEvery = tun.Of(15 * time.Second)

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
	// adresse publique : --url > CL_WEB_URL > desktop_web.url ; invalide : le serveur ne démarre pas
	c, _ := core.DesktopWebSetting() // c.URL : desktop_web.url, même quand l'interface de la fenêtre est éteinte
	pub, _, perr := effectivePublicURL(c)
	if perr != nil {
		return perr
	}
	w.public = pub
	// mode d'accès : CL_WEB_ACCESS > desktop_web.access > token ; valeur inconnue : le serveur ne démarre pas
	access, aerr := core.EffectiveWebAccess(c)
	if aerr != nil {
		return aerr
	}
	w.access = access
	// jeton (mode « token » seulement) : CL_DESKTOP_TOKEN, sinon — dans un pod — le jeton conservé (il ne change plus à
	// chaque redémarrage du pod : un lien déjà distribué reste valable) ; hors cluster, toujours un nouveau jeton (#167).
	// Mode « none » : aucun jeton n'est tiré, lu ni écrit ; CL_DESKTOP_TOKEN est ignoré.
	if access == core.AccessNone {
		w.token = ""
		removeDesktopWebToken() // le fichier d'un mode « token » précédent ne reste pas sur le volume (#F3)
	} else if t := os.Getenv("CL_DESKTOP_TOKEN"); t != "" {
		w.token = t
	} else if core.InCluster() {
		if t, err := desktopWebToken(); t != "" {
			w.token = t
			if err != nil {
				fmt.Fprintln(os.Stderr, "⚠ "+err.Error())
			}
		}
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
	if m := core.AccessNoneRefusal(access, w.strictHost, w.public); m != "" { // garde du mode « none »
		_ = ln.Close()
		return errors.New(m)
	}
	if warn != "" && !core.InCluster() { // en pod, l'écoute sur toutes les interfaces derrière l'Ingress est voulue
		fmt.Println("⚠ " + warn)
	}
	// mode serveur : l'interface est le processus lui-même, toujours « on » ; le commutateur desktop_web.enabled
	// ne s'y applique pas (#183)
	a.webui.mu.Lock()
	a.webui.state = WebUIState{State: "on", Listen: ln.Addr().String(), Lan: !w.strictHost, Warn: warn, Enabled: true, Server: true, URL: publicOrigin(w.public), Access: access}
	a.webui.mu.Unlock()
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), w.tokenNow())
	if w.public != nil {
		url = w.public.Origin + "/?t=" + w.tokenNow() // lien d'amorçage : l'adresse publique
	}
	if access == core.AccessNone { // aucun jeton : l'adresse seule s'ouvre directement
		url = "http://" + ln.Addr().String() + "/"
		if w.public != nil {
			url = w.public.Origin + "/"
		}
	}
	if access == core.AccessNone {
		// aucun jeton : l'adresse seule (dans un pod la sortie part dans les journaux, sans rien de secret)
		base := "http://" + ln.Addr().String()
		if w.public != nil {
			base = w.public.Origin
		}
		fmt.Println("Claude Launcher (interface web) :", base+"/")
		fmt.Println("Accès sans jeton (CL_WEB_ACCESS=none) : l'accès repose sur l'écoute locale ou sur l'adresse publique https (Ingress, liste d'adresses).")
		if core.InCluster() {
			fmt.Println("Arrêt par SIGTERM : serveur fermé, terminaux détachés ; les projets restent ouverts dans leurs pods.")
		} else {
			fmt.Println("Ctrl+C pour arrêter (les sessions des projets continuent dans tmux).")
		}
	} else if core.InCluster() {
		// en pod, la sortie standard part dans les journaux (pods/log, collecte) : jamais le jeton. L'adresse est
		// donnée sans jeton, avec l'endroit où le lire.
		base := "http://" + ln.Addr().String()
		if w.public != nil {
			base = w.public.Origin
		}
		fmt.Println("Claude Launcher (mode serveur) :", base+"/")
		if os.Getenv("CL_DESKTOP_TOKEN") != "" {
			fmt.Println("Jeton d'accès : fourni par CL_DESKTOP_TOKEN ; lien d'amorçage : " + base + "/?t=<jeton>")
		} else {
			fmt.Println("Jeton d'accès : à lire dans " + desktopWebTokenPath() + " (conservé d'un démarrage à l'autre) ; lien d'amorçage : " + base + "/?t=<jeton>")
		}
		fmt.Println("Arrêt par SIGTERM : serveur fermé, terminaux détachés ; les projets restent ouverts dans leurs pods.")
	} else {
		fmt.Println("Claude Launcher (interface web) :", url)
		fmt.Println("Ctrl+C pour arrêter (les sessions des projets continuent dans tmux).")
	}
	srv := newHTTPServer(w.handler())
	done := make(chan struct{})
	quitHook.Set(func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = srv.Close()
		}()
	})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	// SIGTERM (arrêt d'un pod) et SIGINT : arrêt propre — serveur fermé, terminaux détachés (a.Stop après <-done),
	// code 0 ; les projets restent ouverts dans leurs pods / tmux (#167)
	defer onSignalOnce(func() { _ = srv.Close() }, syscall.SIGTERM, os.Interrupt)()
	// mise à jour installée : même adresse et même jeton pour le nouveau
	// binaire, port libéré avant de le lancer ; l'onglet ouvert se reconnecte
	// (flux d'événements) au lieu d'en ouvrir un second
	restartApp.Set(func() {
		bound := ln.Addr().String()
		_ = srv.Close()
		a.Stop()
		if err := relaunch(relaunchArgs(bound)...); err != nil {
			fmt.Fprintln(os.Stderr, "relance impossible :", err)
		}
		os.Exit(0)
	})
	if access != core.AccessNone {
		restartEnv.Set([]string{"CL_DESKTOP_TOKEN=" + w.token})
	}
	a.Start()
	if open {
		openURL(url)
	}
	<-done
	a.Stop()
	return nil
}

// onSignalOnce : exécute fn au PREMIER des signaux sigs, puis rend leur traitement au système (signal.Stop) : un second
// signal tue le processus (abréger un arrêt lent). Rend la fonction qui retire la veille.
func onSignalOnce(fn func(), sigs ...os.Signal) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			// la place du launcher est rendue TOUT DE SUITE (avant l'arrêt, parfois lent) : un arrêt qui finit en SIGKILL
			// ne laisse pas de témoin de présence, le successeur du même nom démarre sans attendre
			core.ReleasePresence()
			signal.Stop(ch) // traitement par défaut rétabli : le prochain signal est fatal
			fn()
		case <-done:
		}
	}()
	return func() { signal.Stop(ch); close(done) }
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

// requestSource : adresse source d'un appel web, pour le journal des écritures de secrets : l'adresse de la
// connexion et, derrière un ingress, X-Forwarded-For (indication, non fiable).
func requestSource(r *http.Request) string {
	s := r.RemoteAddr
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	if x := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); x != "" {
		if len(x) > 100 {
			x = x[:100]
		}
		s += " (X-Forwarded-For: " + strings.NewReplacer("\n", " ", "\r", " ").Replace(x) + ")"
	}
	return s
}

// filesNavigation : navigation de premier niveau (Sec-Fetch-Mode=navigate, Sec-Fetch-Dest=document) : l'utilisateur — ou
// l'extension de navigateur de claude — ouvre un lien, comme une URL tapée ; acceptée quelle que soit sa provenance
// (Sec-Fetch-Site cross-site : lien d'un autre site, onglet ouvert par l'extension). Un cadre (iframe, frame), un objet
// (embed, object) ou un appel fetch/XHR d'un autre site restent refusés. Le contenu ne s'exécute qu'en origine opaque
// (CSP sandbox) : la page ouverte ne lit ni cookie ni /api de l'interface.
func filesNavigation(dest, mode string) bool { return dest == "document" && mode == "navigate" }

// filesSubresource : destination d'une sous-ressource de maquette (Sec-Fetch-Dest) ; un document, un cadre, un objet,
// un appel fetch/XHR (« empty ») ou l'absence de l'en-tête ne comptent pas.
func filesSubresource(dest string) bool {
	switch dest {
	case "style", "script", "image", "font", "audio", "video", "track":
		return true
	}
	return false
}
