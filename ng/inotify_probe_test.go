package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Sonde : inotify livre-t-il les événements sur le disque courant (/mnt/c = 9p) ?
func TestInotifyProbe(t *testing.T) {
	dir := os.Getenv("PROBE_DIR")
	if dir == "" {
		dir = ".fake-probe"
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	abs, _ := filepath.Abs(dir)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Add(abs); err != nil {
		t.Fatalf("Add: %v", err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(abs, "x.txt"), []byte("a"), 0o644)
	}()
	select {
	case ev := <-w.Events:
		t.Logf("événement reçu : %v", ev)
	case <-time.After(3 * time.Second):
		t.Skip("inotify ne livre aucun événement sur ce disque (9p : /mnt/c) : le cache d'état y utilise l'interrogation légère")
	}
}
