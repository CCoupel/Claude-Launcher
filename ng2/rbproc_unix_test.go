//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// rbOwnGroup : le processus lance son propre groupe, tué en entier à l'annulation ou à la fin.
func rbOwnGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
}

func rbKillGroup(c *exec.Cmd) {
	if c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
