//go:build windows

package platform

import (
	"errors"

	"golang.org/x/sys/windows"
)

func replaceBusy(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// SyncDir : sans objet sous Windows.
func SyncDir(string) error { return nil }
