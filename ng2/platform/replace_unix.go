//go:build !windows

package platform

import "os"

const replaceRetries = false

// SyncDir : fsync du dossier (rend le rename durable).
func SyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
