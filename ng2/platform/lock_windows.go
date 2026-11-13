package platform

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFile : verrou exclusif bloquant sur f (LockFileEx), rendu à la fermeture de f.
func LockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}
