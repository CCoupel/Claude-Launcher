package core

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
)

// ── Accès authentifié du launcher de cluster (v7.0.0, contracts/auth.md) ─────
//
// Mode d'accès « proxy » : oauth2-proxy (sidecar du pod) authentifie et pose X-Forwarded-User ; le launcher n'écoute que
// sur la boucle locale du pod et ne lit l'identité que de ce proxy. Aucun jeton, aucune session ni cookie du launcher.

const (
	AccessProxy = "proxy" // identité posée par oauth2-proxy (X-Forwarded-User), launcher en boucle locale

	// LocalAdminLogin : compte local de secours (htpasswd d'oauth2-proxy) ; un login GitHub ne contient jamais « @ ».
	LocalAdminLogin = "admin@local"

	AuthAdminsVar     = "CL_AUTH_ADMINS"
	AuthUsersVar      = "CL_AUTH_USERS"
	AuthLocalHostsVar = "CL_AUTH_LOCAL_ADMIN_HOSTS"
	WebAltURLsVar     = "CL_WEB_ALT_URLS"

	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Refus d'identité (en-tête X-Launcher-Refusal) et leurs corps.
const (
	RefusalIdentity = "identity"
	RefusalUser     = "user"
	MsgNoIdentity   = "identité absente : ouvrir l'adresse du launcher"
	MsgUserDenied   = "compte GitHub non autorisé"
)

// forme d'un login GitHub (contracts/auth.md §2)
var loginRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// Identity : l'identité d'une requête en mode proxy.
type Identity struct {
	Login string // forme canonique (minuscules) ou « admin@local »
	Role  string // admin | user
}

// Local : compte local de secours.
func (i Identity) Local() bool { return i.Login == LocalAdminLogin }

// AuthConf : listes de logins et hôtes d'admission du compte local.
type AuthConf struct {
	Admins     map[string]bool
	Users      map[string]bool
	LocalHosts []string // hôtes (minuscules, sans port) où admin@local est admis ; vide : compte local refusé partout
}

// ParseLogins : logins séparés par des virgules (espaces ignorés), canoniques (minuscules). Une entrée invalide est refusée
// en la nommant.
func ParseLogins(name, s string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, e := range strings.Split(s, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !loginRE.MatchString(e) {
			return nil, fmt.Errorf("%s : login %q invalide (forme d'un login GitHub attendue)", name, e)
		}
		out[strings.ToLower(e)] = true
	}
	return out, nil
}

// ParseAuthConf : listes brutes (CL_AUTH_ADMINS, CL_AUTH_USERS) et hôtes du compte local (CL_AUTH_LOCAL_ADMIN_HOSTS).
func ParseAuthConf(admins, users, localHosts string) (AuthConf, error) {
	var c AuthConf
	var err error
	if c.Admins, err = ParseLogins(AuthAdminsVar, admins); err != nil {
		return AuthConf{}, err
	}
	if c.Users, err = ParseLogins(AuthUsersVar, users); err != nil {
		return AuthConf{}, err
	}
	for _, h := range strings.Split(localHosts, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			c.LocalHosts = append(c.LocalHosts, h)
		}
	}
	return c, nil
}

// LoadAuthConf : lit les variables d'environnement.
func LoadAuthConf() (AuthConf, error) {
	return ParseAuthConf(os.Getenv(AuthAdminsVar), os.Getenv(AuthUsersVar), os.Getenv(AuthLocalHostsVar))
}

// LocalEnabled : le compte local est admis sur au moins un hôte.
func (c AuthConf) LocalEnabled() bool { return len(c.LocalHosts) > 0 }

// localAdmitted : admin@local est admis pour l'hôte de la requête (en-tête Host, port retiré).
func (c AuthConf) localAdmitted(reqHost string) bool {
	h := reqHost
	if hh, _, err := net.SplitHostPort(reqHost); err == nil {
		h = hh
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	for _, a := range c.LocalHosts {
		if a == h {
			return true
		}
	}
	return false
}

// Identify : identité d'une requête d'après les valeurs de l'en-tête X-Forwarded-User (toutes) et l'en-tête Host. Refus :
// kind = RefusalIdentity (401 : absent, vide, invalide, en plusieurs exemplaires) ou RefusalUser (403 : login connu d'aucune
// liste, ou compte local hors des hôtes admis).
func (c AuthConf) Identify(userHeader []string, reqHost string) (id Identity, refusal string) {
	if len(userHeader) != 1 {
		return Identity{}, RefusalIdentity
	}
	v := strings.TrimSpace(userHeader[0])
	switch {
	case v == LocalAdminLogin:
		if !c.localAdmitted(reqHost) {
			return Identity{}, RefusalUser
		}
		return Identity{Login: v, Role: RoleAdmin}, ""
	case !loginRE.MatchString(v):
		return Identity{}, RefusalIdentity
	}
	l := strings.ToLower(v)
	switch {
	case c.Admins[l]:
		return Identity{Login: l, Role: RoleAdmin}, ""
	case c.Users[l]:
		return Identity{Login: l, Role: RoleUser}, ""
	}
	return Identity{}, RefusalUser
}

// StartRefusal : refus de démarrer du mode proxy (contracts/auth.md §2) ; "" si admis. loopback : écoute en boucle locale ;
// pub : adresse publique (CL_WEB_URL / --url).
func (c AuthConf) StartRefusal(loopback bool, pub *PublicURL) string {
	switch {
	case !loopback:
		return "accès proxy : écoute en boucle locale exigée (le proxy d'authentification est le seul client)"
	case pub == nil || !pub.HTTPS():
		return "accès proxy : adresse publique https exigée (CL_WEB_URL)"
	case len(c.Admins) == 0 && !c.LocalEnabled():
		return "accès proxy : aucun administrateur (CL_AUTH_ADMINS ou compte local)"
	}
	return ""
}

// ParseAltURLs : CL_WEB_ALT_URLS — autres adresses publiques admises (virgules), mêmes règles que CL_WEB_URL.
func ParseAltURLs(raw string) ([]PublicURL, error) {
	var out []PublicURL
	for _, e := range strings.Split(raw, ",") {
		if e = strings.TrimSpace(e); e == "" {
			continue
		}
		p, err := ParsePublicURL(e)
		if err != nil {
			return nil, fmt.Errorf("%s : %v", WebAltURLsVar, err)
		}
		if !p.HTTPS() {
			return nil, fmt.Errorf("%s : %q : https exigé", WebAltURLsVar, e)
		}
		out = append(out, p)
	}
	return out, nil
}

// r1Methods : écritures sensibles (règle R1, contracts/auth.md §4).
var r1Methods = map[string]bool{
	"configSave": true, "configApply": true, "projectSettings": true, "projectApply": true, "execApply": true,
	"envTrustAnswer": true, "initProposalAnswer": true, "backendSecretSet": true, "backendSecretDelete": true,
	"backendKnownHostsSet": true, "backendHostScan": true, "relaunch": true,
}

// R1Method : l'appel est une écriture sensible réservée au propriétaire ou à un administrateur authentifié.
func R1Method(method string) bool { return r1Methods[method] }

// R1Allowed : R1 permise selon le mode d'accès web et le rôle (table de contracts/auth.md §4) — la fenêtre native n'appelle
// pas cette fonction (toujours permise).
func R1Allowed(access string, id *Identity) bool {
	switch access {
	case AccessProxy:
		return id != nil && id.Role == RoleAdmin
	case AccessNone:
		return false
	}
	return true // jeton : le jeton désigne le propriétaire du poste
}

// AuditFrequent : méthodes agrégées par (utilisateur, méthode) et par minute dans le journal d'audit (P3).
var auditFrequent = map[string]bool{
	"termInput": true, "termResize": true, "termFocus": true, "termSync": true, "termZoom": true, "resizePane": true,
	"launchInput": true, "cardsFor": true, "select": true, "init": true, "clientInfo": true, "backendStates": true,
	"setPrefs": true,
}
