package core

import (
	"fmt"
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
}

var hubOSKeys = []string{"browser_server", "browser_paths", "browser_device", "paths", "remote_user"}

// UnmarshalYAML : clés connues seulement, comme les pools. L'erreur est une
// erreur de type yaml : le reste du fichier est lu (readBackendsFile), et
// LoadBackends la signale (diag backends).
func (o *hubOSConf) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return &yaml.TypeError{Errors: []string{fmt.Sprintf("ligne %d : surcharge par système : objet attendu ({ browser_server: { listen: … } })", n.Line)}}
	}
	var bad []string
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i].Value
		known := false
		for _, x := range hubOSKeys {
			known = known || k == x
		}
		if !known {
			bad = append(bad, fmt.Sprintf("ligne %d : clé %q inconnue dans une surcharge par système (%s)", n.Content[i].Line, k, strings.Join(hubOSKeys, ", ")))
		}
	}
	type raw hubOSConf
	if err := n.Decode((*raw)(o)); err != nil {
		return err
	}
	if len(bad) > 0 {
		return &yaml.TypeError{Errors: bad}
	}
	return nil
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
