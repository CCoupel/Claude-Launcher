//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup : le processus devient chef de son groupe (arrêt de tout l'arbre : KillTree).
func SetProcessGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
}

// KillTree : SIGKILL au groupe du processus de c (lui et ses enfants : git-remote-https, ssh…).
func KillTree(c *exec.Cmd) error {
	if c.Process == nil {
		return nil
	}
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
		return c.Process.Kill()
	}
	return nil
}
