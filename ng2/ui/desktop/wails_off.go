//go:build !windows && !wails

package desktop

import "errors"

// Binaire sans Wails (Linux et macOS par défaut : pas de cgo ni de WebKitGTK) :
// « desktop » sert l'interface dans le navigateur (mode web).
const wailsAvailable = false

func runWails() error {
	return errors.New("interface native non compilée (go build -tags wails,desktop,production)")
}
