package core

import (
	"os"
	"path/filepath"
)

func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	d := filepath.Join(base, "claude-launcher-ng2")
	_ = os.MkdirAll(d, 0o755)
	return d
}
