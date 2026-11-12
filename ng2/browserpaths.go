package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Correspondance de chemins session ↔ navigateur (#104) ─────────────────
//
// Le template ouvre dans Chrome (claude-in-chrome) des fichiers du projet en
// file:// ; Claude les voit au chemin de sa session (ex. /share/MFS/ROOT/…),
// Chrome, sur le poste de l'utilisateur, ailleurs (\\nas\MFS\ROOT\…, C:\…).
// Le launcher fournit la correspondance à chaque session dans
// CL_BROWSER_PATH_MAP (tableau JSON trié du préfixe le plus long au plus court).
//
// Portée : globale au launcher, surchargeable par poste (hubs.<machine>) — pas
// par backend : NG2 monte le stockage partagé au même chemin partout, seul le
// poste où tourne Chrome change le côté « host ». backends.yaml étant commun à
// tous les hubs (config portable), la surcharge se fait par nom de machine.

const browserPathVar = "CL_BROWSER_PATH_MAP"

type BrowserPath struct {
	Container string `yaml:"container" json:"container"`
	Host      string `yaml:"host" json:"host"`
	OS        string `yaml:"os" json:"os"`
}

// hubConf : réglages propres à une machine de launcher (hubs: de backends.yaml).
type hubConf struct {
	BrowserPaths *[]BrowserPath `yaml:"browser_paths"`
}

// winHostRe : C:\…, C: ou UNC \\serveur\… (dont \\wsl.localhost\…).
var winHostRe = regexp.MustCompile(`^([A-Za-z]:([\\/]|$)|\\\\)`)

// normBrowserPath : entrée validée et normalisée (séparateur final ôté, os
// déduit de la forme de host s'il manque) ; erreur = entrée ignorée.
func normBrowserPath(p BrowserPath) (BrowserPath, error) {
	if !strings.HasPrefix(p.Container, "/") {
		return p, fmt.Errorf("container %q : chemin absolu attendu", p.Container)
	}
	if p.Host == "" {
		return p, fmt.Errorf("container %s : host vide", p.Container)
	}
	win := winHostRe.MatchString(p.Host)
	if !win && !strings.HasPrefix(p.Host, "/") {
		return p, fmt.Errorf("host %q : forme attendue C:\\…, \\\\serveur\\… ou /…", p.Host)
	}
	switch p.OS {
	case "":
		p.OS = "linux"
		if win {
			p.OS = "windows"
		}
	case "windows", "linux":
	case "macos", "darwin":
		p.OS = "linux" // même forme de chemins
	default:
		return p, fmt.Errorf("container %s : os %q (windows ou linux)", p.Container, p.OS)
	}
	if c := strings.TrimRight(p.Container, "/"); c != "" {
		p.Container = c
	}
	if h := strings.TrimRight(p.Host, `/\`); h != "" {
		p.Host = h
	}
	return p, nil
}

// sortBrowserPaths : préfixe container le plus long d'abord (le template prend
// la première entrée qui couvre le fichier).
func sortBrowserPaths(ps []BrowserPath) {
	sort.SliceStable(ps, func(i, j int) bool {
		if len(ps[i].Container) != len(ps[j].Container) {
			return len(ps[i].Container) > len(ps[j].Container)
		}
		return ps[i].Container < ps[j].Container
	})
}

// hubName : nom de la machine du launcher, comparé aux clés de hubs:.
func hubName() string {
	h, _ := os.Hostname()
	return h
}

func pickHub(hubs map[string]hubConf, host string) (string, hubConf, bool) {
	short, _, _ := strings.Cut(host, ".")
	for k, v := range hubs {
		if strings.EqualFold(k, host) || strings.EqualFold(k, short) {
			return k, v, true
		}
	}
	return "", hubConf{}, false
}

// onWSL : launcher sous WSL (Chrome tourne alors côté Windows).
func onWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return bytes.Contains(bytes.ToLower(b), []byte("microsoft"))
}

// wslWinPath : wslpath -w (variable pour les tests).
var wslWinPath = func(p string) (string, error) {
	out, err := exec.Command("wslpath", "-w", p).Output()
	return strings.TrimSpace(string(out)), err
}

// defaultBrowserPaths : sans browser_paths, même chemin des deux côtés pour
// chaque racine connue (racines de projets, shared/volumes des backends) ;
// sous WSL, host = wslpath -w <racine>, os windows. Juste quand Chrome tourne
// sur la machine du launcher.
func defaultBrowserPaths(roots []string, wsl bool) []BrowserPath {
	seen := map[string]bool{}
	var out []BrowserPath
	for _, r := range roots {
		r = strings.TrimRight(r, "/")
		if r == "" || !strings.HasPrefix(r, "/") || seen[r] {
			continue
		}
		seen[r] = true
		p := BrowserPath{Container: r, Host: r, OS: "linux"}
		if wsl {
			if w, err := wslWinPath(r); err == nil && w != "" {
				p.Host, p.OS = strings.TrimRight(w, `\`), "windows"
			}
		}
		out = append(out, p)
	}
	sortBrowserPaths(out)
	return out
}

// resolveBrowserPaths : correspondance effective, d'où elle vient, entrées
// ignorées. hubs.<machine> l'emporte sur defaults ; une liste vide déclarée
// ([]) est gardée : aucun navigateur ne peut ouvrir ces fichiers.
func resolveBrowserPaths(f backendsFile, host string, roots []string, wsl bool) (ps []BrowserPath, source string, warns []string) {
	var decl *[]BrowserPath
	if k, h, ok := pickHub(f.Hubs, host); ok && h.BrowserPaths != nil {
		decl, source = h.BrowserPaths, "hubs."+k+".browser_paths"
	} else if f.Defaults.BrowserPaths != nil {
		decl, source = f.Defaults.BrowserPaths, "defaults.browser_paths"
	}
	if decl == nil {
		source = "défaut : même chemin des deux côtés"
		if wsl {
			source = "défaut WSL : wslpath -w"
		}
		return defaultBrowserPaths(roots, wsl), source, nil
	}
	ps = []BrowserPath{}
	for _, p := range *decl {
		n, err := normBrowserPath(p)
		if err != nil {
			warns = append(warns, source+" : "+err.Error()+" (entrée ignorée)")
			continue
		}
		ps = append(ps, n)
	}
	sortBrowserPaths(ps)
	return ps, source, warns
}

// knownRoots : racines de projets et racines partagées des backends.
func knownRoots() []string {
	roots := append([]string{}, loadConfig().Roots...)
	if bs, err := loadBackends(); err == nil {
		for _, b := range bs {
			roots = append(roots, b.Shared...)
		}
	}
	return roots
}

func readBackendsFile() backendsFile {
	var f backendsFile
	if b, err := os.ReadFile(backendsPath()); err == nil {
		_ = yaml.Unmarshal(b, &f)
	}
	return f
}

// browserPaths : correspondance effective de ce launcher.
func browserPaths() (ps []BrowserPath, source string, warns []string) {
	return resolveBrowserPaths(readBackendsFile(), hubName(), knownRoots(), onWSL())
}

// browserPathJSON : valeur de CL_BROWSER_PATH_MAP (JSON compact, jamais null).
func browserPathJSON(ps []BrowserPath) string {
	if ps == nil {
		ps = []BrowserPath{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ps)
	return strings.TrimSpace(b.String())
}

// browserPathEnv : « CL_BROWSER_PATH_MAP=… » pour la session.
func browserPathEnv() string {
	ps, _, _ := browserPaths()
	return browserPathVar + "=" + browserPathJSON(ps)
}

// exportBrowserPathsHub : projets sur l'hôte — environnement global du tmux du
// hub, hérité par les fenêtres créées ensuite (teamleader et teammates).
func exportBrowserPathsHub() {
	ps, _, _ := browserPaths()
	_ = dtmux("set-environment", "-g", browserPathVar, browserPathJSON(ps)).Run()
}

// printBrowserPaths : section de « claude-launcher-ng2 backends ».
func printBrowserPaths() {
	ps, source, warns := browserPaths()
	fmt.Printf("\nChemins vus par le navigateur (%s, machine %s) — %s :\n", browserPathVar, hubName(), source)
	if len(ps) == 0 {
		fmt.Println("  aucun : le template affichera seulement le chemin des fichiers")
	}
	for _, p := range ps {
		fmt.Printf("  %s → %s  (%s)\n", p.Container, p.Host, p.OS)
	}
	for _, w := range warns {
		fmt.Println("  ⚠ " + w)
	}
}
