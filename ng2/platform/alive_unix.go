//go:build !windows

package platform

import "syscall"

// ProcessAlive : le processus pid existe sur cette machine (signal 0).
func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
