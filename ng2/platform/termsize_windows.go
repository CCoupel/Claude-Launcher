package platform

import "golang.org/x/sys/windows"

// TermSize : lignes et colonnes de la fenêtre de la console fd.
func TermSize(fd int) (rows, cols int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		return 0, 0, err
	}
	w := info.Window
	return int(w.Bottom-w.Top) + 1, int(w.Right-w.Left) + 1, nil
}

// RawMode : la coque tmux (seule utilisatrice : vignettes, aide) ne tourne pas
// sous Windows ; l'interface Windows (#111) a son propre terminal (pty.go).
func RawMode(fd int) (func(), error) { return nil, ErrUnsupported }
