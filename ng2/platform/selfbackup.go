package platform

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Sauvegarde du binaire avant mise à jour (#218) ───────────────────────
//
// Avant de remplacer l'exe, le binaire courant devient <exe>.bak-v<version> (convention de la sauvegarde du hub par
// deployer). Windows : l'exe en cours d'exécution est RENOMMÉ (swapRunningExe) ; Linux : copie atomique (le rename
// par-dessus reste le mécanisme de remplacement). UNE seule sauvegarde gardée : les autres .bak-v* sont supprimées
// après une installation réussie ; une sauvegarde verrouillée est retentée au démarrage (CleanupBackups).
// Aucun retour arrière dans l'interface : la sauvegarde est restaurée à la main.

var (
	warnMu        sync.Mutex
	backupWarning string
)

func setBackupWarning(w string) {
	warnMu.Lock()
	backupWarning = w
	warnMu.Unlock()
}

// TakeBackupWarning : avertissement de la dernière installation (« mise à jour faite sans sauvegarde : … »), vidé
// à la lecture ; "" si la sauvegarde a été faite. Montré dans le résultat de la mise à jour (#218).
func TakeBackupWarning() string {
	warnMu.Lock()
	defer warnMu.Unlock()
	w := backupWarning
	backupWarning = ""
	return w
}

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

// PurgeBackups : supprime les .bak-v* de exe sauf keepPath, le chemin de la sauvegarde à garder (une seule gardée) ;
// rend celles qui restent (verrouillées : retentées au démarrage). keepPath vide, ou qui n'est pas l'une des
// sauvegardes de exe : AUCUNE suppression (jamais de purge totale).
func PurgeBackups(exe, keepPath string) (left []string) {
	known := false
	for _, f := range backups(exe) {
		if f == keepPath {
			known = true
		}
	}
	if !known {
		return nil
	}
	for _, f := range backups(exe) {
		if f == keepPath {
			continue
		}
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			left = append(left, f)
		}
	}
	return left
}

// CleanupBackups : au démarrage, ne garde que la sauvegarde de la version la plus récente ; une sauvegarde
// verrouillée est retentée en tâche de fond pendant une dizaine de secondes.
func CleanupBackups() {
	exe, err := SelfExe()
	if err != nil || len(backups(exe)) < 2 {
		return
	}
	go cleanupBackupsFor(exe, 20, 500*time.Millisecond)
}

// cleanupBackupsFor : partie synchrone — garde la sauvegarde de la plus haute version, retente tries fois (pause
// entre deux) ; rend les sauvegardes verrouillées restantes. 0 ou 1 sauvegarde : intact.
func cleanupBackupsFor(exe string, tries int, pause time.Duration) []string {
	if len(backups(exe)) < 2 {
		return nil
	}
	var left []string
	for i := 0; i < tries || i == 0; i++ {
		if left = PurgeBackups(exe, newestBackup(exe)); len(left) == 0 {
			return nil
		}
		if i < tries-1 {
			time.Sleep(pause)
		}
	}
	return left
}

// newestBackup : sauvegarde de la version la plus récente (comparaison numérique par segments, pas par date).
func newestBackup(exe string) string {
	var best string
	for _, f := range backups(exe) {
		if best == "" || versionNewer(strings.TrimPrefix(f, exe+".bak-v"), strings.TrimPrefix(best, exe+".bak-v")) {
			best = f
		}
	}
	return best
}

// versionNewer : a est plus récente que b (segments séparés par . + -, comparés numériquement s'ils le sont, sinon
// comme texte ; un segment numérique l'emporte sur un texte).
func versionNewer(a, b string) bool {
	sa, sb := splitVer(a), splitVer(b)
	for i := 0; i < len(sa) || i < len(sb); i++ {
		var x, y string
		if i < len(sa) {
			x = sa[i]
		}
		if i < len(sb) {
			y = sb[i]
		}
		if x == y {
			continue
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			return nx > ny
		case ex == nil:
			return true
		case ey == nil:
			return false
		}
		return x > y
	}
	return false
}

func splitVer(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '+' || r == '-' })
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
	setBackupWarning("")
	bak := BackupPath(dst, version)
	if bak == "" {
		setBackupWarning("mise à jour faite sans sauvegarde de l'ancien binaire : version " + version + " non sauvegardable")
	}
	if bak != "" {
		if _, err := os.Lstat(dst); err == nil {
			if err := copyFileAtomic(dst, bak); err != nil {
				log.Printf("sauvegarde du binaire (%s) impossible : %v", filepath.Base(bak), err)
				setBackupWarning("mise à jour faite sans sauvegarde de l'ancien binaire : " + err.Error())
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
		PurgeBackups(dst, bak)
	}
	return nil
}

// swapRunningExeBackup : Windows — l'exe en cours d'exécution est renommé en sauvegarde (ou en .old si la version
// est inutilisable), write(dst) pose le nouveau (échec : dst restauré), puis purge des autres sauvegardes. Une
// sauvegarde de même nom déjà là (réinstallation) est remplacée ; verrouillée : nom .old unique (swapRunningExe).
func swapRunningExeBackup(dst, version string, write func(string) error) error {
	setBackupWarning("")
	bak := BackupPath(dst, version)
	if bak == "" {
		setBackupWarning("mise à jour faite sans sauvegarde de l'ancien binaire : version " + version + " non sauvegardable")
		return swapRunningExe(dst, write)
	}
	if err := os.Remove(bak); err != nil && !os.IsNotExist(err) {
		setBackupWarning("mise à jour faite sans nouvelle sauvegarde : " + filepath.Base(bak) + " est verrouillée")
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
	PurgeBackups(dst, bak)
	return nil
}

// sameContent : a et b ont le même contenu.
func sameContent(a, b string) bool {
	x, e1 := os.ReadFile(a)
	y, e2 := os.ReadFile(b)
	return e1 == nil && e2 == nil && string(x) == string(y)
}
