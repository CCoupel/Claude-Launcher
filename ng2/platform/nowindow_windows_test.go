//go:build windows

package platform

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// Sous Windows, les outils lancés par l'application graphique n'ont pas de
// fenêtre de console (CREATE_NO_WINDOW), et leur sortie arrive quand même.
func TestNoWindow(t *testing.T) {
	c := NoWindow(exec.Command("cmd", "/c", "echo", "ok"))
	if c.SysProcAttr == nil || c.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 || !c.SysProcAttr.HideWindow {
		t.Fatalf("drapeaux : %+v", c.SysProcAttr)
	}
	out, err := c.Output()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("sortie : %q %v", out, err)
	}
	// drapeaux déjà posés : gardés
	c = exec.Command("cmd", "/c", "exit")
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	NoWindow(c)
	if c.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("drapeaux existants perdus")
	}
}
