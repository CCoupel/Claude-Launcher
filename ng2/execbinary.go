package main

import "syscall"

// execBinary : remplace le processus courant par le binaire argv0 (execve).
func ExecBinary(argv0 string, argv []string, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}
