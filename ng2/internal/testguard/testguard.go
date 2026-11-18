// Package testguard : aucun test n'écrit dans le vrai ~ du poste (#199, contracts/template-version.md « Tests »).
//
// Les TestMain des paquets qui touchent TplVersionPath() / TplCommitPath() / InitProjectPath() appellent Start :
// CL_SCRIPT (qui désigne le dossier de ces fichiers) pointe alors vers un dossier temporaire par défaut, et la
// garde échoue si le vrai ~/.template-version, ~/.template-commit ou ~/init-project.md a été touché pendant les tests.
package testguard

import (
	"fmt"
	"os"
	"path/filepath"
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

// Start : isole les tests et rend la fonction à appeler après m.Run ; elle supprime le dossier temporaire et rend
// false (avec un message sur stderr) si le vrai ~ a changé.
func Start() (end func() bool) {
	home, _ := os.UserHomeDir()
	before := take(home)
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
		if tmp != "" {
			os.RemoveAll(tmp)
		}
		return ok
	}
}
