//go:build !windows

package core

import "syscall"

// xdevErr : l'erreur de « rename entre deux systèmes de fichiers » de cette plateforme.
func xdevErr() error { return syscall.EXDEV }
