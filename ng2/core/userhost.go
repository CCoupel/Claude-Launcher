package core

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
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

// HostError : refus d'hôte. Refus de PLAGE ou de RÉSOLUTION (IP, nom interne, DNS en échec) : texte unique HostRefusal (aucun oracle de
// topologie, S8). Refus de FORME (schéma, port, caractère, chemin) : message propre et utile, sans lien avec la topologie. Reason
// sert au diagnostic côté serveur (audit) : « form » (syntaxe), « range » (IP ou nom interdit), « dns » (résolution en échec).
type HostError struct{ Reason, Msg string }

func (e *HostError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return HostRefusal
}

// Uniform : le refus porte le texte unique (plage ou résolution) — code « forbidden_address » côté API.
func (e *HostError) Uniform() bool { return e.Msg == "" }

// HostRefusalCategory : raison interne d'un refus d'hôte (« » si l'erreur n'en est pas un).
func HostRefusalCategory(err error) string {
	var he *HostError
	if errors.As(err, &he) {
		return he.Reason
	}
	return ""
}

// HostRefusalUniform : l'erreur est un refus d'adresse au texte unique (plage ou résolution).
func HostRefusalUniform(err error) bool {
	var he *HostError
	return errors.As(err, &he) && he.Uniform()
}

func formErr(msg string) *HostError {
	return &HostError{Reason: "form", Msg: "adresse invalide : " + msg}
}

var (
	errForm    = &HostError{Reason: "form"} // forme numérique non canonique (127.1, décimal, hexadécimal) : texte unique, comme une plage
	errRange   = &HostError{Reason: "range"}
	errResolve = &HostError{Reason: "dns"}
	// errHostRefused : refus sans raison précise (compatibilité)
	errHostRefused error = errForm

	errFormChar   = formErr("caractère interdit")
	errFormScheme = formErr("schéma obligatoire (tcp://hôte:port)")
	errFormPort   = formErr("port obligatoire, de 1 à 65535 (tcp://hôte:port)")
	errFormPath   = formErr("ni chemin, ni requête, ni identifiant dans l'adresse")
	errFormLong   = formErr("adresse trop longue")
)

// HostRefusalHint : texte ajouté au message d'un refus de forme ou de plage (le même pour toutes les causes).
const HostRefusalHint = " (attendu : tcp://nom-public:port — Internet public seulement)"

// userHostResolver : résolveur du launcher (défaut : net.DefaultResolver) ; userHostCacheTTL : durée du cache de résolution (D1).
var (
	userHostResolver = tun.Of(func(host string) ([]net.IP, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	})
	userHostCacheTTL = tun.Of(30 * time.Second)
)

// UsersAllowPrivateVar : « true » (et rien d'autre) admet pour un utilisateur les plages PRIVÉES (10/8, 172.16/12, 192.168/16, fc00::/7) et les
// noms internes qui y mènent. Loopback, link-local, métadonnées, multicast et plages réservées restent TOUJOURS refusés. Posé par le chart
// (users.allowPrivateNetworks) ; lu à chaque appel : le retirer refuse l'appel suivant sans relire backends.yaml.
const UsersAllowPrivateVar = "CL_USERS_ALLOW_PRIVATE"

// UsersAllowPrivate : le paramètre est actif (valeur exactement « true » : tout autre cas, échec fermé).
func UsersAllowPrivate() bool { return os.Getenv(UsersAllowPrivateVar) == "true" }

func mustPrefixes(ps ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range ps {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

// plages TOUJOURS refusées à un utilisateur (netip.Prefix, jamais une regex sur la chaîne). fd00:ec2::/32 : métadonnées AWS v6, dans fc00::/7.
var alwaysBlockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16",
	"fd00:ec2::/32", "fe80::/10", "fec0::/10", "ff00::/8")

// plages PRIVÉES : refusées par défaut, admises avec CL_USERS_ALLOW_PRIVATE=true
var privatePrefixes = mustPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")

// ipAllowed : l'adresse est atteignable par un utilisateur. IPv4-mappée déballée avant le contrôle ; NAT64, 6to4, Teredo, IPv4-compatible
// refusés en bloc ; plages privées selon UsersAllowPrivate.
func ipAllowed(a netip.Addr) bool {
	a = a.Unmap().WithZone("")
	if !a.IsValid() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range alwaysBlockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	for _, p := range privatePrefixes {
		if p.Contains(a) {
			return UsersAllowPrivate()
		}
	}
	return !a.IsPrivate()
}

// internalSuffixes : noms réservés à un réseau interne (le filtre d'IP reste la barrière réelle).
var internalSuffixes = []string{".local", ".lan", ".internal", ".home.arpa", ".svc", ".cluster.local", ".localdomain", ".localhost", ".home",
	".corp", ".intranet", ".private", ".invalid", ".arpa"}

// splitHostPort : « hôte:port » strict ; port décimal 1–65535 obligatoire.
func splitHostPort(hp string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(hp)
	if err != nil || h == "" || p == "" {
		return "", 0, errFormPort
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return "", 0, errFormPort
		}
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, errFormPort
	}
	return h, n, nil
}

// parseUserEndpoint : « scheme://hôte:port » → (hôte canonique, port). Refus de toute forme non canonique : userinfo, chemin, requête,
// fragment, zone IPv6, caractères de contrôle, espaces, numérique non « a.b.c.d », nom interne, IP interdite. Aucune résolution.
func parseUserEndpoint(raw, scheme string) (host string, port int, err error) {
	return parseUserEndpointPath(raw, scheme, false)
}

// serverPathRE : chemin d'un serveur d'API (Rancher : /k8s/clusters/c-xxxx) — segments simples, jamais « .. », requête ni fragment.
var serverPathRE = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+/?$`)

// parseUserEndpointPath : parseUserEndpoint ; path vrai : un chemin sûr est admis après « hôte:port » (l'hôte et le port, seuls
// responsables de la destination, restent bornés ; le chemin ne change ni l'IP ni le port atteints).
func parseUserEndpointPath(raw, scheme string, path bool) (host string, port int, err error) {
	raw = strings.TrimSpace(raw) // espaces ou saut de ligne autour de la valeur (champ d'éditeur) : sans effet sur la destination
	if !path {
		raw = strings.TrimSuffix(raw, "/") // « tcp://hôte:port/ » : une barre finale seule est tolérée
	}
	if len(raw) > 300 {
		return "", 0, errFormLong
	}
	for _, r := range raw {
		if strings.ContainsRune("@?#", r) {
			return "", 0, errFormPath
		}
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`\"'`+"`"+`$;|&<>{}()%`, r) {
			return "", 0, errFormChar
		}
	}
	pre := scheme + "://"
	if len(raw) <= len(pre) || !strings.EqualFold(raw[:len(pre)], pre) {
		return "", 0, errFormScheme
	}
	rest := raw[len(pre):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		if !path || !serverPathRE.MatchString(rest[i:]) || strings.Contains(rest[i:], "..") {
			return "", 0, errFormPath
		}
		rest = rest[:i]
	}
	h, port, err := splitHostPort(rest)
	if err != nil {
		return "", 0, err
	}
	if strings.HasPrefix(rest, "[") { // IPv6 entre crochets
		a, perr := netip.ParseAddr(h)
		if perr != nil || a.Zone() != "" || !a.Is6() && !a.Is4In6() {
			return "", 0, errFormChar
		}
		if !ipAllowed(a) {
			return "", 0, errRange
		}
		return a.String(), port, nil
	}
	if a, perr := netip.ParseAddr(h); perr == nil { // IP littérale (forme canonique décimale pointée, ou IPv6 sans crochets : refusée par SplitHostPort)
		if a.Zone() != "" {
			return "", 0, errFormChar
		}
		if !ipAllowed(a) {
			return "", 0, errRange
		}
		return a.String(), port, nil
	}
	n := strings.ToLower(strings.TrimSuffix(h, "."))
	allowPriv := UsersAllowPrivate()
	if len(n) == 0 || len(n) > 253 || n == "localhost" || !allowPriv && !strings.Contains(n, ".") {
		return "", 0, errRange // sans point : nom interne (localhost, intranet, kubernetes…) — admis avec le paramètre, sauf localhost
	}
	labels := strings.Split(n, ".")
	last := labels[len(labels)-1]
	if last == "" || isNumericLabel(last) {
		return "", 0, errHostRefused // 127.1, 2130706433, 0x7f.0.0.1, 0177.0.0.1 : forme numérique non canonique
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", 0, errFormChar
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", 0, errFormChar
			}
		}
	}
	if labels[0] == "kubernetes" || labels[0] == "localhost" || strings.HasSuffix("."+n, ".localhost") {
		return "", 0, errRange // jamais, même avec le paramètre
	}
	if !allowPriv { // les noms internes mènent à des plages privées : admis avec le paramètre (l'IP résolue reste contrôlée)
		for _, s := range internalSuffixes {
			if strings.HasSuffix("."+n, s) {
				return "", 0, errRange
			}
		}
		for _, l := range labels {
			if l == "svc" {
				return "", 0, errRange
			}
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
	ck := name + "|" + strconv.FormatBool(UsersAllowPrivate()) // le verdict dépend du paramètre : jamais celui de l'autre réglage
	resolveMu.Lock()
	if c, ok := resolveCache[ck]; ok && now.Before(c.until) {
		resolveMu.Unlock()
		return c.err
	}
	resolveMu.Unlock()
	ips, err := userHostResolver.Get()(name)
	// échec FERMÉ (audit LOT-3.1, M1) : une résolution qui échoue ou ne rend rien est un refus, jamais mis en cache (le prochain appel
	// réessaie) ; une seule IP interdite parmi celles rendues suffit à refuser.
	if err != nil || len(ips) == 0 {
		return errResolve
	}
	var res error
	for _, ip := range ips {
		a, ok := netip.AddrFromSlice(ip)
		if !ok || !ipAllowed(a) {
			res = errRange
			break
		}
	}
	resolveMu.Lock()
	if len(resolveCache) > 512 {
		resolveCache = map[string]resolvedHost{}
	}
	resolveCache[ck] = resolvedHost{until: now.Add(userHostCacheTTL.Get()), err: res}
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
	h, _, err := parseUserEndpointPath(raw, "https", true)
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
	out := append(envUnset(), "DOCKER_CONFIG="+dockerEmptyConfig())
	switch e.B.Type {
	case "docker":
		out = append(out, "docker", "-H", e.B.Host)
		if t := e.B.TLS; t != nil && t.Enabled {
			d := filepath.Join(e.B.vault, e.B.Name)
			use := func(f string) bool { return safeVaultFile(filepath.Join(d, f)) == nil }
			if t.verifies() {
				if !use("ca.pem") {
					return kubectlRefused(errors.New("CA du serveur absente ou refusée"))
				}
				out = append(out, "--tlsverify", "--tlscacert="+filepath.Join(d, "ca.pem"))
			} else {
				out = append(out, "--tls")
			}
			if use("cert.pem") && use("key.pem") {
				out = append(out, "--tlscert="+filepath.Join(d, "cert.pem"), "--tlskey="+filepath.Join(d, "key.pem"))
			}
		}
	default: // podman : API compatible docker, TLS par un proxy (podman n'en a pas) ; jamais l'auth.json du launcher
		out = append(out, "REGISTRY_AUTH_FILE="+filepath.Join(dockerEmptyConfig(), "auth.json"), "podman", "--url", e.B.Host)
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

// SetUserHostResolverForTest : remplace le résolveur (tests d'autres paquets) et vide le cache ; rend la fonction de restauration.
func SetUserHostResolverForTest(fn func(host string) ([]net.IP, error)) (restore func()) {
	old := userHostResolver.Set(fn)
	resetResolveCache()
	return func() { userHostResolver.Set(old); resetResolveCache() }
}

// dockerEmptyConfig : dossier de configuration docker vide et privé — le $HOME/.docker/config.json du launcher (assistants
// d'identifiants, registres) n'est jamais lu pour l'appel d'un utilisateur (audit LOT-3.1, m3).
func dockerEmptyConfig() string {
	d := filepath.Join(CacheDir(), "docker-empty")
	_ = os.MkdirAll(d, 0o700)
	return d
}

// safeVaultFile : fichier du coffre utilisable — régulier, pas un lien symbolique, droits ≤ 0600 (S23).
func safeVaultFile(p string) error {
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		return errors.New("fichier du coffre refusé (lien symbolique ou droits trop larges)")
	}
	return nil
}

// tlsExpiry : avertissements sur les certificats enregistrés (expirés ou à moins de 30 jours) ; expired : au moins un l'est.
func tlsExpiry(infos map[string]TLSCertInfo, now time.Time) (warns []string, expired bool) {
	for _, k := range []string{"ca", "cert"} {
		i, ok := infos[k]
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339, i.NotAfter)
		switch {
		case err != nil:
		case now.After(t):
			warns, expired = append(warns, "certificat « "+k+" » expiré"), true
		case t.Sub(now) < 30*24*time.Hour:
			warns = append(warns, "certificat « "+k+" » expire dans moins de 30 jours")
		}
	}
	return warns, expired
}

// TLSExpiryWarnings : avertissements d'expiration des certificats enregistrés (UI).
func TLSExpiryWarnings(infos map[string]TLSCertInfo) []string {
	w, _ := tlsExpiry(infos, time.Now())
	return w
}

// resolveBudget : durée totale accordée à la résolution des hôtes d'UNE validation (enregistrement) — au-delà, refus.
var resolveBudget = tun.Of(5 * time.Second)

// checkResolvedAll : résout les noms en parallèle (8 à la fois au plus) dans resolveBudget ; rend, pour chaque nom, son refus éventuel.
// Un nom dont la résolution n'a pas abouti dans le délai est refusé (échec fermé).
func checkResolvedAll(names []string) map[string]error {
	out := map[string]error{}
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var uniq []string
	for _, n := range names {
		if _, dup := out[n]; !dup {
			out[n] = errResolve // refus tant que le résultat n'est pas arrivé (délai dépassé)
			uniq = append(uniq, n)
		}
	}
	for _, n := range uniq {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := checkResolved(n)
			mu.Lock()
			out[n] = err
			mu.Unlock()
		}(n)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(resolveBudget.Get()):
	}
	mu.Lock()
	defer mu.Unlock()
	res := make(map[string]error, len(out))
	for k, v := range out {
		res[k] = v
	}
	return res
}

// HostMessage : message affiché pour un refus d'hôte — le texte unique (+ forme attendue) pour une plage ou une résolution, le message
// de forme sinon.
func HostMessage(err error) string {
	if HostRefusalUniform(err) {
		return HostRefusal + HostRefusalHint
	}
	return err.Error() + HostRefusalHint
}
