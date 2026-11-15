package core

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Réglage desktop_web (#163) : interface web servie par la fenêtre native ──
//
// Même emplacement et même priorité que browser_server : hubs.<machine>.<système>.desktop_web
// > hubs.<machine>.desktop_web > defaults.desktop_web > défaut (off). Distinct de
// browser_server : celui-ci sert des fichiers en lecture seule, sans jeton ; desktop_web
// sert l'interface complète, avec jeton.

// DesktopWeb : defaults.desktop_web / hubs.<machine>[.<système>].desktop_web.
type DesktopWeb struct {
	Listen string `yaml:"listen" json:"listen"` // off, port seul (boucle locale) ou hôte:port
	// AllowLAN : confirmation explicite d'une écoute hors de la boucle locale (HTTP en clair,
	// jeton = contrôle du poste) ; sans elle, un listen non loopback est refusé
	AllowLAN *bool `yaml:"allow_lan" json:"allow_lan,omitempty"`
}

// DesktopWebConf : réglage effectif.
type DesktopWebConf struct {
	Off       bool
	Listen    string // hôte:port d'écoute (vide si Off)
	Loopback  bool   // l'hôte d'écoute est la boucle locale
	Source    string // bloc d'où vient le réglage
	ListenSrc string
	// Warn : avertissement à l'enregistrement et dans « backends » (écoute hors boucle locale)
	Warn string
}

// resolveDesktopWebFor : réglage effectif sur le système goos de la machine host.
// Erreur : valeur illisible, ou listen identique à celui de browser_server.
func resolveDesktopWebFor(f backendsFile, host, goos string) (DesktopWebConf, error) {
	listen, src := "", "défaut"
	var allowLAN bool
	merge := func(d *DesktopWeb, blk string) { // champ par champ : la machine l'emporte sur les défauts
		if d == nil {
			return
		}
		if d.Listen != "" {
			listen, src = d.Listen, blk+".listen"
		}
		if d.AllowLAN != nil {
			allowLAN = *d.AllowLAN
		}
	}
	merge(f.Defaults.DesktopWeb, "defaults.desktop_web")
	v := pickHubOS(f.Hubs, host, goos)
	if v.ok {
		merge(v.base.DesktopWeb, v.src("desktop_web", false))
	}
	if v.ov != nil {
		merge(v.ov.DesktopWeb, v.src("desktop_web", true))
	}
	c := DesktopWebConf{Source: src, ListenSrc: src}
	listen = strings.TrimSpace(listen)
	if listen == "" || isOff(listen) {
		c.Off = true
		return c, nil
	}
	if !strings.Contains(listen, ":") {
		listen = "127.0.0.1:" + listen // port seul : boucle locale
	}
	h, port, err := net.SplitHostPort(listen)
	if n, e := strconv.Atoi(port); err != nil || e != nil || n < 1 || n > 65535 {
		return DesktopWebConf{Off: true, Source: src, ListenSrc: src}, fmt.Errorf("%s %q : off, un port ou hôte:port attendu", src, listen)
	}
	c.Listen, c.Loopback = listen, isLoopback(h)
	if bs, err := resolveBrowserServerFor(f, host, goos); err == nil && !bs.Off && sameListen(bs.Listen, listen) {
		return DesktopWebConf{Off: true, Source: src, ListenSrc: src}, fmt.Errorf(
			"%s %q est aussi l'adresse de browser_server (%s) : deux serveurs distincts (fichiers en lecture seule sans jeton / interface complète avec jeton) — choisir un autre port", src, listen, bs.ListenSrc)
	}
	if !c.Loopback {
		if !allowLAN {
			return DesktopWebConf{Off: true, Source: src, ListenSrc: src}, fmt.Errorf(
				"%s %q : écoute hors de la boucle locale refusée sans confirmation — l'interface complète (projets, terminaux) serait atteignable depuis le réseau, en HTTP clair, protégée par un jeton seulement ; ajouter allow_lan: true à desktop_web pour l'accepter (l'accès se fait par le nom de la machine, pas par son IP)", src, listen)
		}
		c.Warn = LANWarning(listen)
	}
	return c, nil
}

// LANWarning : avertissement d'une écoute hors de la boucle locale confirmée par allow_lan.
func LANWarning(listen string) string {
	return "desktop_web écoute sur " + listen + ", hors de la boucle locale : l'interface complète (projets, terminaux) est atteignable depuis le réseau, en HTTP clair, protégée par un jeton seulement — préférer 127.0.0.1 ou un tunnel ; accès par le nom de la machine (l'IP est refusée)"
}

// sameListen : deux adresses d'écoute qui se disputent le même port (même hôte,
// ou l'une écoute sur toutes les interfaces).
func sameListen(a, b string) bool {
	ah, ap, e1 := net.SplitHostPort(a)
	bh, bp, e2 := net.SplitHostPort(b)
	if e1 != nil || e2 != nil || ap != bp {
		return false
	}
	wild := func(h string) bool { return h == "" || h == "0.0.0.0" || h == "::" }
	return wild(ah) || wild(bh) || strings.EqualFold(ah, bh) || isLoopback(ah) && isLoopback(bh)
}

// DesktopWebSetting : réglage effectif de ce launcher (machine et système courants).
func DesktopWebSetting() (DesktopWebConf, error) {
	return resolveDesktopWebFor(readBackendsFile(), hubName(), pathsGOOS)
}

// AllowedHosts : valeurs acceptées pour l'en-tête Host de l'interface web (anti
// DNS rebinding) — la boucle locale, l'hôte d'écoute, le nom de la machine ; mêmes
// règles que browser_server.
func (c DesktopWebConf) AllowedHosts(machine string) map[string]bool {
	if c.Off {
		return map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	}
	return serverConf{Listen: c.Listen}.allowedHosts(machine)
}

// PrintDesktopWeb : section de « claude-launcher-ng2 backends ».
func PrintDesktopWeb() {
	c, err := DesktopWebSetting()
	fmt.Printf("\nInterface web servie par la fenêtre native (%s) :\n", c.Source)
	switch {
	case err != nil:
		fmt.Println("  ⚠ " + err.Error() + " : désactivée")
	case c.Off:
		fmt.Println("  désactivée (défaut) : desktop_web.listen pour l'activer, ex. 127.0.0.1:8770")
	default:
		fmt.Println("  écoute " + c.Listen + " (jeton obligatoire, conservé par poste)")
		if c.Warn != "" {
			fmt.Println("  ⚠ " + c.Warn)
		}
	}
}

// DesktopWebChanged : les réglages desktop_web (défauts, machines, surcharges par système) de
// deux contenus de backends.yaml diffèrent. Un client web ne peut pas les modifier (décision
// de l'audit sécurité LOT-4) : les autres réglages restent permis.
func DesktopWebChanged(oldContent, newContent []byte) bool {
	return desktopWebPrint(oldContent) != desktopWebPrint(newContent)
}

// desktopWebPrint : empreinte canonique des blocs desktop_web d'un backends.yaml.
func desktopWebPrint(content []byte) string {
	var f backendsFile
	if yaml.Unmarshal(content, &f) != nil {
		return ""
	}
	one := func(d *DesktopWeb) string {
		if d == nil {
			return "-"
		}
		b, _ := json.Marshal(d)
		return string(b)
	}
	var names []string
	for k := range f.Hubs {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("defaults=" + one(f.Defaults.DesktopWeb))
	for _, k := range names {
		h := f.Hubs[k]
		sb.WriteString(";" + k + "=" + one(h.DesktopWeb))
		for _, o := range []struct {
			n string
			b *hubOSConf
		}{{"windows", h.Windows}, {"linux", h.Linux}, {"darwin", h.Darwin}} {
			if o.b != nil {
				sb.WriteString("," + o.n + "=" + one(o.b.DesktopWeb))
			} else {
				sb.WriteString("," + o.n + "=-")
			}
		}
	}
	return sb.String()
}
