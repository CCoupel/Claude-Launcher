package desktop

import (
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"claude-launcher-ng2/core"
)

// frontendFS : l'interface (HTML, CSS, JS, xterm.js), embarquée dans le binaire.
//
//go:embed all:frontend
var frontendFS embed.FS

// Run : sous-commande « desktop » (Windows : lancement sans argument).
// Options : --web (interface dans le navigateur), --addr HÔTE:PORT (mode web,
// 127.0.0.1:0 par défaut), --no-open (mode web : ne pas ouvrir le navigateur).
// Sans Wails dans le binaire (Linux compilé sans la balise « wails »), le mode
// web est pris d'office.
func Run(args []string) error {
	web, open, addr, addrSet := false, true, "127.0.0.1:0", false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--web":
			web = true
		case a == "--no-open":
			open = false
		case a == "--addr" && i+1 < len(args):
			addr, addrSet = args[i+1], true
			i++
		case strings.HasPrefix(a, "--addr="):
			addr, addrSet = strings.TrimPrefix(a, "--addr="), true
		case a == "--url" && i+1 < len(args):
			webURLFlag = args[i+1]
			i++
		case strings.HasPrefix(a, "--url="):
			webURLFlag = strings.TrimPrefix(a, "--url=")
		case a == "--launch" || a == "--no-update":
		default:
			return fmt.Errorf("desktop : option inconnue %q (--web, --addr HÔTE:PORT, --url ADRESSE_PUBLIQUE, --no-open)", a)
		}
	}
	if !web && !wailsAvailable {
		fmt.Fprintln(os.Stderr, "interface native non compilée dans ce binaire (balise wails) : interface web")
		web = true
	}
	if web {
		if s, err := core.DesktopWebSetting(); !addrSet && err == nil && !s.Off { // --addr l'emporte ; sinon desktop_web.listen s'il est fixé
			addr = s.Listen
		}
		if addrSet {
			if n := addrLANNotice(addr, core.DesktopWebAllowsLAN()); n != "" {
				fmt.Fprintln(os.Stderr, "⚠ "+n)
			}
		}
		return RunWeb(addr, open)
	}
	return runWails()
}

// Bridge : objet lié à l'interface Wails (window.go.desktop.Bridge.Call).
type Bridge struct{ app *App }

// Call : appel de l'interface ; args et réponse en JSON.
func (b *Bridge) Call(method, args string) (string, error) {
	res, err := b.app.Dispatch(method, json.RawMessage(args))
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(res)
	return string(out), err
}

// addrLANNotice : avertissement d'un « --addr » hors de la boucle locale quand desktop_web.allow_lan n'est
// pas posé (l'option de la ligne de commande reste un choix explicite, permis) ; "" sinon.
func addrLANNotice(addr string, allowLAN bool) string {
	h, _, err := net.SplitHostPort(addr)
	if err != nil || allowLAN || h == "localhost" || net.ParseIP(h) != nil && net.ParseIP(h).IsLoopback() {
		return ""
	}
	return "--addr " + addr + " écoute hors de la boucle locale sans desktop_web.allow_lan : choix explicite de la ligne de commande, accepté — l'interface complète sera atteignable depuis le réseau (HTTP clair, jeton seul)"
}
