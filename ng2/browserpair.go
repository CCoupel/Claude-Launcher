package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ── Appariement du navigateur du hub (#106) ──────────────────────────────
//
// L'extension Chrome peut être connectée à plusieurs navigateurs (autres PC) ;
// le bon est celui de la machine du launcher. Seul lui atteint le serveur web
// du hub (127.0.0.1) : la session claude essaie chaque navigateur connecté sur
// /__claude-launcher__/pair?device=<deviceId>&token=<jeton du hub>, et le
// serveur enregistre le deviceId qui y arrive avec le bon jeton. Le deviceId
// est stable d'un lancement de Chrome à l'autre (propre au profil Chrome et à
// l'installation de l'extension) ; les noms (« Browser 1 ») sont renumérotés.
//
// Les sessions reçoivent la consigne dans CL_BROWSER_HINT, affichée au
// démarrage de chaque session par un hook SessionStart du settings.json
// utilisateur : rien à faire côté template.

const (
	browserPairPath = browserHealthPath + "/pair"
	browserHintVar  = "CL_BROWSER_HINT"
	browserHookMark = "# claude-launcher:browser"
)

var deviceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,80}$`)

func browserTokenPath() string  { return filepath.Join(cacheDir(), "browser-token") }
func browserDevicePath() string { return filepath.Join(cacheDir(), "browser-device") }

// browserToken : jeton du hub, tiré une fois et gardé (les sessions déjà
// ouvertes le connaissent).
func browserToken() string {
	if b, err := os.ReadFile(browserTokenPath()); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 16 {
			return t
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	t := hex.EncodeToString(buf)
	_ = os.MkdirAll(cacheDir(), 0o755)
	if err := os.WriteFile(browserTokenPath(), []byte(t+"\n"), 0o600); err != nil {
		return ""
	}
	return t
}

// browserDevice : deviceId du navigateur du hub et sa source ; hubs.<machine>
// .browser_device l'emporte sur l'appariement enregistré.
func browserDevice(f backendsFile, host string) (id, source string) {
	if k, h, ok := pickHub(f.Hubs, host); ok && h.BrowserDevice != "" {
		return h.BrowserDevice, "hubs." + k + ".browser_device"
	}
	if b, err := os.ReadFile(browserDevicePath()); err == nil {
		if id := strings.TrimSpace(string(b)); deviceIDRe.MatchString(id) {
			return id, "appariement"
		}
	}
	return "", ""
}

// servePair : enregistre le deviceId arrivé avec le bon jeton.
func (s fileServer) servePair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dev, tok := r.URL.Query().Get("device"), r.URL.Query().Get("token")
	page := func(code int, title, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>%s</title><style>body{font:16px system-ui,sans-serif;margin:24px}</style><h1>%s</h1><p>%s</p>",
			html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg))
	}
	want := s.token()
	if want == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(want)) != 1 {
		page(http.StatusForbidden, "✗ autre launcher", "Ce navigateur atteint un launcher qui n'est pas celui de cette session : ce n'est pas le navigateur du hub.")
		return
	}
	if !deviceIDRe.MatchString(dev) {
		page(http.StatusBadRequest, "✗ deviceId invalide", "deviceId attendu : celui donné par list_connected_browsers.")
		return
	}
	_ = os.MkdirAll(cacheDir(), 0o755)
	if err := os.WriteFile(browserDevicePath(), []byte(dev+"\n"), 0o644); err != nil {
		page(http.StatusInternalServerError, "✗ enregistrement impossible", err.Error())
		return
	}
	page(http.StatusOK, "✓ navigateur du hub "+hubName(), "deviceId "+dev+" enregistré : les prochaines sessions claude le sélectionneront directement.")
}

// browserHint : consigne donnée à chaque session claude (CL_BROWSER_HINT), en
// une ligne (environnement tmux).
func browserHint(c serverConf, device, devSource, token, machine string, ps []BrowserPath) string {
	var b strings.Builder
	b.WriteString("Claude-Launcher, navigateur (Chrome via claude-in-chrome). ")
	if c.Off || c.Base == "" {
		b.WriteString("Fichiers du projet dans Chrome : traduire leur chemin avec la correspondance CL_BROWSER_PATH_MAP=" + browserPathJSON(ps) + ". ")
	} else {
		b.WriteString("Fichiers du projet dans Chrome : toujours par l'URL " + c.Base + "<chemin absolu du fichier> (segments encodés, ex. " +
			c.Base + "/share/MFS/ROOT/x/maquette.html), jamais en file:// (refusé par l'extension) ; le serveur web du launcher les sert, un dossier en / affiche sa liste. ")
	}
	switch {
	case device != "":
		b.WriteString("Navigateur du hub " + machine + " : deviceId " + device + " (choisi par l'utilisateur, source " + devSource + "). " +
			"Avant la première action Chrome de la session, appeler select_browser avec ce deviceId. " +
			"S'il est absent de list_connected_browsers, ne pas en prendre un autre : signaler à l'utilisateur que le Chrome de " + machine + " est fermé ou déconnecté")
		if !c.Off && c.Base != "" && token != "" {
			b.WriteString(" (extension réinstallée : refaire l'appariement, " + c.Base + browserPairPath + "?device=<deviceId>&token=" + token + ")")
		}
		b.WriteString(".")
	case !c.Off && c.Base != "" && token != "":
		b.WriteString("Navigateur du hub " + machine + " pas encore identifié. Avant la première action Chrome, appariement : pour chaque navigateur de list_connected_browsers, " +
			"select_browser(deviceId) puis ouvrir dans un nouvel onglet " + c.Base + browserPairPath + "?device=<son deviceId>&token=" + token + ". " +
			"La page « ✓ navigateur du hub » désigne le bon (deviceId enregistré pour les sessions suivantes) : le garder sélectionné et l'indiquer à l'utilisateur. " +
			"Plusieurs ✓ (plusieurs profils Chrome) : demander lequel à l'utilisateur (AskUserQuestion), puis rouvrir la page d'appariement dans celui choisi. Aucun ✓ : demander à l'utilisateur.")
	default:
		b.WriteString("Navigateur du hub non identifié : si plusieurs navigateurs sont connectés, demander à l'utilisateur lequel utiliser.")
	}
	return b.String()
}

// browserEnvVars : variables du navigateur pour les sessions (tmux du hub et
// des conteneurs).
func browserEnvVars() []envVar {
	f := readBackendsFile()
	machine := hubName()
	ps, _, _ := resolveBrowserPaths(f, machine, knownRoots(), onWSL())
	c, err := resolveBrowserServer(f, machine)
	if err != nil {
		c.Off = true
	}
	tok := ""
	if !c.Off {
		tok = browserToken()
	}
	dev, src := browserDevice(f, machine)
	return []envVar{
		{browserPathVar, browserPathJSON(ps)},
		{browserHintVar, browserHint(c, dev, src, tok, machine, ps)},
	}
}

// ── Hook SessionStart ─────────────────────────────────────────────────────

// browserHookCommand : affiche CL_BROWSER_HINT au démarrage de la session (et
// après /clear ou une compaction) ; rien hors launcher. Pur sh : marche aussi
// dans les conteneurs, sans le binaire du launcher.
const browserHookCommand = `[ -n "$CL_BROWSER_HINT" ] && printf '%s\n' "$CL_BROWSER_HINT"; true ` + browserHookMark

// syncBrowserHook : hook SessionStart du launcher dans <dir>/settings.json,
// posé une fois ; les autres hooks sont gardés. JSON illisible : laissé tel quel.
func syncBrowserHook(dir string) error {
	path := filepath.Join(dir, "settings.json")
	doc := map[string]json.RawMessage{}
	mode := os.FileMode(0o644)
	if b, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &doc); err != nil {
				return err
			}
		}
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	hooks := map[string]json.RawMessage{}
	if raw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return err
		}
	}
	var groups []map[string]any
	if raw, ok := hooks["SessionStart"]; ok {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return err
		}
	}
	// groupes du launcher retirés (version précédente de la commande), puis le bon
	kept := groups[:0]
	found := false
	for _, g := range groups {
		mine, same := false, false
		if hs, ok := g["hooks"].([]any); ok {
			for _, h := range hs {
				if hm, ok := h.(map[string]any); ok {
					if cmd, _ := hm["command"].(string); strings.Contains(cmd, browserHookMark) {
						mine, same = true, cmd == browserHookCommand && len(hs) == 1
					}
				}
			}
		}
		if mine && same && !found {
			found = true
			kept = append(kept, g)
		} else if !mine {
			kept = append(kept, g)
		}
	}
	if found && len(kept) == len(groups) {
		return nil // déjà en place
	}
	if !found {
		kept = append(kept, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": browserHookCommand}}})
	}
	enc := func(v any) (json.RawMessage, error) {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		if err := e.Encode(v); err != nil {
			return nil, err
		}
		return json.RawMessage(bytes.TrimSpace(b.Bytes())), nil
	}
	var err error
	if hooks["SessionStart"], err = enc(kept); err != nil {
		return err
	}
	if doc["hooks"], err = enc(hooks); err != nil {
		return err
	}
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".cl-tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), mode); err != nil {
		return err
	}
	_ = os.Chmod(tmp, mode)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// printBrowserDevice : section de « claude-launcher-ng2 backends ».
func printBrowserDevice() {
	dev, src := browserDevice(readBackendsFile(), hubName())
	fmt.Printf("\nNavigateur du hub (machine %s) :\n", hubName())
	if dev == "" {
		fmt.Println("  pas encore identifié : la première session claude qui utilise Chrome fait l'appariement")
		return
	}
	fmt.Printf("  deviceId %s (%s)\n", dev, src)
	if src == "appariement" {
		fmt.Println("  refaire l'appariement : rm " + browserDevicePath())
	}
}
