//go:build windows

package core

import "golang.org/x/sys/windows"

// xdevErr : l'erreur de « rename entre deux volumes » de cette plateforme (ERROR_NOT_SAME_DEVICE).
func xdevErr() error { return windows.ERROR_NOT_SAME_DEVICE }
