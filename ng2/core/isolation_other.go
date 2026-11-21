//go:build !windows

package core

import "path/filepath"

// canonPath : chemin canonique (liens résolus) pour la comparaison de la garde d'isolation.
func canonPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}
