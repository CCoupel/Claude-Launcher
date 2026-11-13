package core

import (
	"os"
	"path/filepath"
	"runtime"
)

// cacheRoot : dossier des caches (logique pure).
func cacheRoot(goos, xdg, home, userCacheDir string) string {
	switch {
	case xdg != "":
		return xdg
	case goos == "windows" && userCacheDir != "":
		return userCacheDir
	}
	return filepath.Join(home, ".cache")
}

// CacheDir :~/.cache/claude-launcher-ng2 ($XDG_CACHE_HOME ; Windows :
// %LocalAppData%\claude-launcher-ng2).
func CacheDir() string {
	home, _ := os.UserHomeDir()
	ucd := ""
	if runtime.GOOS == "windows" {
		ucd, _ = os.UserCacheDir()
	}
	d := filepath.Join(cacheRoot(runtime.GOOS, os.Getenv("XDG_CACHE_HOME"), home, ucd), "claude-launcher-ng2")
	_ = os.MkdirAll(d, 0o755)
	return d
}
