//go:build linux || darwin

package platform

import (
	"os"
	"syscall"
)

// LockFile : verrou exclusif bloquant sur f (flock), rendu à la fermeture de f.
func LockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}
