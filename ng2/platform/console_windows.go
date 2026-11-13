//go:build windows

package platform

import (
	"os"

	"golang.org/x/sys/windows"
)

// Le .exe Windows est une application graphique (-H windowsgui : pas de
// console qui s'ouvre au double-clic). Deux conséquences, traitées ici :
//   - en ligne de commande (claude-launcher-ng2 version, backends…), la sortie
//     irait nulle part : AttachParentConsole la rattache à la console de
//     l'invite qui l'a lancé ;
//   - chaque outil lancé (git, docker, kubectl, ssh) ouvrirait sa propre
//     fenêtre de console : HiddenConsole donne à l'application une console
//     masquée, héritée par ces processus.

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	user32           = windows.NewLazySystemDLL("user32.dll")
	procAttach       = kernel32.NewProc("AttachConsole")
	procAlloc        = kernel32.NewProc("AllocConsole")
	procGetConsoleWn = kernel32.NewProc("GetConsoleWindow")
	procShowWindow   = user32.NewProc("ShowWindow")
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

// HiddenConsole : console masquée pour l'interface graphique, héritée par les
// outils qu'elle lance (pas de fenêtre noire à chaque git ou docker).
func HiddenConsole() {
	if w, _, _ := procGetConsoleWn.Call(); w != 0 {
		return // déjà une console (lancé depuis une invite) : rien à faire
	}
	if r, _, _ := procAlloc.Call(); r == 0 {
		return
	}
	if w, _, _ := procGetConsoleWn.Call(); w != 0 {
		_, _, _ = procShowWindow.Call(w, 0) // SW_HIDE
	}
}
