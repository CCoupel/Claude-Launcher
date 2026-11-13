package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ── Remplacement du binaire en cours d'exécution ─────────────────────────
//
// Linux/macOS : un rename par-dessus le binaire suffit (le processus garde
// l'ancien inode). Windows : un .exe en cours d'exécution ne peut être ni
// écrasé ni remplacé par rename, mais il peut être RENOMMÉ. D'où : l'exe
// courant devient <exe>.old, le nouveau est écrit à sa place, le processus
// relance le nouveau puis se termine ; le .old est supprimé au démarrage
// suivant (CleanupOldExe). Logique ici, sans balise : testée sous Linux.

// OldExe : nom de l'ancien binaire mis de côté.
func OldExe(exe string) string { return exe + ".old" }

// swapRunningExe : remplace dst par write(dst) en mettant d'abord dst de côté
// (OldExe). Un .old encore verrouillé (ancien processus pas encore terminé)
// laisse la place à un nom unique. Échec de write : dst est restauré.
func swapRunningExe(dst string, write func(string) error) error {
	old := OldExe(dst)
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		old = fmt.Sprintf("%s.%d.old", dst, time.Now().UnixNano())
	}
	moved := false
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("%s : mise de côté impossible : %w", dst, err)
		}
		moved = true
	}
	if err := write(dst); err != nil {
		if moved {
			_ = os.Remove(dst) // fichier éventuellement à moitié posé
			_ = os.Rename(old, dst)
		}
		return err
	}
	return nil
}

// oldExes : binaires mis de côté par une mise à jour, à côté de exe.
func oldExes(exe string) []string {
	var out []string
	if _, err := os.Lstat(OldExe(exe)); err == nil {
		out = append(out, OldExe(exe))
	}
	m, _ := filepath.Glob(globEscape(exe) + ".*.old")
	out = append(out, m...)
	sort.Strings(out)
	return out
}

// removeOldExes : supprime les binaires mis de côté ; renvoie ceux qui restent
// (encore verrouillés).
func removeOldExes(exe string) []string {
	var left []string
	for _, f := range oldExes(exe) {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			left = append(left, f)
		}
	}
	return left
}

// globEscape : p protégé pour filepath.Glob (les métacaractères d'un chemin
// réel, ex. « [ », ne doivent pas être interprétés).
func globEscape(p string) string {
	var b []byte
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*', '?', '[':
			b = append(b, '[', c, ']')
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// relaunchArgs : programme et arguments du nouveau processus (Windows : pas
// d'execve, le nouveau binaire est lancé puis l'ancien se termine).
func relaunchArgs(argv0 string, argv []string) (string, []string) {
	if len(argv) == 0 {
		return argv0, nil
	}
	return argv0, append([]string(nil), argv[1:]...)
}
