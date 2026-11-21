//go:build windows

package core

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isCrossDevice : ERROR_NOT_SAME_DEVICE (rename entre deux volumes).
func isCrossDevice(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }
