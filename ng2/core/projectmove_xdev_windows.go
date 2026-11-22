//go:build windows

package core

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// isCrossDevice : ERROR_NOT_SAME_DEVICE (rename entre deux volumes ou vers un partage UNC, p. ex. C: → \\wsl.localhost),
// et EXDEV (valeur de syscall que l'on peut aussi rencontrer : couches de compatibilité, erreurs rejouées).
func isCrossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) || errors.Is(err, syscall.EXDEV)
}

// sameDevice : même volume (lettre de lecteur ou partage UNC).
func sameDevice(path, dest string) bool {
	a, b := filepath.VolumeName(path), filepath.VolumeName(dest)
	return strings.EqualFold(a, b)
}
