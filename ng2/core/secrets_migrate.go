package core

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ── Migration du coffre v6.4.0 vers le coffre commun (v6.5.0, #201) ────────
//
// Au démarrage de chaque instance : pour chaque <backend>/ d'un ancien coffre (emplacement local de la v6.4.0, et
// CL_SECRETS_LEGACY_DIR posé par le chart sur l'ancien PVC) absent du coffre commun, copie des fichiers (0600) sous le
// verrou du coffre, journalisée (action « migrate »). L'ancien coffre est CONSERVÉ (secours, suppression manuelle). Un
// backend présent des deux côtés avec un contenu différent : le coffre commun est gardé, l'écart est journalisé et
// signalé une fois (SecretsMigrationConflicts). Tout l'ancien contenu est de portée globale : rien ne va dans PRIVATE/.

var secretFileNames = []string{secretKeyFile, secretPubFile, secretKnownFile, secretKubeFile}

var secretsMigration = struct {
	sync.Mutex
	conflicts map[string]bool
	logged    map[string]bool
}{conflicts: map[string]bool{}, logged: map[string]bool{}}

// SecretsMigrationConflicts : backends dont l'ancien coffre diffère du coffre commun (triés ; coffre commun conservé).
func SecretsMigrationConflicts() []string {
	secretsMigration.Lock()
	defer secretsMigration.Unlock()
	out := make([]string, 0, len(secretsMigration.conflicts))
	for b := range secretsMigration.conflicts {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// secretsLegacyDirs : anciens coffres à migrer (distincts du coffre commun, existants).
func secretsLegacyDirs(dest string) []string {
	var out []string
	seen := map[string]bool{realPath(dest): true}
	for _, d := range []string{secretsLocalDir(), os.Getenv(SecretsLegacyDirVar)} {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		r := realPath(d)
		if seen[r] {
			continue
		}
		seen[r] = true
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// MigrateLegacySecrets : copie les backends des anciens coffres absents du coffre commun. Rend les backends dont l'écart de
// contenu est signalé PAR CET APPEL (une seule fois par processus et par backend ; coffre commun conservé).
// Sans effet quand il n'y a aucun ancien coffre, ou quand le coffre est refusé (erreur rendue, rien n'est écrit).
func MigrateLegacySecrets() (conflicts []string, err error) {
	dest := secretsDirRaw()
	legacy := secretsLegacyDirs(dest)
	if len(legacy) == 0 {
		return nil, nil
	}
	root, err := SecretsDir()
	if err != nil {
		return nil, err
	}
	n := 0
	err = WithFileLock(filepath.Join(root, vaultLockName), func() error {
		for _, src := range legacy {
			ents, err := os.ReadDir(src)
			if err != nil {
				log.Printf("migration du coffre : %s illisible : %v", src, err)
				continue
			}
			for _, e := range ents {
				name := e.Name()
				if !e.IsDir() || checkBackendName(name) != nil {
					continue
				}
				ok, conflict, cerr := migrateBackend(root, filepath.Join(src, name), name)
				if cerr != nil {
					log.Printf("migration du coffre : backend %s : %v", name, cerr)
					continue
				}
				if ok {
					n++
				}
				if conflict {
					conflicts = append(conflicts, name)
				}
			}
		}
		return nil
	})
	if n > 0 {
		log.Printf("coffre des secrets : %d backend(s) migré(s) depuis l'ancien coffre (conservé)", n)
	}
	sort.Strings(conflicts)
	return conflicts, err
}

// migrateBackend : copie src → <root>/<name> si le backend est absent (ou vide) du coffre commun ; sinon compare.
// conflict : écart signalé par cet appel (première fois).
func migrateBackend(root, src, name string) (copied, conflict bool, err error) {
	dst := filepath.Join(root, name)
	if err := checkSecretsSubdir(root, dst); err != nil {
		return false, false, err
	}
	present := false
	for _, f := range secretFileNames {
		if _, e := os.Stat(filepath.Join(dst, f)); e == nil {
			present = true
		}
	}
	if present {
		diff := false
		for _, f := range secretFileNames {
			a, ea := readRegular(filepath.Join(src, f))
			b, eb := readRegular(filepath.Join(dst, f))
			if (ea == nil) != (eb == nil) && ea == nil || ea == nil && eb == nil && !bytes.Equal(a, b) {
				diff = true
			}
		}
		if diff {
			secretsMigration.Lock()
			first := !secretsMigration.logged[name]
			secretsMigration.logged[name] = true
			secretsMigration.conflicts[name] = true
			secretsMigration.Unlock()
			if first {
				conflict = true
				_ = journalSecret(name, "all", "migrate-conflict", "", "migration", "")
				log.Printf("migration du coffre : backend %s présent dans l'ancien coffre et dans le coffre commun avec un contenu différent : coffre commun conservé", name)
			}
		}
		return false, conflict, nil
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return false, false, err
	}
	_ = os.Chmod(dst, 0o700)
	if err := restrictSecret(dst, true); err != nil {
		return false, false, err
	}
	if err := journalSecret(name, "all", "migrate", "", "migration", ""); err != nil {
		return false, false, err
	}
	for _, f := range secretFileNames {
		b, e := readRegular(filepath.Join(src, f))
		if e != nil {
			continue
		}
		if err := writeSecretFile(filepath.Join(dst, f), b); err != nil {
			return false, false, err
		}
	}
	return true, false, nil
}

// readRegular : contenu d'un fichier ordinaire (jamais un lien : un lien dans un ancien coffre n'est pas suivi).
func readRegular(p string) ([]byte, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(p)
}
