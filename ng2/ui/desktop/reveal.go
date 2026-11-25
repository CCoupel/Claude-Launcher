package desktop

import (
	"encoding/json"
	"net"
	"net/http"
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
	if err := core.JournalReveal(in.Key, iface); err != nil {
		return map[string]any{"ok": false, "msg": err.Error()}, nil
	}
	return map[string]any{"ok": true, "value": v}, nil
}

// DispatchWeb : appel venu du serveur web avec ce que le serveur sait de la requête — remote : accès distant (adresse
// publique, hôte non local, proxy), sid : session. secretReveal et configModel.reveal en dépendent ; le reste est DispatchFromAddr.
func (a *App) DispatchWeb(src string, remote bool, sid, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "clientInfo": // #250 : l'interface grise le réglage modèle · effort d'un accès distant
		return map[string]any{"remote": remote}, nil
	case "teamAgentSet": // #250 : refusé côté serveur pour un accès distant
		return a.teamAgentSet(remote, raw)
	case "secretReveal":
		return a.secretReveal("web local", remote, "web:"+sid, raw)
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
	return a.DispatchFromAddr(originWeb, src, method, raw)
}

// remoteAccess : la requête ne vient PAS de la boucle locale directe — adresse publique, hôte (en-tête Host) autre que la
// boucle locale, source autre que 127.0.0.1 / ::1, ou passage par un proxy (en-têtes X-Forwarded-*, Forwarded, X-Real-IP).
func (w *webBridge) remoteAccess(r *http.Request) bool {
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
	if w.access == core.AccessNone {
		return "src:" + requestSource(r)
	}
	if c, err := r.Cookie(w.cookieName(r)); err == nil && c.Value != "" {
		return c.Value
	}
	return "src:" + requestSource(r)
}
