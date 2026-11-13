//go:build windows

package platform

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Le .exe Windows est une application graphique (-H windowsgui : pas de
// console qui s'ouvre au double-clic). Deux conséquences, traitées ici :
//   - en ligne de commande (claude-launcher-ng2 version, backends…), la sortie
//     irait nulle part : AttachParentConsole la rattache à la console de
//     l'invite qui l'a lancé ;
//   - chaque outil lancé (git, docker, kubectl, ssh) ouvrirait sa propre
//     fenêtre de console : NoWindow le lance sans console (CREATE_NO_WINDOW).
//     Pas de console allouée puis masquée : avec Windows Terminal comme
//     terminal par défaut (Windows 11), elle resterait visible et vide.

var (
	kernel32   = windows.NewLazySystemDLL("kernel32.dll")
	procAttach = kernel32.NewProc("AttachConsole")
)

const attachParentProcess = ^uintptr(0) // (DWORD)-1

// usable : la poignée standard mène quelque part (tube, fichier, console).
func usable(h windows.Handle) bool {
	if h == 0 || h == windows.InvalidHandle {
		return false
	}
	_, err := windows.GetFileType(h)
	return err == nil
}

// AttachParentConsole : sortie et erreur vers la console du processus parent,
// si elles ne sont pas déjà redirigées (tube, fichier).
func AttachParentConsole() {
	out, _ := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	errh, _ := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if usable(out) && usable(errh) {
		return
	}
	if r, _, _ := procAttach.Call(attachParentProcess); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		if !usable(out) {
			os.Stdout = f
		}
		if !usable(errh) {
			os.Stderr = f
		}
	}
	if !usable(windows.Handle(os.Stdin.Fd())) {
		if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = f
		}
	}
}

// noWindowFlags : CREATE_NO_WINDOW, l'outil n'a pas de fenêtre de console.
const noWindowFlags = windows.CREATE_NO_WINDOW

// NoWindow : c sera lancé sans fenêtre de console (outil en ligne de commande
// lancé par l'application graphique ; ses entrées/sorties sont des tuyaux).
func NoWindow(c *exec.Cmd) *exec.Cmd {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.HideWindow = true
	c.SysProcAttr.CreationFlags |= noWindowFlags
	return c
}
