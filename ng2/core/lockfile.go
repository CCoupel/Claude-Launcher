package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ── Écritures sûres des fichiers partagés entre launchers (#176) ──────────
//
// Plusieurs launchers (un hub, un exe Windows, un pod) partagent la racine : projects.json, settings.json,
// env.yaml de projet sont relus, modifiés puis réécrits. Verrou court autour de la séquence, écriture atomique
// (fichier temporaire propre à l'écriture puis renommage) partout.
//
// Le verrou est un fichier « <cible>.lock » créé en exclusivité (O_EXCL) : atomique sur MooseFS, NFS et les
// partages Windows, là où flock n'est pas fiable ; un verrou plus vieux que fileLockStale (processus mort) est repris.

var (
	fileLockStale   = 20 * time.Second // au-delà, le verrou est celui d'un processus mort
	fileLockTimeout = 30 * time.Second // attente maximale du verrou
)

// WithFileLock exécute fn en tenant le verrou de target. Erreur si le verrou reste pris au-delà de fileLockTimeout.
func WithFileLock(target string, fn func() error) error {
	lock := target + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		return err
	}
	deadline := time.Now().Add(fileLockTimeout)
	for wait := 2 * time.Millisecond; ; {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + " " + LauncherID() + "\n")
			f.Close()
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if st, serr := os.Stat(lock); serr == nil && time.Since(st.ModTime()) > fileLockStale {
			_ = os.Remove(lock) // processus mort : verrou repris
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("verrou de %s pris depuis plus de %v : un autre launcher écrit ce fichier", filepath.Base(target), fileLockTimeout)
		}
		time.Sleep(wait)
		if wait < 40*time.Millisecond {
			wait *= 2
		}
	}
	defer os.Remove(lock)
	return fn()
}

// WriteFileAtomic : data dans path par fichier temporaire (nom unique) puis renommage — jamais de fichier à moitié écrit,
// jamais de temporaire partagé entre deux écritures simultanées.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	r := make([]byte, 4)
	_, _ = rand.Read(r)
	tmp := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), hex.EncodeToString(r))
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	_ = os.Chmod(tmp, perm)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// createExclusive : crée path avec data s'il n'existe pas (atomique) ; created faux : il existait déjà.
func createExclusive(path string, data []byte, perm os.FileMode) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return true, werr
}
