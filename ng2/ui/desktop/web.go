package desktop

import (
	"claude-launcher-ng2/internal/tun"
	"context"
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
	"regexp"
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
	// access : « none » — aucun jeton, aucune session, aucun cookie (LOT-4.4) ; « proxy » — idem, l'identité vient d'oauth2-proxy
	// (v7.0.0) ; "" ou « token » : jeton, session, cookie
	access string
	// mode proxy (v7.0.0, contracts/auth.md) : listes de logins, adresses publiques supplémentaires (CL_WEB_ALT_URLS), journal d'audit
	auth  core.AuthConf
	alts  []core.PublicURL
	audit *core.Audit
	// healthSeparate : les sondes sont servies par un port dédié (CL_HEALTH_ADDR) ; l'écoute principale n'en sert plus
	healthSeparate bool

	mu        sync.Mutex           // jeton et sessions (authentification, appels)
	lastStamp time.Time            // dernière date attribuée à une session (strictement croissante : voir stamp)
	sessions  map[string]time.Time // identifiants de session en mémoire → dernier usage (le cookie porte l'un d'eux, jamais le jeton)

	subMu   sync.Mutex // abonnés SSE : jamais tenu en même temps que mu, jamais pendant une attente
	subs    map[chan []byte]bool
	subSess map[chan []byte]string // session de chaque abonné : fermé avec elle (expiration, éviction, révocation)
	subUID  map[chan []byte]string // espace de chaque abonné d'un utilisateur (v7.1.0) ; absent : espace de l'admin (App commune)
	spaces  spaceHub               // instances d'état d'interface des utilisateurs (spaces.go)
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

// noToken : aucun jeton, aucune session ni cookie du launcher (modes « none » et « proxy »).
func (w *webBridge) noToken() bool {
	return w.access == core.AccessNone || w.access == core.AccessProxy
}

// matchesPublic : l'en-tête Host désigne l'adresse publique principale ou l'une des adresses supplémentaires.
func (w *webBridge) matchesPublic(host string) bool {
	if w.public != nil && w.public.MatchesHost(host) {
		return true
	}
	for _, a := range w.alts {
		if a.MatchesHost(host) {
			return true
		}
	}
	return false
}

// baseFor : sous-chemin public (base) de l'adresse publique dont l'hôte est celui de la requête ; "" : racine (hôte interne,
// loopback, hub). contracts/auth.md §9.
func (w *webBridge) baseFor(host string) string {
	if w.public != nil && w.public.MatchesHost(host) {
		return w.public.Base
	}
	for _, a := range w.alts {
		if a.MatchesHost(host) {
			return a.Base
		}
	}
	return ""
}

type baseKey struct{}

// baseOf : base de la requête (posée par secured ; "" si aucune).
func baseOf(r *http.Request) string {
	b, _ := r.Context().Value(baseKey{}).(string)
	return b
}

// baseWriter : remet la base dans tout « Location » absolu-chemin émis par le routeur ou un gestionnaire (redirections de
// http.Redirect, de ServeMux, du serveur de fichiers) : le launcher travaille sans la base, le navigateur la voit.
type baseWriter struct {
	http.ResponseWriter
	base string
}

func (b *baseWriter) WriteHeader(code int) {
	if loc := b.Header().Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") && !strings.HasPrefix(loc, b.base+"/") {
		b.Header().Set("Location", b.base+loc)
	}
	b.ResponseWriter.WriteHeader(code)
}

func (b *baseWriter) Flush() {
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (b *baseWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

var (
	reBaseTag = regexp.MustCompile(`(?i)<base\s[^>]*>`)
	reHeadTag = regexp.MustCompile(`(?i)<head[^>]*>`)
)

// injectBase : <base href="<base>/"> inséré dans la page HTML (après <head>, sinon en tête) ; un <base> existant est remplacé.
func injectBase(page []byte, base string) []byte {
	tag := []byte(`<base href="` + base + `/">`)
	if reBaseTag.Match(page) {
		return reBaseTag.ReplaceAll(page, tag)
	}
	if i := reHeadTag.FindIndex(page); i != nil {
		return append(append(append([]byte{}, page[:i[1]]...), tag...), page[i[1]:]...)
	}
	return append(append([]byte{}, tag...), page...)
}

type ctxKey struct{}
type spaceKey struct{}

// spaceOf : espace de la requête (mode proxy : celui de l'identité) ; l'espace de l'admin ailleurs.
func spaceOf(r *http.Request) core.Space {
	if sp, ok := r.Context().Value(spaceKey{}).(core.Space); ok {
		return sp
	}
	return core.AdminSpace()
}

// identityOf : identité posée par le contrôle du mode proxy (nil ailleurs).
func identityOf(r *http.Request) *core.Identity {
	id, _ := r.Context().Value(ctxKey{}).(*core.Identity)
	return id
}

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
			delete(w.subUID, ch)
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
	w.sendTo("", b)
}

// emitTo : évènement aux seuls flux de l'espace uid (instance d'un utilisateur) — jamais à l'App commune ni aux autres.
func (w *webBridge) emitTo(uid, name string, data any) {
	b, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	w.sendTo(uid, b)
}

// sendTo : b aux abonnés de l'espace uid (« » : l'App commune, c'est-à-dire l'admin et les modes sans identité).
func (w *webBridge) sendTo(uid string, b []byte) {
	w.subMu.Lock()
	defer w.subMu.Unlock()
	for ch := range w.subs {
		if w.subUID[ch] != uid {
			continue
		}
		select {
		case ch <- b:
		default:
			delete(w.subs, ch)
			delete(w.subSess, ch)
			delete(w.subUID, ch)
			close(ch)
		}
	}
}

// authorized : cookie, ou — pour la page d'amorçage seulement (allowQuery) —
// jeton de l'URL. Les appels (/api/call, /api/events) n'acceptent que le cookie :
// le jeton d'une URL traîne dans l'historique, les journaux et l'en-tête Referer.
func (w *webBridge) authorized(r *http.Request, allowQuery bool) bool {
	if w.noToken() { // aucun jeton : l'accès repose sur l'écoute, l'Ingress et les contrôles Host / origine / JSON
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
		if w.noToken() {
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
	for _, a := range w.alts { // autre adresse publique (CL_WEB_ALT_URLS) : son origine exacte
		if a.MatchesHost(r.Host) {
			return strings.EqualFold(o, a.Origin)
		}
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
	if w.matchesPublic(r.Host) { // nom d'une adresse publique, sur son port
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
		if !w.healthSeparate && w.isHealth(r) {
			w.serveHealth(rw, r)
			return
		}
		if !w.hostOK(r) {
			refuse(rw, "host", "hôte refusé")
			return
		}
		// sous-chemin public (contracts/auth.md §9) : la base de l'hôte de la requête est retirée avant le routeur (le proxy ne la
		// retire jamais), les « Location » la retrouvent ; hors de la base : 404 ; la base seule : 308 vers « <base>/ »
		if base := w.baseFor(r.Host); base != "" {
			p := r.URL.Path
			switch {
			case p == base:
				loc := base + "/"
				if r.URL.RawQuery != "" {
					loc += "?" + r.URL.RawQuery
				}
				http.Redirect(rw, r, loc, http.StatusPermanentRedirect)
				return
			case strings.HasPrefix(p, base+"/"):
				u := *r.URL
				u.Path = p[len(base):]
				if u.RawPath != "" {
					if strings.HasPrefix(u.RawPath, base+"/") {
						u.RawPath = u.RawPath[len(base):]
					} else {
						u.RawPath = ""
					}
				}
				r2 := r.WithContext(context.WithValue(r.Context(), baseKey{}, base))
				r2.URL = &u
				r = r2
				rw = &baseWriter{ResponseWriter: rw, base: base}
			default:
				http.NotFound(rw, r)
				return
			}
		}
		h := rw.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		if w.access == core.AccessProxy && !publicStatic(r) { // identité exigée partout sauf santé, /signed-out et /too-many
			// seul le sidecar (même espace réseau que le pod) est un client : toute autre source est refusée à CHAQUE requête
			if !loopbackSource(r.RemoteAddr) {
				w.auditDeny(r, "", http.StatusForbidden)
				rw.Header().Set("X-Launcher-Refusal", "source")
				http.Error(rw, "source refusée", http.StatusForbidden)
				return
			}
			id, why := w.auth.Identify(r.Header.Values("X-Forwarded-User"), r.Host)
			if why != "" {
				code := http.StatusUnauthorized
				if why == core.RefusalUser {
					code = http.StatusForbidden
				}
				w.auditDeny(r, strings.TrimSpace(r.Header.Get("X-Forwarded-User")), code)
				refuseIdentity(rw, why)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &id))
			// espace de l'identité : l'identifiant numérique GitHub d'un utilisateur est résolu (puis mis en cache) ; un login
			// repris par un autre compte est refusé (401 + audit) jusqu'à l'action de l'admin
			sp, err := core.SpaceOf(id)
			switch {
			case errors.Is(err, core.ErrLoginReused):
				w.auditDeny(r, id.Login, http.StatusUnauthorized)
				refuseIdentity(rw, core.RefusalIdentity)
				return
			case err != nil:
				w.auditDeny(r, id.Login, http.StatusServiceUnavailable)
				rw.Header().Set("Cache-Control", "no-store")
				http.Error(rw, "espace indisponible : identifiant GitHub introuvable", http.StatusServiceUnavailable)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), spaceKey{}, sp))
		}
		next.ServeHTTP(rw, r)
	})
}

// isHealth : sonde du cluster (GET ou HEAD /healthz, /readyz).
func (w *webBridge) isHealth(r *http.Request) bool {
	return (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

// serveHealth : réponse des sondes, sans session, SANS contrôle de Host (elles appellent l'adresse du pod), et sans aucune
// information (ni version, ni nom, ni état des projets).
func (w *webBridge) serveHealth(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Path == "/readyz" && w.app != nil && !w.app.ready.Load() {
		rw.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(rw, "starting\n")
		return
	}
	_, _ = io.WriteString(rw, "ok\n")
}

// healthHandler : port de santé dédié (CL_HEALTH_ADDR) — /healthz et /readyz seulement, toute autre route : 404 vide.
func (w *webBridge) healthHandler() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if w.isHealth(r) {
			w.serveHealth(rw, r)
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	})
}

// publicStatic : pages statiques servies sans identité (mode proxy) : /signed-out et /too-many.
func publicStatic(r *http.Request) bool { return signedOutRoute(r) || tooManyRoute(r) }

func tooManyRoute(r *http.Request) bool {
	return r.URL.Path == "/too-many" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

// loopbackSource : la connexion vient de la boucle locale (sidecar du pod).
func loopbackSource(remote string) bool {
	h, _, err := net.SplitHostPort(remote)
	if err != nil {
		h = remote
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// auditDeny : refus d'identité (401, 403) du launcher, agrégé par minute dans le journal d'audit.
func (w *webBridge) auditDeny(r *http.Request, login string, code int) {
	if w.audit != nil {
		_ = w.audit.Deny(login, code)
	}
}

// signedOutRoute : page /signed-out (statique, sans identité, exemptée par le proxy).
func signedOutRoute(r *http.Request) bool {
	return r.URL.Path == "/signed-out" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

// refuseIdentity : 401 (identité absente ou invalide) ou 403 (login non autorisé), cause dans X-Launcher-Refusal.
func refuseIdentity(rw http.ResponseWriter, why string) {
	rw.Header().Set("X-Launcher-Refusal", why)
	rw.Header().Set("Cache-Control", "no-store")
	if why == core.RefusalIdentity {
		http.Error(rw, core.MsgNoIdentity, http.StatusUnauthorized)
		return
	}
	http.Error(rw, core.MsgUserDenied, http.StatusForbidden)
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
			if w.noToken() { // aucun jeton : « /?t=… » (ancien lien) aboutit sur l'interface, sans cookie
				if hasToken {
					rw.Header().Set("Cache-Control", "no-store")
					http.Redirect(rw, r, "/", http.StatusSeeOther)
					return
				}
			} else if !w.authorized(r, true) {
				refuse(rw, "session", "jeton manquant ou invalide : ouvrir l'URL affichée au lancement")
				return
			}
			if hasToken && !w.noToken() {
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
		if r.URL.Path == "/" || r.URL.Path == "/index.html" { // <base href> : le client n'emploie que des chemins relatifs
			if b, err := fs.ReadFile(assets, "index.html"); err == nil {
				rw.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = rw.Write(injectBase(b, baseOf(r)))
				return
			}
		}
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
		if site := r.Header.Get("Sec-Fetch-Site"); (site == "cross-site" || site == "same-site" && w.noToken()) &&
			!filesSubresource(r.Header.Get("Sec-Fetch-Dest")) && !filesNavigation(r.Header.Get("Sec-Fetch-Dest"), r.Header.Get("Sec-Fetch-Mode")) {
			refuse(rw, "origin", "origine refusée")
			return
		}
		if !w.noToken() && !strings.HasPrefix(r.URL.Path, core.FilesPrefix+"/__claude-launcher__") && !w.authorized(r, false) {
			refuse(rw, "session", "session absente ou expirée : rouvrir le lien d'accès")
			return
		}
		w.auditRecord(r, identityOf(r), "files", filesProject(r.URL.Path), true, "")
		if !spaceOf(r).Admin { // un utilisateur n'a pas de projet sur la racine (les siens : LOT-3.2) : jamais ceux de l'admin
			http.NotFound(rw, r)
			return
		}
		filesH.ServeHTTP(rw, r)
	}
	// page de déconnexion (mode proxy, contracts/auth.md §2) : statique, sans identité ni appel /api
	mux.HandleFunc("/signed-out", func(rw http.ResponseWriter, r *http.Request) {
		if w.access != core.AccessProxy || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Cache-Control", "no-store")
		if b, err := fs.ReadFile(assets, "signed-out.html"); err == nil {
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = rw.Write(injectBase(b, baseOf(r)))
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(rw, strings.ReplaceAll(signedOutFallback, `href="/"`, `href="`+baseOf(r)+`/"`))
	})
	// page du 429 de Traefik (essais de connexion limités) : statique, sans identité (exemptée par oauth2-proxy)
	mux.HandleFunc("/too-many", func(rw http.ResponseWriter, r *http.Request) {
		if w.access != core.AccessProxy || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(rw, strings.ReplaceAll(tooManyPage, `href="/"`, `href="`+baseOf(r)+`/"`))
	})
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
		rw.Header().Set("Cache-Control", "no-store") // jamais mis en cache (secretReveal : valeur révélée)
		id := identityOf(r)
		if serr := core.StrictJSONArgs(req.Args); serr != nil { // clés en double (casse comprise) : jamais deux lectures possibles
			http.Error(rw, serr.Error(), http.StatusBadRequest)
			return
		}
		if sp := spaceOf(r); !sp.Admin { // instance de l'utilisateur : ses méthodes seulement, jamais l'App commune
			u := w.spaces.get(sp, id.Login)
			note := func(cat string) {
				op := req.Method
				if w.audit != nil && cat != "" {
					_ = w.audit.Record(core.AuditEntry{User: id.Login, Role: id.Role, Src: requestSource(r), Method: "backend.refused", Target: op, Reason: cat, OK: false, Err: "refused"})
				}
			}
			res, err := u.dispatch(webCall{Src: requestSource(r), Remote: w.remoteAccess(r), Sid: w.sessionID(r), Access: w.access, User: id, Base: baseOf(r), Note: note}, req.Method, req.Args)
			refused := false
			if m, ok := res.(map[string]any); ok && m["forbidden"] != nil { // refus R1 : cible journalisée, comme pour l'App commune
				refused = true
			}
			w.auditCall(r, id, req.Method, req.Args, res, err, userTarget(req.Method, req.Args), refused)
			w.auditBackendCall(r, id, req.Method, req.Args, res, err)
			out := map[string]any{"result": res}
			if err != nil {
				out["error"] = err.Error()
			}
			rw.Header().Set("Content-Type", "application/json")
			if m, ok := res.(map[string]any); ok && m["status"] == 429 && m["code"] == "rate" { // débit des dépôts dépassé : HTTP 429, même corps
				rw.WriteHeader(http.StatusTooManyRequests)
			}
			_ = json.NewEncoder(rw).Encode(out)
			return
		}
		target, r1 := w.app.r1Info(req.Method, req.Args)
		// action R1 : l'INTENTION est journalisée avant l'exécution ; journal inaccessible : l'écriture sensible est refusée
		if w.audit != nil && id != nil && r1 && core.R1Allowed(w.access, id) {
			if aerr := w.audit.Record(core.AuditEntry{User: id.Login, Role: id.Role, Src: requestSource(r), Method: req.Method, Target: target, OK: true, Phase: "intent"}); aerr != nil {
				rw.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(rw).Encode(map[string]any{"result": map[string]any{"ok": false, "msg": aerr.Error()}})
				return
			}
		}
		res, err := w.app.DispatchWebAs(webCall{Src: requestSource(r), Remote: w.remoteAccess(r), Sid: w.sessionID(r), Access: w.access, User: id, Base: baseOf(r)}, req.Method, req.Args)
		w.auditCall(r, id, req.Method, req.Args, res, err, target, r1)
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
		w.auditRecord(r, identityOf(r), "events", "", true, "")
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
		var userInst *userSpace
		if sp := spaceOf(r); !sp.Admin { // flux d'un utilisateur : ses évènements seulement (jamais ceux de l'App commune)
			if w.subUID == nil {
				w.subUID = map[chan []byte]string{}
			}
			w.subUID[ch] = sp.UID
			userInst = w.spaces.get(sp, identityOf(r).Login)
			userInst.clients.Add(1)
		}
		w.subMu.Unlock()
		if userInst != nil {
			defer func() { userInst.touch(); userInst.clients.Add(-1) }()
		}
		defer func() {
			w.subMu.Lock()
			if w.subs[ch] {
				delete(w.subs, ch)
				delete(w.subSess, ch)
				delete(w.subUID, ch)
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
		// mode proxy : durée de vie bornée du flux (≈ renouvellement du cookie) ; le client se reconnecte, l'identité est revérifiée
		var maxLife <-chan time.Time
		if w.access == core.AccessProxy {
			t := time.NewTimer(sseMaxLife())
			defer t.Stop()
			maxLife = t.C
		}
		ping := time.NewTicker(ssePingEvery.Get())
		defer ping.Stop()
		check := time.NewTicker(checkEvery())
		defer check.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-maxLife:
				return
			case <-check.C:
				if !w.noToken() && !w.sessionAlive(sid) { // session expirée, évincée ou révoquée : flux fermé
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

// signedOutFallback : page de déconnexion minimale (sans le fichier signed-out.html du client).
const signedOutFallback = `<!doctype html><html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Déconnecté</title>` +
	`<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem"><h1>Déconnecté du launcher</h1>` +
	`<p>La session GitHub de ce navigateur reste ouverte : se reconnecter peut aboutir sans saisie.</p><p><a href="/">Se reconnecter</a></p></body></html>`

// tooManyPage : « Trop de tentatives » (style M2 A, sans ressource externe).
const tooManyPage = `<!doctype html><html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Trop de tentatives</title>` +
	`<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;background:#0F1217;color:#C9CED6"><h1>Trop de tentatives</h1>` +
	`<p>Trop de tentatives de connexion depuis cette adresse. Réessayer dans une minute.</p><p><a style="color:#60CDFF" href="/">Réessayer</a></p></body></html>`

// filesProject : nom de projet d'un chemin /files/<projet>/… ("" : racine ou chemin réservé).
func filesProject(p string) string {
	rest := strings.TrimPrefix(p, core.FilesPrefix+"/")
	name, _, _ := strings.Cut(rest, "/")
	if strings.HasPrefix(name, "__") {
		return ""
	}
	return name
}

// auditRecord : une entrée du journal d'audit (mode proxy seulement) ; l'erreur d'écriture n'interrompt pas l'appel (avertissement
// unique du journal), seules les méthodes R1 en dépendent (contrôle préalable à l'appel).
func (w *webBridge) auditRecord(r *http.Request, id *core.Identity, method, project string, ok bool, errs string) {
	if w.audit == nil || id == nil {
		return
	}
	_ = w.audit.Record(core.AuditEntry{User: id.Login, Role: id.Role, Src: requestSource(r), Method: method, Project: project, OK: ok, Err: errs})
}

// auditCall : journalise un appel /api/call — jamais d'argument, seulement le nom du projet qu'il porte.
func (w *webBridge) auditCall(r *http.Request, id *core.Identity, method string, args json.RawMessage, res any, err error, target string, r1 bool) {
	if w.audit == nil || id == nil {
		return
	}
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &a)
	project := ""
	if a.Path != "" {
		project = core.CtrName(a.Path)
	}
	ok, es := err == nil, ""
	if err != nil {
		es = "error"
	}
	if m, isMap := res.(map[string]any); isMap {
		if _, forbid := m["forbidden"]; forbid {
			ok, es = false, "forbidden"
		} else if v, has := m["ok"].(bool); has && !v && ok {
			ok, es = false, "error"
		} else if inv, has := m["invalid"].(bool); has && inv && ok {
			ok, es = false, "invalid"
		}
	}
	if !r1 {
		target = ""
	}
	_ = w.audit.Record(core.AuditEntry{User: id.Login, Role: id.Role, Src: requestSource(r), Method: method, Project: project, Target: target, OK: ok, Err: es})
}

// sseMaxLife : durée de vie maximale d'un flux d'évènements en mode proxy : CL_AUTH_SESSION_REFRESH (défaut 1 h).
var sseMaxLife = func() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("CL_AUTH_SESSION_REFRESH")); err == nil && d > 0 {
		return d
	}
	return time.Hour
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

// runWebApp : crochet de test, appelé avec l'App créée par RunWeb (qui ne la rend pas) : un test l'arrête et attend ses tâches de fond
// avant l'effacement de ses dossiers temporaires (#270). Sans effet en production.
var runWebApp = tun.Of(func(*App) {})

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
	// une base (sous-chemin public) n'a de sens que derrière oauth2-proxy : hors du mode proxy, le cookie de session du launcher
	// reste en Path=/ (__Host-) et fuirait vers les autres applications de l'hôte partagé
	if access != core.AccessProxy {
		if w.public != nil && w.public.Base != "" {
			return fmt.Errorf("adresse publique avec chemin (%s) réservée au mode proxy (CL_WEB_ACCESS=proxy) : le cookie de session du mode %s est en Path=/ et fuirait vers les autres applications de l'hôte", w.public.URL(), access)
		}
	}
	// mode proxy (v7.0.0) : listes de logins, adresses publiques supplémentaires ; réservé au cluster (fichier de réglages)
	if access == core.AccessProxy {
		if !core.InCluster() && strings.TrimSpace(os.Getenv(core.WebAccessVar)) == "" {
			return errors.New("accès proxy : réservé au launcher de cluster")
		}
		if os.Getenv("CL_ALLOW_HOST_BACKEND") != "" { // une session tmux du conteneur du launcher forgerait l'identité (MAJ-3)
			return errors.New("backend host interdit derrière le proxy d'authentification")
		}
		auth, err := core.LoadAuthConf()
		if err != nil {
			return err
		}
		alts, err := core.ParseAltURLs(os.Getenv(core.WebAltURLsVar))
		if err != nil {
			return err
		}
		w.auth, w.alts, w.audit = auth, alts, core.NewAudit("")
		if core.UsersAllowPrivate() { // journalisé au démarrage (users.allowPrivateNetworks) : risque assumé par l'administrateur
			fmt.Println("⚠ users.allowPrivateNetworks actif : les backends des utilisateurs peuvent viser les plages privées et les noms internes (loopback, link-local, métadonnées, multicast restent refusés)")
		}
	}
	// jeton (mode « token » seulement) : CL_DESKTOP_TOKEN, sinon — dans un pod — le jeton conservé (il ne change plus à
	// chaque redémarrage du pod : un lien déjà distribué reste valable) ; hors cluster, toujours un nouveau jeton (#167).
	// Mode « none » : aucun jeton n'est tiré, lu ni écrit ; CL_DESKTOP_TOKEN est ignoré.
	if access == core.AccessNone || access == core.AccessProxy {
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
	reapStop := make(chan struct{})
	defer close(reapStop)
	go w.spaces.runReaper(reapStop) // instances des utilisateurs libérées après 10 min sans client
	a := NewApp(w.emit)
	runWebApp.Get()(a) // crochet de test : le test arrête et attend les tâches de fond de CETTE App (#270)
	a.web = true       // le presse-papiers de cette machine n'est jamais exposé à un client web
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
	if access == core.AccessProxy { // garde du mode « proxy » : boucle locale, adresse https, au moins un administrateur
		if m := w.auth.StartRefusal(w.strictHost, w.public); m != "" {
			_ = ln.Close()
			return errors.New(m)
		}
	}
	// port de santé dédié (CL_HEALTH_ADDR) : /healthz et /readyz seulement ; vide : sondes sur l'écoute principale
	var healthSrv *http.Server
	if ha := strings.TrimSpace(os.Getenv("CL_HEALTH_ADDR")); ha != "" {
		hln, herr := listenRetry(ha, 10*time.Second)
		if herr != nil {
			_ = ln.Close()
			return fmt.Errorf("port de santé %s : %v", ha, herr)
		}
		w.healthSeparate = true
		healthSrv = newHTTPServer(w.healthHandler())
		go func() { _ = healthSrv.Serve(hln) }()
		defer healthSrv.Close()
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
		url = w.public.URL() + "/?t=" + w.tokenNow() // lien d'amorçage : l'adresse publique
	}
	if w.noToken() { // aucun jeton : l'adresse seule s'ouvre directement
		url = "http://" + ln.Addr().String() + "/"
		if w.public != nil {
			url = w.public.URL() + "/"
		}
	}
	if access == core.AccessProxy {
		// jamais la liste des logins, jamais de jeton
		fmt.Println("Claude Launcher (interface web) :", w.public.URL()+"/")
		fmt.Printf("Accès authentifié (oauth2-proxy, GitHub) : %d administrateur(s), %d utilisateur(s)\n", len(w.auth.Admins), len(w.auth.Users))
		fmt.Println("Arrêt par SIGTERM : serveur fermé, terminaux détachés ; les projets restent ouverts dans leurs pods.")
	} else if access == core.AccessNone {
		// aucun jeton : l'adresse seule (dans un pod la sortie part dans les journaux, sans rien de secret)
		base := "http://" + ln.Addr().String()
		if w.public != nil {
			base = w.public.URL()
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
			base = w.public.URL()
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
	if !w.noToken() {
		restartEnv.Set([]string{"CL_DESKTOP_TOKEN=" + w.token})
	}
	a.Start()
	if w.audit != nil {
		stopAudit := make(chan struct{})
		defer func() { close(stopAudit); _ = w.audit.Flush(true) }()
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					_ = w.audit.Flush(false)
				case <-stopAudit:
					return
				}
			}
		}()
	}
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
		if len(x) > 256 { // la QUEUE est gardée : Traefik ajoute la vraie adresse source en fin de chaîne
			x = x[len(x)-256:]
		}
		x = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, x)
		s += " (X-Forwarded-For: " + x + ")"
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

// auditBackendCall : audit « backend.call » d'un appel sortant d'un utilisateur vers un de ses backends (D5) — utilisateur, backend,
// opération, résultat ; JAMAIS d'adresse, de secret ni de réponse du backend.
func (w *webBridge) auditBackendCall(r *http.Request, id *core.Identity, method string, args json.RawMessage, res any, err error) {
	if w.audit == nil || id == nil {
		return
	}
	switch method {
	case "backendTest", "backendSecretSet", "backendSecretGet", "backendSecretDelete", "userBackendAdd", "userBackendRemove":
	default:
		return
	}
	var a struct {
		Backend json.RawMessage `json:"backend"`
		Name    string          `json:"name"`
	}
	_ = json.Unmarshal(args, &a)
	name := a.Name
	if json.Unmarshal(a.Backend, &name) != nil { // backendTest : objet {name…}
		var o struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(a.Backend, &o)
		name = o.Name
	}
	if len([]rune(name)) > 63 || !auditNameRE.MatchString(name) {
		name = ""
	}
	ok, es := err == nil, ""
	if err != nil {
		es = "error"
	}
	if m, isMap := res.(map[string]any); isMap {
		if v, has := m["ok"].(bool); has && !v {
			ok, es = false, "refused"
			if st, _ := m["status"].(int); st == 429 {
				es = "rate"
			}
		}
	}
	_ = w.audit.Record(core.AuditEntry{User: id.Login, Role: id.Role, Src: requestSource(r), Method: "backend.call", Target: method + ":" + name, OK: ok, Err: es})
}

var auditNameRE = regexp.MustCompile(`^[a-z0-9-]*$`)
