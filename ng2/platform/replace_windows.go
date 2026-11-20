//go:build windows

package platform

const replaceRetries = true

// SyncDir : sans objet sous Windows.
func SyncDir(string) error { return nil }
