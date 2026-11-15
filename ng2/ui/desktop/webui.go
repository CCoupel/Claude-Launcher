package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"claude-launcher-ng2/core"
)

// ── Interface web servie par la fenêtre native (#163) ─────────────────────
//
// Le serveur de « desktop --web » devient un composant attaché à l'App de la fenêtre :
// même processus, mêmes onglets, même état ; les évènements partent aux deux interfaces.
// Réglage desktop_web de backends.yaml, relu à chaud (démarrage, arrêt, changement de
// port). Les protections du mode --web sont inchangées (jeton, cookie, Host, origine,
// en-têtes) ; chaque appel porte son origine.

// origin : d'où vient un appel de l'interface.
type origin int

const (
	originWindow origin = iota // la fenêtre native (Wails) : confiance complète
	originWeb                  // un navigateur, par le serveur web
)

// WebUIState : champ `webUI` de l'instantané — jamais le jeton.
type WebUIState struct {
	State  string `json:"state"` // off, on, error
	Listen string `json:"listen"`
	Msg    string `json:"msg"`
}

// webUI : serveur de l'interface attaché à l'App (a.webui).
type webUI struct {
	mu     sync.Mutex
	srv    *http.Server
	bridge *webBridge
	listen string // adresse réellement écoutée (vide : arrêté)
	last   string // dernière adresse écoutée, même arrêtée : un changement d'adresse renouvelle le jeton
	state  WebUIState
}

// desktopWebTokenPath : jeton de l'interface web, tiré une fois par poste.
func desktopWebTokenPath() string { return filepath.Join(core.CacheDir(), "desktop-web-token") }

// desktopWebToken : jeton conservé (CacheDir()/desktop-web-token, 0600) pour qu'un navigateur
// qui a le lien reste valable après une relance de la fenêtre ; créé atomiquement (O_EXCL, 0600).
// Erreur : le jeton rendu n'est alors pas conservé (la raison est dite dans webUI.msg).
func desktopWebToken() (string, error) {
	p := desktopWebTokenPath()
	if b, err := os.ReadFile(p); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 32 {
			return t, nil
		}
		_ = os.Remove(p) // illisible ou trop court : retiré, retiré au tirage suivant
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) { // tiré à l'instant par un autre : celui-là fait foi
			if b, e := os.ReadFile(p); e == nil && len(strings.TrimSpace(string(b))) >= 32 {
				return strings.TrimSpace(string(b)), nil
			}
		}
		return t, fmt.Errorf("jeton non conservé (%v) : il ne survivra pas à une relance", err)
	}
	defer f.Close()
	if _, err := f.WriteString(t + "\n"); err != nil {
		return t, fmt.Errorf("jeton non conservé (%v) : il ne survivra pas à une relance", err)
	}
	return t, nil
}

// rotateDesktopWebToken : jeton remplacé (fichier supprimé, nouveau tirage).
func rotateDesktopWebToken() (string, error) {
	_ = os.Remove(desktopWebTokenPath())
	return desktopWebToken()
}

// WebUI : état courant du serveur de l'interface.
func (a *App) WebUI() WebUIState {
	a.webui.mu.Lock()
	defer a.webui.mu.Unlock()
	if a.webui.state.State == "" {
		return WebUIState{State: "off"}
	}
	return a.webui.state
}

// applyWebUI : aligne le serveur sur le réglage desktop_web (démarre, arrête, déplace).
// Sans effet dans le processus de « desktop --web » (le serveur y est le processus lui-même).
func (a *App) applyWebUI() {
	if a.web {
		return
	}
	conf, err := core.DesktopWebSetting()
	w := &a.webui
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case err != nil: // réglage illisible : désactivé, la raison est dite
		a.stopWebUILocked()
		w.state = WebUIState{State: "error", Msg: err.Error()}
	case conf.Off:
		a.stopWebUILocked()
		w.state = WebUIState{State: "off"}
	case w.srv != nil && w.listen == conf.Listen:
		return // déjà à jour
	default:
		a.stopWebUILocked()
		a.startWebUILocked(conf)
	}
	go a.changed()
}

func (a *App) startWebUILocked(conf core.DesktopWebConf) {
	w := &a.webui
	ln, err := net.Listen("tcp", conf.Listen)
	if err != nil {
		w.state = WebUIState{State: "error", Listen: conf.Listen, Msg: "écoute impossible sur " + conf.Listen + " : " + err.Error()}
		return
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	var tok string
	var terr error
	if w.last != "" && w.last != conf.Listen { // adresse changée (surtout hors boucle locale) : nouveau jeton
		tok, terr = rotateDesktopWebToken()
	} else {
		tok, terr = desktopWebToken()
	}
	b := &webBridge{app: a, token: tok, subs: map[chan []byte]bool{}, port: port, hosts: conf.AllowedHosts(core.HubHost())}
	srv := newHTTPServer(b.handler())
	w.srv, w.bridge, w.listen, w.last = srv, b, conf.Listen, conf.Listen
	msg := conf.Warn
	if terr != nil {
		msg = strings.TrimSpace(msg + " " + terr.Error())
	}
	w.state = WebUIState{State: "on", Listen: conf.Listen, Msg: msg}
	mirror := Emitter(b.emit)
	a.mirror.Store(&mirror) // les évènements partent aussi au navigateur
	go func() { _ = srv.Serve(ln) }()
}

func (a *App) stopWebUILocked() {
	w := &a.webui
	if w.srv == nil {
		return
	}
	a.mirror.Store(nil)
	_ = w.srv.Close()
	w.bridge.revokeAll() // sessions révoquées, flux fermés (verrou propre au pont : pas celui de w)
	w.srv, w.bridge, w.listen = nil, nil, ""
}

// stopWebUI : arrêt à la fermeture de la fenêtre.
func (a *App) stopWebUI() {
	a.webui.mu.Lock()
	defer a.webui.mu.Unlock()
	a.stopWebUILocked()
	a.webui.state = WebUIState{State: "off"}
}

// webUIOpen : ouvre l'interface dans le navigateur par défaut du poste, avec l'adresse
// d'amorçage (jeton) construite ici. Refusé à un client web (ne passe que par la fenêtre).
func (a *App) webUIOpen() map[string]any {
	w := &a.webui
	w.mu.Lock()
	on, listen := w.srv != nil, w.listen
	var tok string
	if w.bridge != nil {
		tok = w.bridge.tokenNow() // le jeton du serveur : un seul tirage
	}
	w.mu.Unlock()
	if !on {
		return map[string]any{"ok": false, "msg": "l'interface web est désactivée (Réglages › Ce poste › Interface web)"}
	}
	h, port, _ := net.SplitHostPort(listen)
	if h == "" || h == "0.0.0.0" || h == "::" {
		h = "localhost"
	}
	openURL("http://" + net.JoinHostPort(h, port) + "/?t=" + tok)
	return map[string]any{"ok": true}
}

// closeAll : ferme les flux d'évènements des navigateurs connectés.
func (w *webBridge) closeAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subs {
		delete(w.subs, ch)
		close(ch)
	}
}

// hostAllowed : en-tête Host d'une écoute hors boucle locale — l'hôte d'écoute, le nom de la
// machine et localhost, sur le port d'écoute (même règle que browser_server).
func (w *webBridge) hostAllowed(r *http.Request) bool {
	h, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		h, port = strings.Trim(r.Host, "[]"), ""
	}
	if !w.hosts[strings.ToLower(strings.Trim(h, "[]"))] {
		return false
	}
	return port == "" || w.port == "" || port == w.port
}

// webUIRegenerateToken : nouveau jeton — l'ancien lien ne marche plus, les sessions et flux des
// navigateurs connectés sont révoqués. Fenêtre native seulement. Serveur arrêté : le fichier est
// supprimé, le prochain démarrage en tire un nouveau.
func (a *App) webUIRegenerateToken() map[string]any {
	w := &a.webui
	w.mu.Lock()
	defer w.mu.Unlock()
	tok, err := rotateDesktopWebToken()
	if w.bridge != nil {
		w.bridge.setToken(tok)
	}
	if err != nil {
		w.state.Msg = err.Error()
		go a.changed()
		return map[string]any{"ok": true, "msg": err.Error()}
	}
	return map[string]any{"ok": true}
}
