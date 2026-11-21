//go:build !windows

package core

import (
	"errors"
	"syscall"
)

// isCrossDevice : le rename a échoué parce que source et destination sont sur deux systèmes de fichiers (EXDEV).
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
