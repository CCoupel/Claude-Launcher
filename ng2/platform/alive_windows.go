//go:build windows

package platform

// ProcessAlive : sous Windows l'existence d'un processus n'est pas sondée ici (le témoin vieillit de lui-même).
func ProcessAlive(pid int) bool { return true }
