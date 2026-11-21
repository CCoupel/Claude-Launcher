//go:build windows

package core

import (
	"path/filepath"
	"strings"
	"syscall"
)

// canonPath : chemin canonique pour comparer deux chemins du même dossier — noms courts 8.3 (C:\Users\RUNNER~1) développés
// (GetLongPathName), liens résolus, casse ignorée (NTFS). Un chemin qui n'existe pas est rendu nettoyé, tel quel.
func canonPath(p string) string {
	p = filepath.Clean(p)
	if u, err := syscall.UTF16PtrFromString(p); err == nil {
		buf := make([]uint16, 32768)
		if n, err := syscall.GetLongPathName(u, &buf[0], uint32(len(buf))); err == nil && n > 0 && int(n) < len(buf) {
			p = syscall.UTF16ToString(buf[:n])
		}
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(filepath.Clean(p))
}
