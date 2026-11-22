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

// FakeTokenMarker : tout jeton factice d'un test contient cette chaîne (« ghp_FAKE_TEST_… » : invalide par construction,
// reconnaissable). La garde échoue si elle apparaît dans un settings*.json RÉEL après la suite : seule une fuite
// d'isolation (CLAUDE_CONFIG_DIR du poste visé par un test) peut l'y écrire (#216, enquête GH_TOKEN).
const FakeTokenMarker = "FAKE_TEST"

// realClaudeDirs : dossiers de config claude du poste, relevés à l'import — AVANT que le moindre TestMain ne change
// l'environnement : CLAUDE_CONFIG_DIR de la session et ~/.claude.
var realClaudeDirs, realMarkersBefore = func() ([]string, map[string]bool) {
	var dirs []string
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".claude"))
	}
	return dirs, markerFiles(dirs)
}()

// markerFiles : fichiers settings*.json (settings.json, claude-launcher/settings-env*.json) qui portent le marqueur.
func markerFiles(dirs []string) map[string]bool {
	out := map[string]bool{}
	for _, d := range dirs {
		for _, pat := range []string{"settings*.json", filepath.Join("claude-launcher", "settings-env*.json")} {
			m, _ := filepath.Glob(filepath.Join(d, pat))
			for _, f := range m {
				if b, err := os.ReadFile(f); err == nil && strings.Contains(string(b), FakeTokenMarker) {
					out[f] = true
				}
			}
		}
	}
	return out
}

// newLeaks : fichiers qui portent le marqueur maintenant sans l'avoir porté avant.
func newLeaks(dirs []string, before map[string]bool) []string {
	var out []string
	for f := range markerFiles(dirs) {
		if !before[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

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
		if leaks := newLeaks(realClaudeDirs, realMarkersBefore); len(leaks) > 0 {
			fmt.Fprintf(os.Stderr, "GARDE : un jeton factice de test (%s) est dans la config claude RÉELLE : %s\n  → un test écrit dans CLAUDE_CONFIG_DIR du poste (isolation à corriger)\n", FakeTokenMarker, strings.Join(leaks, ", "))
			ok = false
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
