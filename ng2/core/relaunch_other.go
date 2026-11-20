//go:build !linux

package core

import "time"

// FindStaleProcs : sans objet hors Linux (pas de coque tmux).
func FindStaleProcs(string, string, string) []StaleProc { return []StaleProc{} }

// StaleProcs : sans objet hors Linux.
func StaleProcs() []StaleProc { return []StaleProc{} }

// ProcArgs : sans objet hors Linux.
func ProcArgs(int) []string { return nil }

// StopProcs : sans objet hors Linux.
func StopProcs([]StaleProc, time.Duration) {}

// TermProcs : sans objet hors Linux.
func TermProcs([]StaleProc) {}

// KillSurvivors : sans objet hors Linux.
func KillSurvivors([]StaleProc) {}
