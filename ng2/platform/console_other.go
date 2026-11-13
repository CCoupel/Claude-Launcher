//go:build !windows

package platform

// AttachParentConsole / HiddenConsole : propres à l'exécutable graphique
// Windows (console_windows.go) ; rien à faire ailleurs.
func AttachParentConsole() {}
func HiddenConsole()       {}
