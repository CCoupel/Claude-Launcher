package platform

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ── Sauvegarde du binaire avant mise à jour (#218) ───────────────────────
//
// Avant de remplacer l'exe, le binaire courant devient <exe>.bak-v<version> (convention de la sauvegarde du hub par
// deployer). Windows : l'exe en cours d'exécution est RENOMMÉ (swapRunningExe) ; Linux : copie atomique (le rename
// par-dessus reste le mécanisme de remplacement). UNE seule sauvegarde gardée : les autres .bak-v* sont supprimées
// après une installation réussie ; une sauvegarde verrouillée est retentée au démarrage (CleanupBackups).
// Aucun retour arrière dans l'interface : la sauvegarde est restaurée à la main.

var backupVerRe = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.+-]*$`)

// BackupPath : <exe>.bak-v<version> (« v6.5.0 » → « .bak-v6.5.0 ») ; "" si la version n'est pas utilisable dans un nom
// de fichier (vide, « dev »…).
func BackupPath(exe, version string) string {
	v := strings.TrimPrefix(version, "v")
	if !backupVerRe.MatchString(v) || v == "dev" || strings.Contains(v, "..") {
		return ""
	}
	return exe + ".bak-v" + v
}

// backups : sauvegardes .bak-v* à côté de exe.
func backups(exe string) []string {
	m, _ := filepath.Glob(globEscape(exe) + ".bak-v*")
	sort.Strings(m)
	return m
}

// BackupExe : nom de la sauvegarde de exe pour la version donnée (alias de BackupPath).
func BackupExe(exe, version string) string { return BackupPath(exe, version) }

// PurgeBackups : supprime les .bak-v* de exe sauf celle à garder (une seule sauvegarde gardée), désignée par sa
// version (« v6.5.0 ») ou par son chemin ; rend celles qui restent (verrouillées : retentées au démarrage).
func PurgeBackups(exe, keep string) []string {
	if !strings.HasPrefix(keep, exe+".bak-v") {
		keep = BackupPath(exe, keep)
	}
	return purgeKeepPath(exe, keep)
}

func purgeKeepPath(exe, keep string) (left []string) {
	for _, f := range backups(exe) {
		if f == keep {
			continue
		}
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			left = append(left, f)
		}
	}
	return left
}

// CleanupBackups : au démarrage, ne garde que la sauvegarde la plus récente (date) ; une sauvegarde verrouillée est
// retentée en tâche de fond pendant une dizaine de secondes.
func CleanupBackups() {
	exe, err := SelfExe()
	if err != nil || len(backups(exe)) < 2 {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			if len(purgeKeepPath(exe, newestBackup(exe))) == 0 {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func newestBackup(exe string) string {
	var best string
	var bt time.Time
	for _, f := range backups(exe) {
		if st, err := os.Stat(f); err == nil && (best == "" || st.ModTime().After(bt)) {
			best, bt = f, st.ModTime()
		}
	}
	return best
}

// copyFileAtomic : copie src vers dst (temporaire du même dossier, fsync, droits de src, rename).
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // sans effet après le rename
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), st.Mode().Perm()); err != nil {
		return err
	}
	if err := ReplaceFile(tmp.Name(), dst); err != nil {
		return err
	}
	_ = SyncDir(filepath.Dir(dst))
	return nil
}

// replaceWithCopyBackup : Linux/macOS — sauvegarde par copie atomique, puis write(dst) (rename par-dessus ;
// l'original reste intact si write échoue), puis purge des autres sauvegardes. Une sauvegarde impossible (disque
// plein…) est journalisée mais ne bloque pas la mise à jour.
func replaceWithCopyBackup(dst, version string, write func(string) error) error {
	bak := BackupPath(dst, version)
	if bak != "" {
		if _, err := os.Lstat(dst); err == nil {
			if err := copyFileAtomic(dst, bak); err != nil {
				log.Printf("sauvegarde du binaire (%s) impossible : %v", filepath.Base(bak), err)
				bak = ""
			}
		} else {
			bak = ""
		}
	}
	if err := write(dst); err != nil {
		if bak != "" { // un write qui a laissé un fichier à moitié posé : l'original est remis depuis la sauvegarde
			if !sameContent(dst, bak) {
				_ = copyFileAtomic(bak, dst)
			}
		}
		return err
	}
	if bak != "" {
		purgeKeepPath(dst, bak)
	}
	return nil
}

// swapRunningExeBackup : Windows — l'exe en cours d'exécution est renommé en sauvegarde (ou en .old si la version
// est inutilisable), write(dst) pose le nouveau (échec : dst restauré), puis purge des autres sauvegardes. Une
// sauvegarde de même nom déjà là (réinstallation) est remplacée ; verrouillée : nom .old unique (swapRunningExe).
func swapRunningExeBackup(dst, version string, write func(string) error) error {
	bak := BackupPath(dst, version)
	if bak == "" {
		return swapRunningExe(dst, write)
	}
	if err := os.Remove(bak); err != nil && !os.IsNotExist(err) {
		return swapRunningExe(dst, write) // sauvegarde de même nom verrouillée : pas de nouvelle, mise à jour faite
	}
	moved := false
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, bak); err != nil {
			return fmt.Errorf("%s : mise de côté impossible : %w", dst, err)
		}
		moved = true
	}
	if err := write(dst); err != nil {
		if moved {
			_ = os.Remove(dst) // fichier éventuellement à moitié posé
			_ = os.Rename(bak, dst)
		}
		return err
	}
	purgeKeepPath(dst, bak)
	return nil
}

// sameContent : a et b ont le même contenu.
func sameContent(a, b string) bool {
	x, e1 := os.ReadFile(a)
	y, e2 := os.ReadFile(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}
