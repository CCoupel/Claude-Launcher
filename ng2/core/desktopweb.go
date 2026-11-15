package core

import (
	"fmt"
	"net"
	"strconv"
	"strings"
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
	if d := f.Defaults.DesktopWeb; d != nil && d.Listen != "" {
		listen, src = d.Listen, "defaults.desktop_web.listen"
	}
	v := pickHubOS(f.Hubs, host, goos)
	if v.ok && v.base.DesktopWeb != nil && v.base.DesktopWeb.Listen != "" {
		listen, src = v.base.DesktopWeb.Listen, v.src("desktop_web", false)+".listen"
	}
	if v.ov != nil && v.ov.DesktopWeb != nil && v.ov.DesktopWeb.Listen != "" {
		listen, src = v.ov.DesktopWeb.Listen, v.src("desktop_web", true)+".listen"
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
		c.Warn = "desktop_web écoute sur " + listen + ", hors de la boucle locale : l'interface complète (projets, terminaux) est atteignable depuis le réseau, protégée par un jeton seulement — préférer 127.0.0.1"
	}
	return c, nil
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
