package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
	BrowserPaths  *[]BrowserPath  `yaml:"browser_paths"`
	BrowserServer *BrowserServer  `yaml:"browser_server"`
	BrowserDevice string          `yaml:"browser_device"` // deviceId imposé (#106, browserpair.go)
	Paths         []PathMap       `yaml:"paths"`          // chemins locaux → chemins des backends (#110, pathmap.go)
	RemoteUser    *RemoteUserConf `yaml:"remote_user"`    // uid/gid/HOME dans les conteneurs (#110, Windows)
	// surcharges par système, clé par clé (#110, hubos.go) : hub Linux sous WSL
	// et exe Windows du même poste portent le même nom de machine
	Windows *hubOSConf `yaml:"windows"`
	Linux   *hubOSConf `yaml:"linux"`
	Darwin  *hubOSConf `yaml:"darwin"`
	warns   []string   // clés inconnues, ignorées (hubos.go)
	// cache d'état du hub Linux vu de cette machine (#125, lecture seule) :
	// les badges et cartes d'un projet en sont repris s'ils sont plus frais
	HubCache string `yaml:"hub_cache"`
}

// HubCacheDir : hubs.<machine>[.<système>].hub_cache de backends.yaml ("" :
// aucun). C'est un chemin vu de ce système (Windows : \\wsl.localhost\…) :
// sa place naturelle est hubs.<machine>.windows (#127).
func HubCacheDir() string { return hubCacheOf(readBackendsFile().Hubs, hubName(), runtime.GOOS) }

// hubCacheOf : hub_cache de la machine host vu de goos, surcharge par système
// d'abord (logique pure).
func hubCacheOf(hubs map[string]hubConf, host, goos string) string {
	v := pickHubOS(hubs, host, goos)
	if !v.ok {
		return ""
	}
	if v.ov != nil && strings.TrimSpace(v.ov.HubCache) != "" {
		return strings.TrimSpace(v.ov.HubCache)
	}
	return strings.TrimSpace(v.base.HubCache)
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
	if strings.HasPrefix(p.Host, "http://") || strings.HasPrefix(p.Host, "https://") || p.OS == "url" {
		if !strings.HasPrefix(p.Host, "http://") && !strings.HasPrefix(p.Host, "https://") {
			return p, fmt.Errorf("container %s : os url, host http(s)://… attendu", p.Container)
		}
		p.OS = "url" // base d'URL (#106) : host + chemin relatif encodé
		if c := strings.TrimRight(p.Container, "/"); c != "" {
			p.Container = c
		}
		p.Host = strings.TrimRight(p.Host, "/")
		return p, nil
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
		return p, fmt.Errorf("container %s : os %q (windows, linux ou url)", p.Container, p.OS)
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
	if runtime.GOOS == "windows" {
		return false // Windows natif (#110) : ni /proc ni WSL_DISTRO_NAME ; Chrome est local
	}
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

// mappedBrowserPaths : pour chaque racine connue (locale ou vue des sessions),
// chemin des sessions → chemin local selon la correspondance m.
func mappedBrowserPaths(roots []string, m PathMapper) []BrowserPath {
	seen := map[string]bool{}
	var out []BrowserPath
	for _, r := range roots {
		c, _ := m.ToRemote(r)
		c = strings.TrimRight(c, "/")
		if c == "" || !strings.HasPrefix(c, "/") || seen[c] {
			continue
		}
		seen[c] = true
		h, _ := m.ToLocal(c)
		p := BrowserPath{Container: c, Host: h, OS: "linux"}
		if winPathForm(h) {
			p.Host, p.OS = strings.TrimRight(h, `\`), "windows"
		}
		out = append(out, p)
	}
	sortBrowserPaths(out)
	return out
}

// resolveBrowserPaths : correspondance effective, d'où elle vient, entrées
// ignorées. hubs.<machine> l'emporte sur defaults ; une liste vide déclarée
// ([]) est gardée : aucun navigateur ne peut ouvrir ces fichiers. Sans
// browser_paths : le serveur web du launcher (#106), sauf browser_server off.
func resolveBrowserPaths(f backendsFile, host string, roots []string, wsl bool) (ps []BrowserPath, source string, warns []string) {
	return resolveBrowserPathsFor(f, host, roots, wsl, "")
}

// resolveBrowserPathsFor : resolveBrowserPaths sur le système goos, surcharge
// hubs.<machine>.<goos> comprise ("" : sans surcharge).
func resolveBrowserPathsFor(f backendsFile, host string, roots []string, wsl bool, goos string) (ps []BrowserPath, source string, warns []string) {
	decl, source := pickHubOS(f.Hubs, host, goos).browserPaths()
	if decl == nil && f.Defaults.BrowserPaths != nil {
		decl, source = f.Defaults.BrowserPaths, "defaults.browser_paths"
	}
	if decl == nil {
		// serveur web du launcher (#106) : le chemin de l'URL est le chemin interne
		if c, err := resolveBrowserServerFor(f, host, goos); err == nil && !c.Off {
			return []BrowserPath{{Container: "/", Host: c.Base, OS: "url"}}, "serveur web du launcher (" + c.Listen + ")", nil
		}
		// toutes les entrées de paths, quelle que soit leur forme (comme avant)
		if hs := resolveHubSettingsOS(f, host, goos, ""); !hs.paths.Empty() {
			// poste à correspondance déclarée (Windows, #110) : Chrome y voit les
			// fichiers à leur chemin local
			return mappedBrowserPaths(roots, hs.paths), "défaut : correspondance " + hs.src, nil
		}
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
	roots := append([]string{}, LoadConfig().Roots...)
	if bs, err := LoadBackends(); err == nil {
		for _, b := range bs {
			roots = append(roots, b.Shared...)
		}
	}
	return roots
}

func readBackendsFile() backendsFile {
	var f backendsFile
	if b, err := os.ReadFile(BackendsPath()); err == nil {
		_ = yaml.Unmarshal(b, &f)
	}
	return f
}

// browserPaths : correspondance effective de ce launcher.
func browserPaths() (ps []BrowserPath, source string, warns []string) {
	return resolveBrowserPathsFor(readBackendsFile(), hubName(), knownRoots(), onWSL(), pathsGOOS)
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

// PrintBrowserPaths : section de « claude-launcher-ng2 backends ».
func PrintBrowserPaths() {
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
