package desktop

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/core"
)

// ── secretReveal (#227) : révéler un élément caché, à la demande ──────────
//
// Seules les clés de type secret du modèle (liste fermée, core.RevealableKey) ; refusé depuis un accès distant ; au plus
// revealPerMin révélations par minute et par session ; journal sans valeur ; la valeur n'est ni logguée ni dans un évènement.

const msgRevealRemote = "indisponible depuis un accès distant"

// revealPerMin : plafond de révélations par minute et par session.
var revealPerMin = 10

type revealLimiter struct {
	mu sync.Mutex
	at map[string][]time.Time
}

// allow : vrai si la session n'a pas atteint le plafond de la dernière minute ; la demande est comptée.
func (l *revealLimiter) allow(session string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.at == nil {
		l.at = map[string][]time.Time{}
	}
	now := time.Now()
	for k, ts := range l.at { // purge : sessions sans révélation dans la dernière minute (pas de croissance par clé distincte)
		if k != session && (len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= time.Minute) {
			delete(l.at, k)
		}
	}
	keep := l.at[session][:0]
	for _, t := range l.at[session] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= revealPerMin {
		l.at[session] = keep
		return false
	}
	l.at[session] = append(keep, now)
	return true
}

// secretReveal : {kind, key} → {ok:true, value} | {ok:false, msg}. iface : « fenêtre native » ou « web local » (journal) ;
// remote : accès distant → refusé ; session : clé du plafond de débit.
func (a *App) secretReveal(iface string, remote bool, session string, raw json.RawMessage) (any, error) {
	return a.secretRevealAs(iface, "", remote, session, raw)
}

// secretRevealAs : secretReveal avec l'utilisateur journalisé (login en mode proxy).
func (a *App) secretRevealAs(iface, user string, remote bool, session string, raw json.RawMessage) (any, error) {
	var in struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return map[string]any{"ok": false, "msg": "arguments invalides"}, nil
	}
	kind := core.ConfigKind(in.Kind)
	switch {
	case remote:
		return map[string]any{"ok": false, "msg": msgRevealRemote}, nil
	case !core.RevealableKey(kind, in.Key):
		return map[string]any{"ok": false, "msg": core.ErrNotRevealable.Error()}, nil
	case !a.reveal.allow(session):
		return map[string]any{"ok": false, "msg": "trop de révélations : réessayer dans une minute"}, nil
	}
	v, err := core.RevealSecret(kind, in.Key)
	if err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	if err := core.JournalRevealAs(in.Key, iface, user); err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	return map[string]any{"ok": true, "value": v}, nil
}

// DispatchWeb : appel venu du serveur web avec ce que le serveur sait de la requête — remote : accès distant (adresse
// publique, hôte non local, proxy), sid : session. secretReveal et configModel.reveal en dépendent ; le reste est DispatchFromAddr.
func (a *App) DispatchWeb(src string, remote bool, sid, method string, raw json.RawMessage) (any, error) {
	return a.DispatchWebAs(webCall{Src: src, Remote: remote, Sid: sid}, method, raw)
}

// webCall : ce que le serveur web sait d'une requête d'appel (v7.0.0).
type webCall struct {
	Src    string
	Remote bool
	Sid    string
	// Access : mode d'accès du serveur (token, none, proxy ; "" = token) ; User : identité du mode proxy (nil ailleurs)
	Access string
	User   *core.Identity
}

// msgR1 : refus d'une écriture sensible (règle R1, contracts/auth.md §4).
const msgR1 = "réservé à un administrateur authentifié (règle R1)"

// userLabel : utilisateur journalisé pour une écriture de secret : login (proxy), sinon "" (la source fait foi).
func (c webCall) userLabel() string {
	if c.User != nil {
		return c.User.Login
	}
	return ""
}

// userInfo : champ « user » d'init et de clientInfo (mode proxy seulement).
func (c webCall) userInfo() map[string]any {
	if c.User == nil || c.Access != core.AccessProxy {
		return nil
	}
	u := map[string]any{"login": c.User.Login, "role": c.User.Role, "logout": "/oauth2/sign_out?rd=%2Fsigned-out", "kind": "github"}
	if c.User.Local() {
		u["kind"] = "local"
	}
	// durée de session (affichage seulement : le cookie d'oauth2-proxy est opaque pour le launcher)
	if d, r := os.Getenv("CL_AUTH_SESSION_DURATION"), os.Getenv("CL_AUTH_SESSION_REFRESH"); d != "" && r != "" {
		u["session"] = map[string]any{"duration": d, "refresh": r}
	}
	return u
}

// accessInfo : champ « access » d'init et de clientInfo (tous les modes du serveur web).
func (c webCall) accessInfo() core.AccessInfo {
	mode := c.Access
	if mode == "" {
		mode = core.AccessToken
	}
	return core.AccessFor(mode, c.User)
}

// r1Info : l'appel est-il une écriture réservée à l'administrateur (R1) ? target : identifiant court pour le journal (kind de
// config, nom de backend), jamais une valeur. Méthode non classée : R1 (échec fermé) ; configSave / configApply : selon le
// fichier visé (config.conf, env.yaml du launcher, hub : R1 ; project-env : non ; backends.yaml : R1 si un champ R1 change).
func (a *App) r1Info(method string, raw json.RawMessage) (target string, r1 bool) {
	cls, known := core.ClassOf(method)
	if known && cls != core.ClassR1 {
		return "", false
	}
	var in struct {
		Kind, Path, Content, MTime, Backend string
		Changes                             []core.ConfigChange
	}
	// « backend » est un nom (chaîne) pour les secrets, un objet pour backendTest : seul le premier cas est décodé ici
	var generic map[string]json.RawMessage
	_ = json.Unmarshal(raw, &generic)
	_ = json.Unmarshal(generic["kind"], &in.Kind)
	_ = json.Unmarshal(generic["path"], &in.Path)
	_ = json.Unmarshal(generic["content"], &in.Content)
	_ = json.Unmarshal(generic["changes"], &in.Changes)
	_ = json.Unmarshal(generic["backend"], &in.Backend)
	switch method {
	case "configSave", "configApply":
		kind := core.ConfigKind(in.Kind)
		if core.ConfigKindR1(in.Kind) {
			return in.Kind, true
		}
		switch kind {
		case core.ConfProjectEnv:
			return in.Kind, false
		case core.ConfBackends:
			f, err := a.configFile(core.ConfBackends, "", "")
			if err != nil {
				return in.Kind, true
			}
			old, _ := os.ReadFile(f.Path)
			next := []byte(in.Content)
			if method == "configApply" {
				var aerr error
				if next, aerr = core.ApplyConfigChanges(core.ConfBackends, old, in.Changes); aerr != nil {
					return in.Kind, true
				}
			}
			return in.Kind, core.AdminFieldsChanged(old, next)
		}
		return in.Kind, true // fichier inconnu : échec fermé
	}
	return in.Backend, true
}

// DispatchWebAs : DispatchWeb avec l'identité et le mode d'accès (v7.0.0) — règle R1, champ user, journal des secrets.
func (a *App) DispatchWebAs(c webCall, method string, raw json.RawMessage) (any, error) {
	src, remote, sid := c.Src, c.Remote, c.Sid
	if c.Access == core.AccessProxy && method == "quit" { // arrêt du serveur : jamais depuis le web du cluster, même admin (MAJ-2)
		return nil, fmt.Errorf("quit : indisponible derrière le proxy d'authentification")
	}
	if _, r1 := a.r1Info(method, raw); r1 && !core.R1Allowed(c.Access, c.User) {
		return map[string]any{"ok": false, "forbidden": core.RoleAdmin, "msg": msgR1}, nil
	}
	switch method {
	case "clientInfo": // #250 : l'interface grise le réglage modèle · effort d'un accès distant
		out := map[string]any{"remote": remote, "access": c.accessInfo()}
		if u := c.userInfo(); u != nil {
			out["user"] = u
		}
		return out, nil
	case "init":
		res, err := a.DispatchFromAddr(originWeb, src, method, raw)
		if m, ok := res.(map[string]any); ok && err == nil {
			m["access"] = c.accessInfo()
			if u := c.userInfo(); u != nil {
				m["user"] = u
			}
		}
		return res, err
	case "teamAgentSet": // #250 : refusé côté serveur pour un accès distant
		return a.teamAgentSet(remote, raw)
	case "secretReveal":
		return a.secretRevealAs("web local", c.userLabel(), remote, "web:"+sid, raw)
	case "configModel":
		var arg struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}
		_ = json.Unmarshal(raw, &arg)
		if err := a.checkProjectArg(core.ConfigKind(arg.Kind) == core.ConfProjectEnv, arg.Path); err != nil {
			return nil, err
		}
		return a.configModelFor(!remote, core.ConfigKind(arg.Kind), arg.Path)
	}
	if secretMethods[method] {
		if src == "" {
			src = "navigateur"
		}
		return a.secretCallAs(src, c.userLabel(), method, raw)
	}
	return a.DispatchFromAddr(originWeb, src, method, raw)
}

// remoteAccess : la requête ne vient PAS de la boucle locale directe — adresse publique, hôte (en-tête Host) autre que la
// boucle locale, source autre que 127.0.0.1 / ::1, ou passage par un proxy (en-têtes X-Forwarded-*, Forwarded, X-Real-IP).
func (w *webBridge) remoteAccess(r *http.Request) bool {
	if w.access == core.AccessProxy { // derrière oauth2-proxy : toujours distant (contracts/auth.md §2)
		return true
	}
	if w.public != nil && w.public.MatchesHost(r.Host) {
		return true
	}
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	if h != "127.0.0.1" && h != "::1" && h != "localhost" {
		return true
	}
	src := r.RemoteAddr
	if hh, _, err := net.SplitHostPort(src); err == nil {
		src = hh
	}
	if ip := net.ParseIP(strings.Trim(src, "[]")); ip == nil || !ip.IsLoopback() {
		return true
	}
	for _, hd := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-IP"} {
		if r.Header.Get(hd) != "" {
			return true
		}
	}
	return false
}

// sessionID : identifiant de la session du navigateur (cookie) pour le plafond de débit ; à défaut, la source. En mode « none »
// (aucune session : le cookie n'est pas validé, un client en changerait à chaque appel) : la SOURCE seule.
func (w *webBridge) sessionID(r *http.Request) string {
	if id := identityOf(r); id != nil { // mode proxy : le plafond est par login (« user:<login> »)
		return "user:" + id.Login
	}
	if w.noToken() {
		return "src:" + requestSource(r)
	}
	if c, err := r.Cookie(w.cookieName(r)); err == nil && c.Value != "" {
		return c.Value
	}
	return "src:" + requestSource(r)
}
