//go:build !linux

package core

// StaleProcs : sans objet hors Linux (pas de coque tmux).
func StaleProcs() []StaleProc { return []StaleProc{} }

// ProcArgs : sans objet hors Linux.
func ProcArgs(int) []string { return nil }

// TerminateStale : sans objet hors Linux.
func TerminateStale([]StaleProc, func()) map[int][]string { return map[int][]string{} }
