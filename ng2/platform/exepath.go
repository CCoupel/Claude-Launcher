package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Chemin du binaire du launcher.
//
// os.Executable relit /proc/self/exe à chaque appel. Quand le binaire est sur
// un stockage partagé (MooseFS, NFS : mode portable) dont le montage tombe puis
// est refait, le processus en cours garde l'ancien montage, détaché : le noyau
// ne donne plus que le chemin interne au partage (« /CLAUDE_PROJETS/… » au lieu
// de « /share/MFS/ROOT/CLAUDE_PROJETS/… »). La mise à jour écrivait alors dans un
// dossier inexistant. Le chemin lu au démarrage sert de repli.

var startExe = currentExe()

func currentExe() string {
	e, err := os.Executable()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(e, " (deleted)")
}

func dirExists(d string) bool {
	st, err := os.Stat(d)
	return err == nil && st.IsDir()
}

// exeCandidates : chemin actuel, chemin au démarrage, puis argv[0] dans le PATH.
func exeCandidates(cur, start, arg0 string) []string {
	out := []string{cur, start}
	if arg0 != "" {
		if p, err := exec.LookPath(arg0); err == nil {
			if a, err := filepath.Abs(p); err == nil {
				out = append(out, a)
			}
		}
	}
	return out
}

// pickExe : premier candidat dont le dossier existe.
func pickExe(cands []string) (string, error) {
	for _, c := range cands {
		if c != "" && filepath.IsAbs(c) && dirExists(filepath.Dir(c)) {
			return c, nil
		}
	}
	return "", fmt.Errorf("chemin du binaire introuvable (%q : montage du stockage perdu ? relancer le launcher)", cands[0])
}

// SelfExe : chemin utilisable du binaire du launcher (voir plus haut).
func SelfExe() (string, error) {
	return pickExe(exeCandidates(currentExe(), startExe, os.Args[0]))
}
