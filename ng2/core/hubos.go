package core

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Surcharge par système des réglages d'une machine (#110) ──────────────
//
// Une configuration portable est partagée par le hub Linux sous WSL et l'exe
// Windows du même poste, qui portent le même nom de machine (WSL reprend le
// hostname de Windows) et le même localhost. hubs.<machine>.windows / .linux /
// .darwin surchargent, clé par clé, les réglages de hubs.<machine> pour ce
// système (browser_server champ par champ) :
//
//	hubs:
//	  MSI:
//	    browser_server: { listen: 127.0.0.1:8080 }   # hub Linux (WSL)
//	    windows:
//	      browser_server: { listen: 127.0.0.1:8081 } # exe Windows

// hubOSConf : bloc hubs.<machine>.<système>.
type hubOSConf struct {
	BrowserPaths  *[]BrowserPath  `yaml:"browser_paths"`
	BrowserServer *BrowserServer  `yaml:"browser_server"`
	BrowserDevice string          `yaml:"browser_device"`
	Paths         []PathMap       `yaml:"paths"`
	RemoteUser    *RemoteUserConf `yaml:"remote_user"`
	warns         []string        // clés inconnues ou valeurs illisibles, ignorées
}

var (
	hubOSKeys = []string{"browser_server", "browser_paths", "browser_device", "paths", "remote_user"}
	osKeys    = []string{"windows", "linux", "darwin"}
	// clés de hubs.<machine> (browserpaths.go, hubConf)
	hubKeys = append(append([]string{}, hubOSKeys...), osKeys...)
	// noms courants d'un système → clé attendue
	osAliases = map[string]string{"win": "windows", "win32": "windows", "win64": "windows", "windows_nt": "windows",
		"mac": "darwin", "macos": "darwin", "osx": "darwin", "wsl": "linux", "ubuntu": "linux"}
)

// Une configuration partagée avec un hub de production ne doit jamais devenir
// illisible pour une faute de frappe : une clé inconnue de hubs.<machine> ou
// d'une surcharge par système est ignorée et signalée (hubConfigWarnings),
// jamais une erreur.

// UnmarshalYAML : bloc d'un système ; clé inconnue ou valeur illisible :
// avertissement, le reste du bloc s'applique.
func (o *hubOSConf) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		o.warns = append(o.warns, fmt.Sprintf("ligne %d : objet attendu ({ browser_server: { listen: … } }), bloc ignoré", n.Line))
		return nil
	}
	known := &yaml.Node{Kind: yaml.MappingNode, Tag: n.Tag, Line: n.Line, Column: n.Column}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		switch {
		case contains(hubOSKeys, k.Value):
			known.Content = append(known.Content, k, n.Content[i+1])
		case contains(osKeys, k.Value):
			o.warns = append(o.warns, fmt.Sprintf("ligne %d : surcharge %q imbriquée dans une surcharge par système, ignorée", k.Line, k.Value))
		default:
			o.warns = append(o.warns, unknownKeyWarn(k, hubOSKeys))
		}
	}
	type raw hubOSConf
	if err := known.Decode((*raw)(o)); err != nil {
		o.warns = append(o.warns, "valeur illisible, ignorée : "+strings.TrimPrefix(err.Error(), "yaml: unmarshal errors:\n  "))
	}
	return nil
}

// UnmarshalYAML : hubs.<machine> ; clés inconnues signalées (avant : ignorées
// en silence), comportement des clés connues inchangé.
func (h *hubConf) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; !contains(hubKeys, k.Value) {
				h.warns = append(h.warns, unknownKeyWarn(k, hubKeys))
			}
		}
	}
	type raw hubConf
	return n.Decode((*raw)(h))
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// unknownKeyWarn : « ligne N : clé "x" inconnue, ignorée », avec la clé
// connue la plus proche si elle ressemble (vouliez-vous dire …).
func unknownKeyWarn(k *yaml.Node, known []string) string {
	w := fmt.Sprintf("ligne %d : clé %q inconnue, ignorée", k.Line, k.Value)
	if s := suggestKey(k.Value, known); s != "" {
		w += " — vouliez-vous dire " + s + " ?"
	} else {
		w += " (clés : " + strings.Join(known, ", ") + ")"
	}
	return w
}

// suggestKey : clé connue la plus proche de k (distance d'édition faible, ou
// nom courant d'un système), "" : aucune.
func suggestKey(k string, known []string) string {
	lk := strings.ToLower(strings.TrimSpace(k))
	if a, ok := osAliases[lk]; ok && contains(known, a) {
		return a
	}
	best, bd := "", 0
	for _, c := range known {
		d := editDistance(lk, c)
		if best == "" || d < bd {
			best, bd = c, d
		}
	}
	// 1 écart pour une clé courte, 2 au-delà de 5 lettres, 3 au-delà de 10
	max := 1
	switch {
	case len(lk) > 10:
		max = 3
	case len(lk) > 5:
		max = 2
	}
	if best != "" && bd <= max {
		return best
	}
	return ""
}

// editDistance : distance de Levenshtein (octets).
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := prev[j-1]
			if a[i-1] != b[j-1] {
				c++
			}
			if prev[j]+1 < c {
				c = prev[j] + 1
			}
			if cur[j-1]+1 < c {
				c = cur[j-1] + 1
			}
			cur[j] = c
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// hubConfigWarnings : avertissements de hubs: (toutes les machines : le
// fichier est partagé), préfixés par la clé du bloc, triés.
func hubConfigWarnings(f backendsFile) []string {
	ks := make([]string, 0, len(f.Hubs))
	for k := range f.Hubs {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var out []string
	for _, k := range ks {
		h := f.Hubs[k]
		for _, w := range h.warns {
			out = append(out, "hubs."+k+" : "+w)
		}
		for _, o := range osKeys {
			if b := h.osBlock(o); b != nil {
				for _, w := range b.warns {
					out = append(out, "hubs."+k+"."+o+" : "+w)
				}
			}
		}
	}
	return out
}

// HubConfigWarnings : avertissements de hubs: dans backends.yaml (clés
// inconnues ignorées), pour backends et l'interface.
func HubConfigWarnings() []string { return hubConfigWarnings(readBackendsFile()) }

// PrintHubConfigWarnings : section de « claude-launcher-ng2 backends » (rien
// sans avertissement).
func PrintHubConfigWarnings() {
	ws := HubConfigWarnings()
	if len(ws) == 0 {
		return
	}
	fmt.Printf("\n⚠ Réglages par machine (hubs:) de %s — ignorés, le reste s'applique :\n", BackendsPath())
	for _, w := range ws {
		fmt.Println("  " + w)
	}
}

// osKey : clé de surcharge d'un système ("" : aucune).
func osKey(goos string) string {
	switch goos {
	case "windows", "linux", "darwin":
		return goos
	}
	return ""
}

// osBlock : surcharge de h pour goos (nil : aucune).
func (h hubConf) osBlock(goos string) *hubOSConf {
	switch goos {
	case "windows":
		return h.Windows
	case "linux":
		return h.Linux
	case "darwin":
		return h.Darwin
	}
	return nil
}

// hubView : hubs.<machine> vu de goos (logique pure).
type hubView struct {
	key  string     // clé de hubs: retenue
	goos string     // système de la surcharge
	base hubConf    // hubs.<machine>
	ov   *hubOSConf // hubs.<machine>.<goos> (nil : aucune)
	ok   bool
}

// pickHubOS : réglages de la machine host pour le système goos ("" : sans
// surcharge, comme avant).
func pickHubOS(hubs map[string]hubConf, host, goos string) hubView {
	k, h, ok := pickHub(hubs, host)
	v := hubView{key: k, goos: osKey(goos), base: h, ok: ok}
	if ok {
		v.ov = h.osBlock(v.goos)
	}
	return v
}

// src : nom de la clé d'un réglage, surcharge comprise.
func (v hubView) src(field string, fromOS bool) string {
	if fromOS {
		return "hubs." + v.key + "." + v.goos + "." + field
	}
	return "hubs." + v.key + "." + field
}

// browserPaths : browser_paths de la machine et sa source (nil : aucun).
func (v hubView) browserPaths() (*[]BrowserPath, string) {
	if v.ov != nil && v.ov.BrowserPaths != nil {
		return v.ov.BrowserPaths, v.src("browser_paths", true)
	}
	if v.ok && v.base.BrowserPaths != nil {
		return v.base.BrowserPaths, v.src("browser_paths", false)
	}
	return nil, ""
}

// browserDevice : browser_device de la machine et sa source.
func (v hubView) browserDevice() (string, string) {
	if v.ov != nil && v.ov.BrowserDevice != "" {
		return v.ov.BrowserDevice, v.src("browser_device", true)
	}
	if v.ok && v.base.BrowserDevice != "" {
		return v.base.BrowserDevice, v.src("browser_device", false)
	}
	return "", ""
}

// paths : entrées de paths de la machine et leur source (la surcharge les
// remplace toutes).
func (v hubView) paths() ([]PathMap, string) {
	if v.ov != nil && v.ov.Paths != nil {
		return v.ov.Paths, v.src("paths", true)
	}
	return v.base.Paths, v.src("paths", false)
}

// remoteUser : remote_user de la machine et sa source.
func (v hubView) remoteUser() (*RemoteUserConf, string) {
	if v.ov != nil && v.ov.RemoteUser != nil {
		return v.ov.RemoteUser, v.src("remote_user", true)
	}
	if v.base.RemoteUser != nil {
		return v.base.RemoteUser, v.src("remote_user", false)
	}
	return nil, ""
}
