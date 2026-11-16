package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	fileLockStale   = 60 * time.Second // très au-dessus de la durée d'une section (quelques ms) : un verrou plus vieux est celui d'un processus mort
	fileLockTimeout = 90 * time.Second // attente maximale du verrou (au-delà de fileLockStale : un verrou abandonné est repris avant)
)

// newLockToken : identifiant unique d'un détenteur (pid + aléa) ; le fichier verrou le porte, la libération ne
// supprime que le verrou qui le contient.
func newLockToken() string {
	r := make([]byte, 6)
	_, _ = rand.Read(r)
	return strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(r)
}

// lockOwnedBy : le fichier verrou porte l'identifiant token.
func lockOwnedBy(path, token string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.HasPrefix(string(b), token)
}

// reclaimStale : reprise d'un fichier périmé (verrou, témoin) SANS jamais retirer un fichier vivant : le retrait n'a lieu
// que sous un petit verrou de reprise (« <fichier>.reclaim », création exclusive), après avoir revérifié la péremption.
// Plusieurs processus qui constatent le même périmé : un seul le retire ; ceux dont le constat est dépassé (le
// fichier a été recréé entre-temps) le voient frais sous le verrou de reprise et n'y touchent pas. Rend vrai si le
// fichier n'est plus là.
func reclaimStale(path string, stale time.Duration) (bool, error) {
	guard := path + ".reclaim"
	token := newLockToken()
	created, err := createExclusive(guard, []byte(token+"\n"), 0o644)
	if err != nil {
		return false, err
	}
	if !created {
		if st, serr := os.Stat(guard); serr == nil && time.Since(st.ModTime()) > 10*time.Second {
			_ = os.Remove(guard) // verrou de reprise d'un processus mort (la section dure des microsecondes)
		}
		return false, nil
	}
	defer func() {
		if lockOwnedBy(guard, token) {
			_ = removeRetry(guard)
		}
	}()
	st, err := os.Stat(path)
	if err != nil {
		return true, nil // déjà parti
	}
	if time.Since(st.ModTime()) <= stale {
		return false, nil // recréé depuis le constat : vivant
	}
	if rerr := removeRetry(path); rerr != nil {
		return false, rerr
	}
	return true, nil
}

// WithFileLock exécute fn en tenant le verrou de target. Erreur si le verrou reste pris au-delà de fileLockTimeout.
// La péremption se mesure sur la date du fichier telle que la voit le serveur de fichiers : les horloges des
// launchers n'y interviennent qu'à travers elle (écart d'horloge à rester très inférieur à fileLockStale).
func WithFileLock(target string, fn func() error) error {
	lock := target + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		return err
	}
	token := newLockToken()
	deadline := time.Now().Add(fileLockTimeout)
	var lastErr error // dernière cause d'échec (accès refusé, suppression impossible) : jointe à l'erreur d'échéance
	for wait := 2 * time.Millisecond; ; {
		created, err := createExclusive(lock, []byte(token+" "+LauncherID()+"\n"), 0o644)
		if err != nil && !lockBusyErr(err) {
			return fmt.Errorf("verrou de %s : création de %s impossible : %w", filepath.Base(target), filepath.Base(lock), err)
		}
		if err != nil {
			lastErr = err
		}
		if created {
			break
		}
		if st, serr := os.Stat(lock); serr == nil && time.Since(st.ModTime()) > fileLockStale {
			ok, rerr := reclaimStale(lock, fileLockStale)
			if ok {
				continue // périmé retiré : nouvelle tentative tout de suite
			}
			if rerr != nil {
				lastErr = rerr
			}
		}
		if time.Now().After(deadline) {
			return lockTimeoutError(target, lastErr)
		}
		time.Sleep(wait)
		if wait < 40*time.Millisecond {
			wait *= 2
		}
	}
	defer func() {
		if lockOwnedBy(lock, token) { // jamais le verrou d'un autre (détenteur lent dont le verrou a été repris)
			_ = removeRetry(lock)
		}
	}()
	return fn()
}

// lockTimeoutError : échéance du verrou ; distingue « occupé par un autre » de « droits insuffisants » (le verrou
// périmé ne peut pas être retiré, ou le dossier n'est pas modifiable) en joignant la dernière erreur système.
func lockTimeoutError(target string, last error) error {
	name := filepath.Base(target)
	switch {
	case last != nil && errors.Is(last, os.ErrPermission):
		return fmt.Errorf("verrou de %s : droits insuffisants pour le prendre ou retirer un verrou périmé (%w) — vérifier les droits du dossier", name, last)
	case last != nil:
		return fmt.Errorf("verrou de %s pris depuis plus de %v, retrait impossible (%w)", name, fileLockTimeout, last)
	}
	return fmt.Errorf("verrou de %s pris depuis plus de %v : un autre launcher écrit ce fichier", name, fileLockTimeout)
}

// lockBusyErr : l'échec de création du verrou veut dire « occupé », pas « impossible ». Sous Windows, un fichier
// dont la suppression est en cours (poignée encore ouverte chez un lecteur concurrent) refuse sa recréation avec
// « accès refusé » : le verrou est simplement repris plus tard. Ailleurs (ou sans fichier visible), une vraie erreur.
func lockBusyErr(err error) bool {
	return runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission)
}

// removeRetry : os.Remove, réessayé brièvement sous Windows (suppression refusée tant qu'un lecteur concurrent tient
// une poignée sur le fichier : violation de partage passagère).
func removeRetry(path string) error {
	var err error
	for try := 0; try < 6; try++ {
		err = os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if runtime.GOOS != "windows" {
			return err
		}
		time.Sleep(time.Duration(5+try*10) * time.Millisecond)
	}
	return err
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
	// sous Windows, le renommage échoue brièvement (violation de partage) si un lecteur tient la cible : réessayé
	var rerr error
	for try := 0; try < 4; try++ {
		if rerr = os.Rename(tmp, path); rerr == nil {
			return nil
		}
		if runtime.GOOS != "windows" {
			break
		}
		time.Sleep(time.Duration(10+try*15) * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return rerr
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
