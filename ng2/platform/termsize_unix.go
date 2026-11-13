//go:build linux || darwin

package platform

import "golang.org/x/sys/unix"

// TermSize : lignes et colonnes du terminal fd (TIOCGWINSZ).
func TermSize(fd int) (rows, cols int, err error) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Row), int(ws.Col), nil
}
