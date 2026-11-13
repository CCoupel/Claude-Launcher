//go:build !windows

package platform

import "os/exec"

// AttachParentConsole / NoWindow : propres à l'exécutable graphique Windows
// (console_windows.go) ; rien à faire ailleurs.
func AttachParentConsole() {}

// NoWindow : sans effet hors Windows (c inchangé).
func NoWindow(c *exec.Cmd) *exec.Cmd { return c }
