// Package testguard : aucun test n'écrit dans le vrai ~ du poste (#199, contracts/template-version.md « Tests »).
//
// Les TestMain des paquets qui touchent TplVersionPath() / TplCommitPath() / InitProjectPath() appellent Start :
// CL_SCRIPT (qui désigne le dossier de ces fichiers) pointe alors vers un dossier temporaire par défaut, et la
// garde échoue si le vrai ~/.template-version, ~/.template-commit ou ~/init-project.md a été touché pendant les tests.
package testguard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var watched = []string{".template-version", ".template-commit", "init-project.md"}

type snap struct {
	exists bool
	size   int64
	mtime  int64
	body   string
}

func take(dir string) map[string]snap {
	out := map[string]snap{}
	for _, n := range watched {
		p := filepath.Join(dir, n)
		st, err := os.Stat(p)
		if err != nil {
			out[n] = snap{}
			continue
		}
		b, _ := os.ReadFile(p)
		out[n] = snap{true, st.Size(), st.ModTime().UnixNano(), string(b)}
	}
	return out
}

// stateDirs : dossiers du vrai poste surveillés (coffre des secrets, configuration, journal) : jamais touchés par un test.
func stateDirs(home string) []string {
	return []string{filepath.Join(home, ".config", "claude-launcher"), filepath.Join(home, ".config", "claude-launcher-ng2")}
}

// tree : photographie (chemin, taille, date) de tout ce que contiennent les dossiers surveillés.
func tree(dirs []string) string {
	var out []string
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info, ierr := e.Info(); ierr == nil {
				out = append(out, fmt.Sprintf("%s|%d|%d", p, info.Size(), info.ModTime().UnixNano()))
			}
			return nil
		})
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// Start : isole les tests et rend la fonction à appeler après m.Run ; elle supprime le dossier temporaire et rend
// false (avec un message sur stderr) si le vrai ~ a changé.
func Start() (end func() bool) {
	home, _ := os.UserHomeDir()
	before := take(home)
	dirs := stateDirs(home)
	beforeTree := tree(dirs)
	// #201 : le coffre des secrets et la configuration se résolvent sous HOME / APPDATA : un dossier personnel temporaire
	// leur est donné, la racine de base, le coffre et l'ancien coffre sont retirés de l'environnement (un test qui en a
	// besoin les pose lui-même, vers un dossier temporaire).
	fakeHome, _ := os.MkdirTemp("", "ng2-test-home-")
	if fakeHome != "" {
		os.Setenv("HOME", fakeHome)
		os.Setenv("USERPROFILE", fakeHome)
		os.Setenv("APPDATA", filepath.Join(fakeHome, "AppData"))
		os.Setenv("LOCALAPPDATA", filepath.Join(fakeHome, "AppLocal"))
	}
	for _, v := range []string{"CL_ROOT_BASE", "CL_SECRETS_DIR", "CL_SECRETS_LEGACY_DIR"} {
		os.Unsetenv(v)
	}
	tmp := ""
	if os.Getenv("CL_SCRIPT") == "" {
		if d, err := os.MkdirTemp("", "ng2-test-script-"); err == nil {
			tmp = d
			os.Setenv("CL_SCRIPT", filepath.Join(d, "claude-launcher.sh"))
		}
	}
	return func() bool {
		ok := true
		after := take(home)
		for _, n := range watched {
			if before[n] != after[n] {
				fmt.Fprintf(os.Stderr, "GARDE : un test a modifié le vrai %s (CL_SCRIPT à régler vers un dossier temporaire)\n", filepath.Join(home, n))
				ok = false
			}
		}
		if t := tree(dirs); t != beforeTree {
			fmt.Fprintf(os.Stderr, "GARDE : un test a modifié le vrai %s (coffre, configuration ou journal du launcher)\n", dirs[0])
			ok = false
		}
		if fakeHome != "" {
			os.RemoveAll(fakeHome)
		}
		if tmp != "" {
			os.RemoveAll(tmp)
		}
		return ok
	}
}
