package core

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ── Valeurs EFFECTIVES de « Ce poste » (#183) ─────────────────────────────
//
// browser_server et desktop_web se résolvent champ par champ : hubs.<machine>.<système> l'emporte sur
// hubs.<machine>, qui l'emporte sur defaults. Les Réglages affichent et modifient la valeur effective pour ce
// poste et ce système — jamais une valeur masquée : le modèle la porte avec sa provenance, et l'écriture
// vise la couche qui gagne.

// Provenances d'un champ : « defaults », « hub » (hubs.<machine>), ou le système de la surcharge.
const (
	SrcDefaults = "defaults"
	SrcHub      = "hub"
)

// HubResolved : valeurs effectives de browser_server et de desktop_web pour ce poste et ce système, avec la
// provenance de chaque champ (clé « browser_server.listen », « desktop_web.enabled »…). Un champ qui n'est
// posé nulle part a la provenance « ».
type HubResolved struct {
	BrowserServer HubServerView     `json:"browser_server"`
	DesktopWeb    HubDesktopWebView `json:"desktop_web"`
	// BrowserPaths : liste effective (la couche la plus prioritaire qui la pose la remplace entièrement) ;
	// BrowserDevice : deviceId effectif. Provenance : sources["browser_paths"], sources["browser_device"].
	BrowserPaths  []BrowserPath     `json:"browser_paths"`
	BrowserDevice string            `json:"browser_device"`
	Sources       map[string]string `json:"sources"`
}

type hubLayer struct {
	name string
	bs   *BrowserServer
	dw   *DesktopWeb
	bp   *[]BrowserPath
	dev  string
}

// hubLayers : couches d'un poste pour le système goos, de la moins prioritaire à la plus prioritaire.
func hubLayers(f backendsFile, host, goos string) []hubLayer {
	ls := []hubLayer{{name: SrcDefaults, bs: f.Defaults.BrowserServer, dw: f.Defaults.DesktopWeb, bp: f.Defaults.BrowserPaths}}
	if _, h, ok := pickHub(f.Hubs, host); ok {
		ls = append(ls, hubLayer{name: SrcHub, bs: h.BrowserServer, dw: h.DesktopWeb, bp: h.BrowserPaths, dev: h.BrowserDevice})
		if ov := h.osBlock(osKey(goos)); ov != nil {
			ls = append(ls, hubLayer{name: osKey(goos), bs: ov.BrowserServer, dw: ov.DesktopWeb, bp: ov.BrowserPaths, dev: ov.BrowserDevice})
		}
	}
	return ls
}

// resolvedOf : valeurs effectives champ par champ.
func resolvedOf(f backendsFile, host, goos string) HubResolved {
	r := HubResolved{BrowserServer: HubServerView{Allow: []string{}}, BrowserPaths: []BrowserPath{}, Sources: map[string]string{}}
	for _, k := range []string{"browser_paths", "browser_device", "browser_server.listen", "browser_server.url", "browser_server.allow",
		"desktop_web.listen", "desktop_web.allow_lan", "desktop_web.enabled", "desktop_web.url"} {
		r.Sources[k] = ""
	}
	for _, l := range hubLayers(f, host, goos) {
		if l.bp != nil {
			r.BrowserPaths, r.Sources["browser_paths"] = *l.bp, l.name
		}
		if l.dev != "" {
			r.BrowserDevice, r.Sources["browser_device"] = l.dev, l.name
		}
		if b := l.bs; b != nil {
			if b.Listen != "" {
				r.BrowserServer.Listen, r.Sources["browser_server.listen"] = b.Listen, l.name
			}
			if b.URL != "" {
				r.BrowserServer.URL, r.Sources["browser_server.url"] = b.URL, l.name
			}
			if b.Allow != nil {
				r.BrowserServer.Allow, r.Sources["browser_server.allow"] = b.Allow, l.name
			}
		}
		if d := l.dw; d != nil {
			if d.Listen != "" {
				r.DesktopWeb.Listen, r.Sources["desktop_web.listen"] = d.Listen, l.name
			}
			if d.AllowLAN != nil {
				r.DesktopWeb.AllowLAN, r.Sources["desktop_web.allow_lan"] = *d.AllowLAN, l.name
			}
			if d.Enabled != nil {
				r.DesktopWeb.Enabled, r.Sources["desktop_web.enabled"] = d.Enabled, l.name
			}
			if d.URL != "" {
				r.DesktopWeb.URL, r.Sources["desktop_web.url"] = d.URL, l.name
			}
		}
	}
	return r
}

// hubFields : champs de chaque réglage résolu champ par champ.
var hubFields = map[string][]string{
	"browser_server": {"listen", "url", "allow"},
	"desktop_web":    {"listen", "allow_lan", "enabled", "url"},
}

// ApplyHubChangesFor : ApplyHubChanges, avec l'écriture des réglages browser_server et desktop_web portée sur la
// couche qui GAGNE pour le système goos. Quand hubs.<machine>.<goos> définit un champ, la valeur saisie (qui est
// la valeur effective affichée) y est écrite, pas dans hubs.<machine> où elle serait masquée ; sans surcharge de
// ce champ, l'écriture va dans hubs.<machine> comme avant. Un changement qui vise explicitement une surcharge
// (at commençant par un système) est appliqué tel quel.
func ApplyHubChangesFor(content []byte, host, goos string, changes []ConfigChange) ([]byte, error) {
	routed, err := routeHubChanges(content, host, goos, changes)
	if err != nil {
		return nil, err
	}
	if len(routed) == 0 && len(changes) > 0 {
		return content, nil // rien à écrire (ex. retrait d'un champ qui ne vient que de defaults) : fichier intact
	}
	return ApplyHubChanges(content, host, routed)
}

func routeHubChanges(content []byte, host, goos string, changes []ConfigChange) ([]ConfigChange, error) {
	var f backendsFile
	if err := yaml.Unmarshal(content, &f); err != nil {
		return changes, nil // l'application des changements dira l'erreur du fichier
	}
	_, h, ok := pickHub(f.Hubs, host)
	os := osKey(goos)
	var ov *hubOSConf
	if ok && os != "" {
		ov = h.osBlock(os)
	}
	if ov == nil {
		ov = &hubOSConf{} // sans surcharge : même filtrage (un champ qu'aucune couche éditable ne pose n'est pas retiré)
	}
	// champs définis par la surcharge et par la couche commune
	has := func(key, field string, o bool) bool {
		var bs *BrowserServer
		var dw *DesktopWeb
		if o {
			bs, dw = ov.BrowserServer, ov.DesktopWeb
		} else {
			bs, dw = h.BrowserServer, h.DesktopWeb
		}
		switch key + "." + field {
		case "browser_server.listen":
			return bs != nil && bs.Listen != ""
		case "browser_server.url":
			return bs != nil && bs.URL != ""
		case "browser_server.allow":
			return bs != nil && bs.Allow != nil
		case "desktop_web.listen":
			return dw != nil && dw.Listen != ""
		case "desktop_web.allow_lan":
			return dw != nil && dw.AllowLAN != nil
		case "desktop_web.enabled":
			return dw != nil && dw.Enabled != nil
		case "desktop_web.url":
			return dw != nil && dw.URL != ""
		}
		return false
	}
	layerOf := func(key, field string) []string { // chemin relatif de la couche qui gagne
		if has(key, field, true) {
			return []string{os, key, field}
		}
		return []string{key, field}
	}
	var out []ConfigChange
	for _, c := range changes {
		if len(c.At) == 1 && (c.At[0] == "browser_paths" || c.At[0] == "browser_device") && (c.Op == "set" || c.Op == "delete") {
			key := c.At[0]
			inOV := key == "browser_paths" && ov.BrowserPaths != nil || key == "browser_device" && ov.BrowserDevice != ""
			inHub := key == "browser_paths" && h.BrowserPaths != nil || key == "browser_device" && h.BrowserDevice != ""
			switch {
			case c.Op == "set" && inOV: // la valeur effective vient de la surcharge : c'est elle qu'on écrit
				out = append(out, ConfigChange{Op: "set", At: []string{os, key}, Value: c.Value})
			case c.Op == "set":
				out = append(out, c)
			default: // retour au défaut : retiré de la surcharge et de la couche commune
				if inOV {
					out = append(out, ConfigChange{Op: "delete", At: []string{os, key}})
				}
				if inHub {
					out = append(out, c)
				}
			}
			continue
		}
		if len(c.At) == 0 || hubFields[c.At[0]] == nil {
			out = append(out, c)
			continue
		}
		key := c.At[0]
		switch {
		case len(c.At) == 2 && (c.Op == "set" || c.Op == "delete"):
			if c.Op == "delete" {
				// retiré de TOUTES les couches qui le posent (système, machine) : aucune valeur masquée ne
				// réapparaît. Un champ qui ne vient que de defaults n'est pas modifiable d'ici (contrat).
				for _, o := range []bool{true, false} {
					if has(key, c.At[1], o) {
						at := []string{key, c.At[1]}
						if o {
							at = []string{os, key, c.At[1]}
						}
						out = append(out, ConfigChange{Op: "delete", At: at})
					}
				}
				continue
			}
			out = append(out, ConfigChange{Op: c.Op, At: layerOf(key, c.At[1]), Value: c.Value})
		case len(c.At) == 1 && c.Op == "set":
			obj, isMap := c.Value.(map[string]any)
			if !isMap {
				out = append(out, c)
				continue
			}
			for _, field := range hubFields[key] {
				v, given := obj[field]
				switch {
				case given:
					out = append(out, ConfigChange{Op: "set", At: layerOf(key, field), Value: v})
				case has(key, field, true) || has(key, field, false):
					// remplacement de l'objet effectif : un champ absent est retiré de toutes les couches qui le posent
					for _, o := range []bool{true, false} {
						if has(key, field, o) {
							at := []string{key, field}
							if o {
								at = []string{os, key, field}
							}
							out = append(out, ConfigChange{Op: "delete", At: at})
						}
					}
				}
			}
		case len(c.At) == 1 && c.Op == "delete":
			// l'objet effectif retiré : les deux couches (le bloc d'un autre système n'est pas touché)
			if ov.BrowserServer != nil && key == "browser_server" || ov.DesktopWeb != nil && key == "desktop_web" {
				out = append(out, ConfigChange{Op: "delete", At: []string{os, key}})
			}
			if key == "browser_server" && h.BrowserServer != nil || key == "desktop_web" && h.DesktopWeb != nil {
				out = append(out, c)
			}
		default:
			return nil, fmt.Errorf("changement de « %s » : opération %q non prévue", key, c.Op)
		}
	}
	return out, nil
}
