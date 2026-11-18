package desktop

import (
	"claude-launcher-ng2/internal/tun"
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
	Lan    bool   `json:"lan"`  // écoute hors de la boucle locale active (confirmée par allow_lan)
	Warn   string `json:"warn"` // avertissement qui l'accompagne
	Msg    string `json:"msg"`  // erreur : écoute impossible, réglage refusé, jeton non conservé
	// Enabled : valeur effective du commutateur desktop_web.enabled (#183) ; Server : « desktop --web »
	// (l'interface est le processus lui-même : état toujours on, commutateur verrouillé).
	Enabled bool `json:"enabled"`
	Server  bool `json:"server"`
	// URL : adresse publique effective (--url > CL_WEB_URL > desktop_web.url), vide sinon (#171)
	URL string `json:"url"`
	// Access : mode d'accès effectif, « token » ou « none » (aucun jeton ni cookie, LOT-4.4)
	Access string `json:"access"`
}

// webUI : serveur de l'interface attaché à l'App (a.webui).
type webUI struct {
	mu     sync.Mutex
	srv    *http.Server
	bridge *webBridge
	listen string // adresse réellement écoutée (vide : arrêté)
	pubKey string // adresse publique du serveur en marche : un changement le redémarre (#171)
	last   string // dernière adresse écoutée, même arrêtée : un changement d'adresse renouvelle le jeton
	state  WebUIState
}

// desktopWebTokenPath : jeton de l'interface web, tiré une fois par poste.
func desktopWebTokenPath() string { return filepath.Join(core.CacheDir(), "desktop-web-token") }

// removeDesktopWebToken : supprime le fichier du jeton (passage en mode « none ») ; absent : sans effet. Un échec de
// suppression (droits) n'empêche pas le démarrage : le fichier n'est plus lu.
func removeDesktopWebToken() {
	_ = os.Remove(desktopWebTokenPath())
	_ = os.Remove(desktopWebTokenPath() + ".new")
}

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

// rotateDesktopWebToken : jeton remplacé et réécrit sur disque (fichier temporaire 0600 puis
// renommage : jamais d'état intermédiaire). Si le disque refuse, l'ancien fichier est retiré s'il le
// peut ; l'erreur dit que l'ancien jeton pourrait être réutilisé à la relance.
func rotateDesktopWebToken() (string, error) {
	p := desktopWebTokenPath()
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".new"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.WriteString(t + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			if err = os.Rename(tmp, p); err == nil {
				return t, nil
			}
		}
		_ = os.Remove(tmp)
	}
	if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return t, fmt.Errorf("jeton non conservé (%v) et ancien fichier non retiré (%v) : l'ancien jeton pourrait être réutilisé à la relance — corriger les droits de %s", err, rerr, filepath.Dir(p))
	}
	return t, fmt.Errorf("jeton non conservé (%v) : il ne survivra pas à une relance", err)
}

// desktopWebListenPath : dernière adresse d'écoute du serveur de l'interface (0600), gardée avec le jeton
// pour renouveler celui-ci quand l'adresse change entre deux lancements.
func desktopWebListenPath() string { return filepath.Join(core.CacheDir(), "desktop-web-listen") }

func lastDesktopWebListen() string {
	b, err := os.ReadFile(desktopWebListenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveDesktopWebListen(l string) {
	p := desktopWebListenPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(l+"\n"), 0o600)
}

// WebUI : état courant du serveur de l'interface.
func (a *App) WebUI() WebUIState {
	a.webui.mu.Lock()
	defer a.webui.mu.Unlock()
	if a.web { // « desktop --web » : l'interface est le processus ; état on, commutateur verrouillé (#183)
		s := a.webui.state
		s.State, s.Enabled, s.Server = "on", true, true
		return s
	}
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
		w.state = WebUIState{State: "error", Msg: err.Error(), Enabled: conf.Enabled}
	case conf.Off: // éteinte (commutateur ou pas d'adresse) : l'adresse saisie reste affichée
		a.stopWebUILocked()
		w.state = WebUIState{State: "off", Listen: conf.Kept}
	default:
		pub, _, perr := effectivePublicURL(conf)
		if perr != nil { // adresse publique de forme invalide : interface éteinte, raison dite
			a.stopWebUILocked()
			w.state = WebUIState{State: "error", Listen: conf.Listen, Enabled: true, Msg: perr.Error()}
			break
		}
		access, aerr := core.EffectiveWebAccess(conf)
		if aerr == nil {
			if m := core.AccessNoneRefusal(access, conf.Loopback, pub); m != "" {
				aerr = errors.New(m) // garde : « none » seulement en boucle locale ou derrière une adresse publique https
			}
		}
		if aerr != nil {
			a.stopWebUILocked()
			w.state = WebUIState{State: "error", Listen: conf.Listen, Enabled: true, Msg: aerr.Error(), Access: access}
			break
		}
		if w.srv != nil && w.listen == conf.Listen && w.pubKey == publicOrigin(pub)+"|"+access {
			return // déjà à jour
		}
		a.stopWebUILocked()
		a.startWebUILocked(conf, pub, access)
	}
	go a.changed()
}

func (a *App) startWebUILocked(conf core.DesktopWebConf, pub *core.PublicURL, access string) {
	w := &a.webui
	ln, err := net.Listen("tcp", conf.Listen)
	if err != nil {
		w.state = WebUIState{State: "error", Listen: conf.Listen, Enabled: true, Msg: "écoute impossible sur " + conf.Listen + " : " + err.Error()}
		return
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	var tok string
	var terr error
	if access == core.AccessNone { // mode « none » : le jeton n'est ni tiré, ni lu, ni écrit — et celui d'un mode « token » précédent est supprimé
		removeDesktopWebToken()
	} else {
		if w.last == "" {
			w.last = lastDesktopWebListen() // dernier lancement
		}
		if w.last != "" && w.last != conf.Listen { // adresse changée (surtout hors boucle locale) : nouveau jeton
			tok, terr = rotateDesktopWebToken()
		} else {
			tok, terr = desktopWebToken()
		}
	}
	b := &webBridge{app: a, token: tok, subs: map[chan []byte]bool{}, port: port, hosts: conf.AllowedHosts(core.HubHost()), public: pub, access: access}
	srv := newHTTPServer(b.handler())
	w.srv, w.bridge, w.listen, w.pubKey = srv, b, conf.Listen, publicOrigin(pub)+"|"+access
	if access != core.AccessNone {
		w.last = conf.Listen
		saveDesktopWebListen(conf.Listen)
	}
	w.state = WebUIState{State: "on", Listen: conf.Listen, Lan: !conf.Loopback, Warn: conf.Warn, Enabled: true, URL: publicOrigin(pub), Access: access}
	if terr != nil {
		w.state.Msg = terr.Error()
	}
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
	w.srv, w.bridge, w.listen, w.pubKey = nil, nil, "", ""
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
	var pub *core.PublicURL
	if w.bridge != nil {
		pub = w.bridge.public
	}
	var tok string
	noTok := false
	if w.bridge != nil {
		tok = w.bridge.tokenNow() // le jeton du serveur : un seul tirage
		noTok = w.bridge.access == core.AccessNone
	}
	w.mu.Unlock()
	if !on {
		return map[string]any{"ok": false, "msg": "l'interface web est désactivée (Réglages › Ce poste › Interface web)"}
	}
	suffix := "/?t=" + tok
	if noTok {
		suffix = "/"
	}
	if pub != nil { // lien d'amorçage : l'adresse publique
		openURL(pub.Origin + suffix)
		return map[string]any{"ok": true}
	}
	h, port, _ := net.SplitHostPort(listen)
	if h == "" || h == "0.0.0.0" || h == "::" {
		h = "localhost"
	}
	openURL("http://" + net.JoinHostPort(h, port) + suffix)
	return map[string]any{"ok": true}
}

// closeAll : ferme les flux d'évènements des navigateurs connectés.
func (w *webBridge) closeAll() {
	w.subMu.Lock()
	defer w.subMu.Unlock()
	for ch := range w.subs {
		delete(w.subs, ch)
		delete(w.subSess, ch)
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

// webUIRegen : nouveau jeton — l'ancien lien ne marche plus, les sessions et flux des
// navigateurs connectés sont révoqués. Fenêtre native seulement. Serveur arrêté : le fichier est
// supprimé, le prochain démarrage en tire un nouveau.
func (a *App) webUIRegen() map[string]any {
	w := &a.webui
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state.Access == core.AccessNone { // aucun jeton : rien à régénérer
		return map[string]any{"ok": false, "msg": "accès sans jeton : aucun jeton à régénérer"}
	}
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

// webURLFlag : option --url de la ligne de commande (#171) ; la plus prioritaire.
var webURLFlag = tun.Of("")

// effectivePublicURL : adresse publique effective, par priorité : option --url > variable CL_WEB_URL >
// desktop_web.url (conf.URL). Aucune adresse : nil, sans erreur. Forme invalide : erreur de réglage.
func effectivePublicURL(conf core.DesktopWebConf) (*core.PublicURL, string, error) {
	raw, src := strings.TrimSpace(webURLFlag.Get()), "l'option --url"
	if raw == "" {
		raw, src = strings.TrimSpace(os.Getenv("CL_WEB_URL")), "la variable CL_WEB_URL"
	}
	if raw == "" {
		raw, src = conf.URL, "desktop_web.url"
	}
	if raw == "" {
		return nil, "", nil
	}
	p, err := core.ParsePublicURL(raw)
	if err != nil {
		return nil, raw, fmt.Errorf("%s (%s)", err.Error(), src)
	}
	return &p, raw, nil
}

// publicOrigin : adresse publique pour l'instantané et la comparaison ("" : aucune).
func publicOrigin(p *core.PublicURL) string {
	if p == nil {
		return ""
	}
	return p.Origin
}

// relaunchArgs : arguments de la relance du mode serveur après une mise à jour (même adresse, même adresse publique).
func relaunchArgs(bound string) []string {
	a := []string{"desktop", "--web", "--addr", bound, "--no-open"}
	if webURLFlag.Get() != "" {
		a = append(a, "--url", webURLFlag.Get())
	}
	return a
}
