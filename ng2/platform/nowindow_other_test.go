//go:build !windows

package platform

import (
	"os/exec"
	"testing"
)

// Hors Windows, NoWindow ne touche pas à la commande.
func TestNoWindowNoop(t *testing.T) {
	c := exec.Command("true")
	if NoWindow(c) != c || c.SysProcAttr != nil {
		t.Errorf("commande modifiée : %+v", c.SysProcAttr)
	}
}
