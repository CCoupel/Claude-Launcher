package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// CacheDirVar : dossier de cache du launcher, utilisé tel quel (état d'un launcher partageant une racine, #169).
const CacheDirVar = "CL_CACHE_DIR"

// CacheDirOverride : CL_CACHE_DIR, "" s'il n'est pas posé.
func CacheDirOverride() string { return strings.TrimSpace(os.Getenv(CacheDirVar)) }

// CacheDir :~/.cache/claude-launcher-ng2 ($XDG_CACHE_HOME ; Windows :
// %LocalAppData%\claude-launcher-ng2).
func CacheDir() string {
	if d := CacheDirOverride(); d != "" { // CL_CACHE_DIR : utilisé tel quel (#169)
		ensureDir(d)
		return d
	}
	home, _ := os.UserHomeDir()
	ucd := ""
	if runtime.GOOS == "windows" {
		ucd, _ = os.UserCacheDir()
	}
	d := filepath.Join(cacheRoot(runtime.GOOS, os.Getenv("XDG_CACHE_HOME"), home, ucd), "claude-launcher-ng2")
	ensureDir(d)
	return d
}

// ensureDir : le dossier existe ; un seul stat quand il est là (CacheDir est appelé très souvent, parfois sur
// un partage réseau), la création seulement s'il manque.
func ensureDir(d string) {
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return
	}
	_ = os.MkdirAll(d, 0o755)
}
