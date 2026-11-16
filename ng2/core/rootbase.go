package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── Racine de base (#165) ─────────────────────────────────────────────────
//
// CL_ROOT_BASE=<dossier absolu> (ou l'option --root-base) place la configuration, les projets et la config de
// claude du launcher sous une racine, au lieu du dossier personnel : un binaire posé n'importe où, y compris
// hors de la racine, retrouve le même arbre. Sans elle, comportement de la v6.2.0. Contrat :
// contracts/launcher-env.md.

// RootBaseVar : variable d'environnement de la racine de base (reportée dans les processus relancés).
const RootBaseVar = "CL_ROOT_BASE"

// rootBaseOpt : vrai quand la valeur vient de l'option --root-base de ce processus (la source des processus
// relancés est la variable, qui la porte).
var rootBaseOpt bool

// SetRootBaseOption : racine donnée par l'option --root-base ; elle l'emporte sur la variable et est reportée
// dans CL_ROOT_BASE pour toute relance.
func SetRootBaseOption(dir string) {
	rootBaseOpt = true
	_ = os.Setenv(RootBaseVar, dir)
}

// RootBase : racine de base effective et sa source (« --root-base » ou « CL_ROOT_BASE ») ; "" : aucune.
func RootBase() (dir, source string) {
	d := strings.TrimSpace(os.Getenv(RootBaseVar))
	if d == "" {
		return "", ""
	}
	if rootBaseOpt {
		return filepath.Clean(d), "--root-base"
	}
	return filepath.Clean(d), RootBaseVar
}

// ValidateRootBase : la racine de base, si elle est posée, est un dossier absolu existant et lisible. Message
// d'erreur : le chemin et sa source. Rien n'est créé.
func ValidateRootBase() error {
	dir, src := RootBase()
	if dir == "" {
		return nil
	}
	bad := func(why string) error {
		return fmt.Errorf("racine de base invalide (%s) : %q — %s", src, dir, why)
	}
	if !filepath.IsAbs(dir) {
		return bad("un chemin absolu est attendu")
	}
	st, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return bad("le dossier n'existe pas")
	case err != nil:
		return bad("illisible (" + err.Error() + ")")
	case !st.IsDir():
		return bad("ce n'est pas un dossier")
	}
	if _, err := os.ReadDir(dir); err != nil {
		return bad("illisible (" + err.Error() + ")")
	}
	return nil
}

// RootBaseConfigDir : <racine>/.config/claude-launcher-ng2 ("" sans racine).
func RootBaseConfigDir() string {
	if d, _ := RootBase(); d != "" {
		return filepath.Join(d, ".config", "claude-launcher-ng2")
	}
	return ""
}

// RootBaseProjects, RootBaseClaudeDir : racine de projets et config de claude proposées sous la racine ("" sans elle).
func RootBaseProjects() string {
	if d, _ := RootBase(); d != "" {
		return filepath.Join(d, "PROJETS")
	}
	return ""
}

func RootBaseClaudeDir() string {
	if d, _ := RootBase(); d != "" {
		return filepath.Join(d, ".claude")
	}
	return ""
}

// defaultConfigFor : config.conf proposé ; sous une racine de base, la racine de projets et CLAUDE_CONFIG_DIR sont
// ceux de la racine (jamais $HOME/GITHUB).
func defaultConfigFor() string {
	rb, _ := RootBase()
	if rb == "" {
		return defaultConfig
	}
	c := strings.Replace(defaultConfig, `GITHUB_DIR="$HOME/GITHUB"`, `GITHUB_DIR="`+RootBaseProjects()+`"`, 1)
	return strings.Replace(c, "# CLAUDE_CONFIG_DIR=\"/chemin/partage/.claude\"", `CLAUDE_CONFIG_DIR="`+RootBaseClaudeDir()+`"`, 1)
}

// PrintRootBase : première section de « claude-launcher-ng2 backends ».
func PrintRootBase() {
	d, src := RootBase()
	if d == "" {
		fmt.Println("Racine de base : aucune")
		fmt.Println()
		return
	}
	fmt.Printf("Racine de base : %s (%s)\n", d, src)
	fmt.Printf("  configuration   : %s\n", Ng2ConfigDir())
	cd := LoadConfig()
	claude := cd.ClaudeConfigDir
	if claude == "" {
		claude = RootBaseClaudeDir()
	}
	fmt.Printf("  config de claude: %s\n", claude)
	if len(cd.Roots) > 0 {
		fmt.Printf("  projets         : %s\n", strings.Join(cd.Roots, ", "))
	}
	fmt.Println()
}
