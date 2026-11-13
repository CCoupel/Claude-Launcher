//go:build linux || darwin

package platform

import "syscall"

// ExecBinary : remplace le processus courant par le binaire argv0 (execve).
func ExecBinary(argv0 string, argv []string, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}
