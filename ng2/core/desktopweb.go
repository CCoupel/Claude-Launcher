package core

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
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
	// Enabled : commutateur (#183). Absent : v6.2.0 (allumée si listen est renseigné) ; false : éteinte, listen
	// conservé ; true : allumée, listen obligatoire.
	Enabled *bool `yaml:"enabled" json:"enabled,omitempty"`
	// URL : adresse publique derrière un proxy inverse (#171) : http(s)://hôte[:port], sans chemin
	// (PublicURL). Admise dans Host et Origin ; https → cookie Secure. Jamais d'en-tête X-Forwarded-* lu.
	URL string `yaml:"url" json:"url,omitempty"`
}

// DesktopWebConf : réglage effectif.
type DesktopWebConf struct {
	Off    bool
	Listen string // hôte:port d'écoute (vide si Off)
	// Enabled : valeur effective du commutateur (#183) ; Kept : adresse saisie gardée quand le commutateur est
	// éteint (SavedListen telle qu'écrite dans le fichier : « 8787 », « off », « hôte:port »…).
	Enabled bool
	Kept    string
	// ExplicitOff : desktop_web.enabled vaut explicitement false (≠ listen: off ou rien, libellé v6.2.0)
	ExplicitOff bool
	// URL : adresse publique posée dans les fichiers (brute, validée par ParsePublicURL par l'appelant : l'option
	// --url et la variable CL_WEB_URL la précèdent) ; renseignée même quand l'interface est éteinte
	URL       string
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
	var enabled *bool
	var pubURL string
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
		if d.Enabled != nil {
			enabled = d.Enabled
		}
		if d.URL != "" {
			pubURL = d.URL
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
	c := DesktopWebConf{Source: src, ListenSrc: src, URL: strings.TrimSpace(pubURL)}
	listen = strings.TrimSpace(listen)
	noListen := listen == "" || isOff(listen)
	// table enabled × listen (contracts/desktop-web.md) : absent → v6.2.0 ; false → éteinte, adresse gardée ;
	// true sans adresse → erreur de réglage
	switch {
	case enabled != nil && !*enabled:
		c.Off, c.Kept, c.ExplicitOff = true, listen, true
		if noListen {
			c.Kept = ""
		}
		return c, nil
	case enabled != nil && noListen:
		c.Enabled, c.Off = true, true
		return c, fmt.Errorf("%s : interface web activée sans adresse d'écoute", src)
	case noListen:
		c.Off = true
		return c, nil
	}
	c.Enabled = true
	if !strings.Contains(listen, ":") {
		listen = "127.0.0.1:" + listen // port seul : boucle locale
	}
	h, port, err := net.SplitHostPort(listen)
	if n, e := strconv.Atoi(port); err != nil || e != nil || n < 1 || n > 65535 {
		return DesktopWebConf{Off: true, Enabled: true, Source: src, ListenSrc: src, URL: c.URL}, fmt.Errorf("%s %q : off, un port ou hôte:port attendu", src, listen)
	}
	c.Listen, c.Loopback = listen, isLoopback(h)
	if bs, err := resolveBrowserServerFor(f, host, goos); err == nil && !bs.Off && sameListen(bs.Listen, listen) {
		return DesktopWebConf{Off: true, Enabled: true, Source: src, ListenSrc: src, URL: c.URL}, fmt.Errorf(
			"%s %q est aussi l'adresse de browser_server (%s) : deux serveurs distincts (fichiers en lecture seule sans jeton / interface complète avec jeton) — choisir un autre port", src, listen, bs.ListenSrc)
	}
	if !c.Loopback {
		if !allowLAN {
			return DesktopWebConf{Off: true, Enabled: true, Source: src, ListenSrc: src, URL: c.URL}, fmt.Errorf(
				"%s %q : écoute hors de la boucle locale refusée sans confirmation — l'interface complète (projets, terminaux) serait atteignable depuis le réseau, en HTTP clair, protégée par un jeton seulement ; ajouter allow_lan: true à desktop_web pour l'accepter (accès par le nom de la machine ; sur une écoute 0.0.0.0 ou ::, l'adresse IP est refusée par le contrôle de Host)", src, listen)
		}
		c.Warn = LANWarning(listen)
	}
	return c, nil
}

// LANWarning : avertissement d'une écoute hors de la boucle locale confirmée par allow_lan.
func LANWarning(listen string) string {
	return "desktop_web écoute sur " + listen + ", hors de la boucle locale : l'interface complète (projets, terminaux) est atteignable depuis le réseau, en HTTP clair, protégée par un jeton seulement — préférer 127.0.0.1 ou un tunnel ; accès par le nom de la machine (sur une écoute 0.0.0.0 ou ::, l'adresse IP est refusée par le contrôle de Host)"
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

// DesktopWebAllowsLAN : allow_lan est posé pour ce launcher (quel que soit listen).
func DesktopWebAllowsLAN() bool {
	f, host, goos := readBackendsFile(), hubName(), pathsGOOS
	allow := false
	merge := func(d *DesktopWeb) {
		if d != nil && d.AllowLAN != nil {
			allow = *d.AllowLAN
		}
	}
	merge(f.Defaults.DesktopWeb)
	v := pickHubOS(f.Hubs, host, goos)
	if v.ok {
		merge(v.base.DesktopWeb)
	}
	if v.ov != nil {
		merge(v.ov.DesktopWeb)
	}
	return allow
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
	case c.Off && c.ExplicitOff && c.Kept != "":
		fmt.Println("  désactivée (desktop_web.enabled: false), adresse gardée : " + c.Kept)
	case c.Off && c.ExplicitOff:
		fmt.Println("  désactivée (desktop_web.enabled: false)")
	case c.Off:
		fmt.Println("  désactivée (défaut) : desktop_web.listen pour l'activer, ex. 127.0.0.1:8770 (ou enabled: true avec listen)")
	default:
		fmt.Println("  écoute " + c.Listen + " (jeton obligatoire, conservé par poste)")
		if c.Warn != "" {
			fmt.Println("  ⚠ " + c.Warn)
		}
	}
}

// DesktopWebChanged : voir WebExposureChanged (nom conservé).
func DesktopWebChanged(oldContent, newContent []byte) bool {
	return WebExposureChanged(oldContent, newContent)
}

// WebExposureChanged : les réglages des deux rubriques web — desktop_web (interface complète), browser_server
// (fichiers en lecture seule), browser_paths et browser_device (chemins et appariement du serveur de maquettes) : défauts, machines, surcharges par système — de deux contenus
// de backends.yaml diffèrent. Un client web ne peut modifier ni l'un ni l'autre ; les autres réglages restent
// permis. Contenu illisible : l'empreinte vide diffère, donc refus (fail-closed).
func WebExposureChanged(oldContent, newContent []byte) bool {
	return desktopWebPrint(oldContent) != desktopWebPrint(newContent)
}

// desktopWebPrint : empreinte canonique des blocs desktop_web d'un backends.yaml.
func desktopWebPrint(content []byte) string {
	var f backendsFile
	if yaml.Unmarshal(content, &f) != nil {
		return ""
	}
	one := func(d *DesktopWeb, bs *BrowserServer, bp *[]BrowserPath, dev string) string {
		x := "-"
		if d != nil {
			b, _ := json.Marshal(d)
			x = string(b)
		}
		y := "-"
		if bs != nil {
			b, _ := json.Marshal(bs)
			y = string(b)
		}
		z, dv := "-", dev
		if bp != nil {
			b, _ := json.Marshal(*bp)
			z = string(b)
		}
		return x + "|" + y + "|" + z + "|" + dv
	}
	var names []string
	for k := range f.Hubs {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("defaults=" + one(f.Defaults.DesktopWeb, f.Defaults.BrowserServer, f.Defaults.BrowserPaths, ""))
	for _, k := range names {
		h := f.Hubs[k]
		sb.WriteString(";" + k + "=" + one(h.DesktopWeb, h.BrowserServer, h.BrowserPaths, h.BrowserDevice))
		for _, o := range []struct {
			n string
			b *hubOSConf
		}{{"windows", h.Windows}, {"linux", h.Linux}, {"darwin", h.Darwin}} {
			if o.b != nil {
				sb.WriteString("," + o.n + "=" + one(o.b.DesktopWeb, o.b.BrowserServer, o.b.BrowserPaths, o.b.BrowserDevice))
			} else {
				sb.WriteString("," + o.n + "=-")
			}
		}
	}
	return sb.String()
}

// ── Adresse publique (#171) ──────────────────────────────────────────────

// PublicURL : adresse publique d'une interface web derrière un proxy inverse, découpée.
type PublicURL struct {
	Origin string // schéma://hôte[:port] — l'origine exacte attendue pour cet hôte
	Scheme string // http ou https
	Host   string // nom d'hôte en minuscules, sans port ni crochets
	Port   string // port explicite de l'adresse, "" sinon
}

// HTTPS : le proxy termine le TLS — le cookie de session est alors Secure.
func (p PublicURL) HTTPS() bool { return p.Scheme == "https" }

// ParsePublicURL : « http(s)://hôte[:port] » — sans chemin, paramètres, fragment ni identifiants. Toute autre
// forme est une erreur de réglage (l'interface web reste éteinte, la raison est dite).
func ParsePublicURL(raw string) (PublicURL, error) {
	bad := func(why string) (PublicURL, error) {
		return PublicURL{}, fmt.Errorf("adresse publique %q invalide : %s (forme attendue : https://hôte[:port] ou http://hôte[:port], sans chemin ni paramètres)", raw, why)
	}
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return bad("illisible")
	case u.Scheme != "http" && u.Scheme != "https":
		return bad("schéma http ou https attendu")
	case u.Opaque != "" || u.Hostname() == "":
		return bad("hôte manquant")
	case u.User != nil:
		return bad("identifiants interdits")
	case u.Path != "" && u.Path != "/" || strings.Contains(raw, "?") || strings.Contains(raw, "#"):
		return bad("chemin, paramètres et fragment interdits")
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" {
		if n, e := strconv.Atoi(p); e != nil || n < 1 || n > 65535 {
			return bad("port invalide")
		}
	}
	origin := u.Scheme + "://" + strings.ToLower(u.Host)
	return PublicURL{Origin: origin, Scheme: u.Scheme, Host: host, Port: u.Port()}, nil
}

// MatchesHost : l'en-tête Host d'une requête désigne l'adresse publique — son nom, sur son port (ou sans port si
// l'adresse n'en porte pas).
func (p PublicURL) MatchesHost(reqHost string) bool {
	h, port, err := net.SplitHostPort(reqHost)
	if err != nil {
		h, port = strings.Trim(reqHost, "[]"), ""
	}
	return strings.EqualFold(strings.Trim(h, "[]"), p.Host) && port == p.Port
}
