//go:build windows

package core

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// isCrossDevice : ERROR_NOT_SAME_DEVICE (rename entre deux volumes).
func isCrossDevice(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }

// sameDevice : même volume (lettre de lecteur ou partage UNC).
func sameDevice(path, dest string) bool {
	a, b := filepath.VolumeName(path), filepath.VolumeName(dest)
	return strings.EqualFold(a, b)
}
