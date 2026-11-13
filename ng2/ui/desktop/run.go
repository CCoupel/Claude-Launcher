package desktop

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	web, open, addr := false, true, "127.0.0.1:0"
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--web":
			web = true
		case a == "--no-open":
			open = false
		case a == "--addr" && i+1 < len(args):
			addr = args[i+1]
			i++
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimPrefix(a, "--addr=")
		case a == "--launch" || a == "--no-update":
		default:
			return fmt.Errorf("desktop : option inconnue %q (--web, --addr HÔTE:PORT, --no-open)", a)
		}
	}
	if !web && !wailsAvailable {
		fmt.Fprintln(os.Stderr, "interface native non compilée dans ce binaire (balise wails) : interface web")
		web = true
	}
	if web {
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
