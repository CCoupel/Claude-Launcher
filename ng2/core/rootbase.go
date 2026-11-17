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
// exprimés RELATIVEMENT à la racine par « ${CL_ROOT_BASE} » : le fichier est partagé entre le hub Linux, l'exe Windows et
// le pod, chacun résout la racine sous sa propre forme de chemin (jamais un chemin Windows écrit pour Linux ni
// l'inverse) — bash et confLite développent la variable de l'environnement du launcher. Jamais $HOME/GITHUB.
func defaultConfigFor() string {
	rb, _ := RootBase()
	if rb == "" {
		return defaultConfig
	}
	c := strings.Replace(defaultConfig, `GITHUB_DIR="$HOME/GITHUB"`, `GITHUB_DIR="${CL_ROOT_BASE}/PROJETS"`, 1)
	return strings.Replace(c, "# CLAUDE_CONFIG_DIR=\"/chemin/partage/.claude\"", `CLAUDE_CONFIG_DIR="${CL_ROOT_BASE}/.claude"`, 1)
}

// rootBaseUnresolved : valeur donnée à ${CL_ROOT_BASE} quand ni la variable ni l'emplacement du fichier ne donnent la
// racine : les chemins qui en dérivent sont écartés (jamais un « /PROJETS » silencieux) et signalés.
const rootBaseUnresolved = "/__CL_ROOT_BASE_NON_RESOLUE__"

// rootFromConfigPath : racine déduite de l'emplacement d'un config.conf « <racine>/.config/claude-launcher-ng2/config.conf »
// ("" si la forme ne correspond pas). Sert de repli quand CL_ROOT_BASE est absente de l'environnement (exe Windows, hub
// en mode portable) : le fichier créé sous la racine (« ${CL_ROOT_BASE}/PROJETS ») reste lisible sans exporter la variable.
func rootFromConfigPath(path string) string {
	d := filepath.Dir(path)
	if filepath.Base(path) != "config.conf" || filepath.Base(d) != "claude-launcher-ng2" || filepath.Base(filepath.Dir(d)) != ".config" {
		return ""
	}
	return filepath.Dir(filepath.Dir(d))
}

// rootBaseFallback : valeur de ${CL_ROOT_BASE} pour la lecture du fichier path de contenu src, quand la variable est
// absente de l'environnement ("" : rien à développer — variable posée, ou fichier qui ne l'utilise pas). unresolved
// vrai : le fichier l'utilise sans qu'on puisse la déduire.
func rootBaseFallback(path, src string) (root string, unresolved bool) {
	if rb, _ := RootBase(); rb != "" || !strings.Contains(src, "CL_ROOT_BASE") {
		return "", false
	}
	if r := rootFromConfigPath(path); r != "" {
		return r, false
	}
	return rootBaseUnresolved, true
}

// PrintRootBase : première section de « claude-launcher-ng2 backends ».
func PrintRootBase() {
	d, src := RootBase()
	if d == "" {
		fmt.Println("Racine de base : aucune")
		if w := LoadConfig().RootBaseWarn; w != "" {
			fmt.Println("  ⚠ " + w)
		}
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
	// cache : sous une racine de base, il reste sous le dossier personnel (état par launcher, jamais partagé) tant
	// que CL_CACHE_DIR n'en désigne pas un autre (convention du chart : <racine>/.cache/claude-launcher-ng2/<nom>)
	cacheSrc := "défaut"
	if CacheDirOverride() != "" {
		cacheSrc = CacheDirVar
	}
	fmt.Printf("  cache           : %s (%s)\n", CacheDir(), cacheSrc)
	fmt.Println()
}
