package core

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/internal/tun"
)

// Bornage des hôtes d'un backend d'UTILISATEUR (S1–S9, S13, S14, D1 ; contracts/user-backends.md §3). Décision : un utilisateur
// n'atteint que l'Internet public ; un admin n'a AUCUNE contrainte (aucune de ces fonctions ne lui est appliquée). Le message de
// refus est unique et ne dit rien de la raison (S8 : pas d'oracle de topologie).

// HostRefusal : message unique de tout refus d'hôte.
const HostRefusal = "adresse non autorisée pour un utilisateur"

var errHostRefused = errors.New(HostRefusal)

// userHostResolver : résolveur du launcher (défaut : net.DefaultResolver) ; userHostCacheTTL : durée du cache de résolution (D1).
var (
	userHostResolver = tun.Of(func(host string) ([]net.IP, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	})
	userHostCacheTTL = tun.Of(30 * time.Second)
)

// plages refusées à un utilisateur (netip.Prefix, jamais une regex sur la chaîne)
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
		"192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16",
		"fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// ipAllowed : l'adresse est publique. IPv4-mappée déballée avant le contrôle ; NAT64, 6to4, Teredo, IPv4-compatible refusés en bloc.
func ipAllowed(a netip.Addr) bool {
	a = a.Unmap().WithZone("")
	if !a.IsValid() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() && !a.IsMulticast() && !a.IsUnspecified()
}

// internalSuffixes : noms réservés à un réseau interne (le filtre d'IP reste la barrière réelle).
var internalSuffixes = []string{".local", ".lan", ".internal", ".home.arpa", ".svc", ".cluster.local", ".localdomain", ".localhost", ".home",
	".corp", ".intranet", ".private", ".invalid", ".arpa"}

// splitHostPort : « hôte:port » strict ; port décimal 1–65535 obligatoire.
func splitHostPort(hp string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(hp)
	if err != nil || h == "" || p == "" {
		return "", 0, errHostRefused
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return "", 0, errHostRefused
		}
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, errHostRefused
	}
	return h, n, nil
}

// parseUserEndpoint : « scheme://hôte:port » → (hôte canonique, port). Refus de toute forme non canonique : userinfo, chemin, requête,
// fragment, zone IPv6, caractères de contrôle, espaces, numérique non « a.b.c.d », nom interne, IP interdite. Aucune résolution.
func parseUserEndpoint(raw, scheme string) (host string, port int, err error) {
	if len(raw) > 300 {
		return "", 0, errHostRefused
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`\"'`+"`"+`$;|&<>{}()@#?%`, r) {
			return "", 0, errHostRefused
		}
	}
	pre := scheme + "://"
	if len(raw) <= len(pre) || !strings.EqualFold(raw[:len(pre)], pre) {
		return "", 0, errHostRefused
	}
	rest := raw[len(pre):]
	if strings.ContainsAny(rest, "/") {
		return "", 0, errHostRefused
	}
	h, port, err := splitHostPort(rest)
	if err != nil {
		return "", 0, err
	}
	if strings.HasPrefix(rest, "[") { // IPv6 entre crochets
		a, perr := netip.ParseAddr(h)
		if perr != nil || a.Zone() != "" || !a.Is6() && !a.Is4In6() || !ipAllowed(a) {
			return "", 0, errHostRefused
		}
		return a.String(), port, nil
	}
	if a, perr := netip.ParseAddr(h); perr == nil { // IP littérale (forme canonique décimale pointée, ou IPv6 sans crochets : refusée par SplitHostPort)
		if a.Zone() != "" || !ipAllowed(a) {
			return "", 0, errHostRefused
		}
		return a.String(), port, nil
	}
	n := strings.ToLower(strings.TrimSuffix(h, "."))
	if len(n) == 0 || len(n) > 253 || !strings.Contains(n, ".") {
		return "", 0, errHostRefused // sans point : nom interne (localhost, intranet, kubernetes…)
	}
	labels := strings.Split(n, ".")
	last := labels[len(labels)-1]
	if last == "" || isNumericLabel(last) {
		return "", 0, errHostRefused // 127.1, 2130706433, 0x7f.0.0.1, 0177.0.0.1 : forme numérique non canonique
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", 0, errHostRefused
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", 0, errHostRefused
			}
		}
	}
	if labels[0] == "kubernetes" || labels[0] == "localhost" || n == "localhost" {
		return "", 0, errHostRefused
	}
	for _, s := range internalSuffixes {
		if strings.HasSuffix("."+n, s) {
			return "", 0, errHostRefused
		}
	}
	for _, l := range labels {
		if l == "svc" {
			return "", 0, errHostRefused
		}
	}
	return n, port, nil
}

// isNumericLabel : « 0x7f », « 0177 », « 2130706433 »… (étiquette qu'un résolveur C lirait comme un nombre).
func isNumericLabel(l string) bool {
	if strings.HasPrefix(l, "0x") {
		return true
	}
	for _, r := range l {
		if r < '0' || r > '9' {
			return false
		}
	}
	return l != ""
}

type resolvedHost struct {
	until time.Time
	err   error
}

var (
	resolveMu    sync.Mutex
	resolveCache = map[string]resolvedHost{}
)

// checkResolved : toutes les IP rendues pour name sont publiques (une seule interdite : refus). Cache userHostCacheTTL. Une IP
// littérale n'est pas résolue.
func checkResolved(name string) error {
	if _, err := netip.ParseAddr(name); err == nil {
		return nil // déjà contrôlée par parseUserEndpoint
	}
	now := time.Now()
	resolveMu.Lock()
	if c, ok := resolveCache[name]; ok && now.Before(c.until) {
		resolveMu.Unlock()
		return c.err
	}
	resolveMu.Unlock()
	ips, err := userHostResolver.Get()(name)
	// une résolution qui échoue (aucune réponse) n'ouvre aucune adresse : l'outil échouera seul ; seule une réponse contenant une IP
	// interdite est refusée. Le nom est revérifié avant chaque appel (cache de userHostCacheTTL), donc un nom qui se met à résoudre
	// vers une IP interne est refusé dès l'appel suivant.
	var res error
	if err == nil {
		for _, ip := range ips {
			a, ok := netip.AddrFromSlice(ip)
			if !ok || !ipAllowed(a) {
				res = errHostRefused
				break
			}
		}
	}
	resolveMu.Lock()
	if len(resolveCache) > 512 {
		resolveCache = map[string]resolvedHost{}
	}
	resolveCache[name] = resolvedHost{until: now.Add(userHostCacheTTL.Get()), err: res}
	resolveMu.Unlock()
	return res
}

// CheckUserHost : « tcp://hôte:port » d'un backend docker/podman d'utilisateur — forme, puis résolution (D1). Erreur : HostRefusal.
func CheckUserHost(raw string) error {
	h, _, err := parseUserEndpoint(raw, "tcp")
	if err != nil {
		return err
	}
	return checkResolved(h)
}

// CheckUserServer : « https://hôte:port » (serveur d'un kubeconfig) — mêmes règles, https obligatoire.
func CheckUserServer(raw string) error {
	h, _, err := parseUserEndpoint(raw, "https")
	if err != nil {
		return err
	}
	return checkResolved(h)
}

// userHostGate : revérification (cache D1) avant chaque appel d'un backend d'utilisateur docker/podman.
func (b BackendConf) userHostGate() error {
	if !b.user || (b.Type != "docker" && b.Type != "podman") {
		return nil
	}
	return CheckUserHost(b.Host)
}

// userEnvUnset : variables retirées de l'environnement de tout appel d'un backend d'utilisateur (S10, S32) : proxys, démon, contexte,
// certificats et kubeconfig hérités du launcher ne s'appliquent jamais à lui.
var userEnvUnset = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
	"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY", "DOCKER_TLS", "DOCKER_API_VERSION",
	"DOCKER_DEFAULT_PLATFORM", "CONTAINER_HOST", "CONTAINER_CONNECTION", "CONTAINER_SSHKEY", "CONTAINER_PASSPHRASE", "KUBECONFIG",
	"KUBECTL_PROXY", "KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT"}

// envUnset : « env -u VAR … » (argv, jamais un shell).
func envUnset() []string {
	out := []string{"env"}
	for _, v := range userEnvUnset {
		out = append(out, "-u", v)
	}
	return out
}

// userCli : ligne de commande docker / podman d'un backend d'UTILISATEUR — hôte revérifié (D1), environnement nettoyé, TLS par les
// fichiers de SON coffre (jamais DOCKER_CERT_PATH), arguments en liste (jamais un shell). Refus : commande qui ne se lance pas.
func (e Engine) userCli(args []string) []string {
	if err := e.B.userHostGate(); err != nil {
		return kubectlRefused(err)
	}
	out := envUnset()
	switch e.B.Type {
	case "docker":
		out = append(out, "docker", "-H", e.B.Host)
		if t := e.B.TLS; t != nil && t.Enabled {
			d := filepath.Join(e.B.vault, e.B.Name)
			if t.verifies() {
				out = append(out, "--tlsverify", "--tlscacert="+filepath.Join(d, "ca.pem"))
			} else {
				out = append(out, "--tls")
			}
			if regularFile(filepath.Join(d, "cert.pem")) && regularFile(filepath.Join(d, "key.pem")) {
				out = append(out, "--tlscert="+filepath.Join(d, "cert.pem"), "--tlskey="+filepath.Join(d, "key.pem"))
			}
		}
	default: // podman : API compatible docker, TLS par un proxy (podman n'en a pas)
		out = append(out, "podman", "--url", e.B.Host)
	}
	return append(out, args...)
}

func regularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// resetResolveCache : vide le cache de résolution (tests).
func resetResolveCache() {
	resolveMu.Lock()
	resolveCache = map[string]resolvedHost{}
	resolveMu.Unlock()
}
