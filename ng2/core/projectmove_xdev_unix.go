//go:build !windows

package core

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// isCrossDevice : le rename a échoué parce que source et destination sont sur deux systèmes de fichiers (EXDEV).
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// sameDevice : path et le plus proche dossier existant de dest sont sur le même système de fichiers.
func sameDevice(path, dest string) bool {
	a, err := os.Stat(path)
	if err != nil {
		return true
	}
	d := filepath.Dir(dest)
	for {
		if b, err := os.Stat(d); err == nil {
			sa, ok1 := a.Sys().(*syscall.Stat_t)
			sb, ok2 := b.Sys().(*syscall.Stat_t)
			return !ok1 || !ok2 || sa.Dev == sb.Dev
		}
		up := filepath.Dir(d)
		if up == d {
			return true
		}
		d = up
	}
}
