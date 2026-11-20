//go:build windows

package platform

import (
	"os/exec"
	"strconv"
)

// SetProcessGroup : sans objet (taskkill /T suit l'arbre).
func SetProcessGroup(*exec.Cmd) {}

// KillTree : arrête le processus de c et ses enfants (taskkill /T /F).
func KillTree(c *exec.Cmd) error {
	if c.Process == nil {
		return nil
	}
	k := NoWindow(exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)))
	if err := k.Run(); err != nil {
		return c.Process.Kill()
	}
	return nil
}
