package core

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
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
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		n, ok := normHost(h)
		if !ok {
			return AuthConf{}, fmt.Errorf("%s : hôte %q invalide", AuthLocalHostsVar, h)
		}
		c.LocalHosts = append(c.LocalHosts, n)
	}
	return c, nil
}

// LoadAuthConf : lit les variables d'environnement.
func LoadAuthConf() (AuthConf, error) {
	return ParseAuthConf(os.Getenv(AuthAdminsVar), os.Getenv(AuthUsersVar), os.Getenv(AuthLocalHostsVar))
}

// LocalEnabled : le compte local est admis sur au moins un hôte.
func (c AuthConf) LocalEnabled() bool { return len(c.LocalHosts) > 0 }

var hostRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// normHost : hôte comparable (m2) — minuscules, port numérique retiré, UN point final retiré ; "" et false si illisible
// (espace, « @ », « # », « / », crochets, point final double, port non numérique).
func normHost(reqHost string) (string, bool) {
	h := reqHost
	if i := strings.LastIndex(h, ":"); i >= 0 {
		port := h[i+1:]
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", false
		}
		h = h[:i]
	}
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if h == "" || strings.Contains(h, "..") || !hostRE.MatchString(h) {
		return "", false
	}
	return h, true
}

// localAdmitted : admin@local est admis pour l'hôte de la requête (en-tête Host) : égalité stricte après normalisation.
func (c AuthConf) localAdmitted(reqHost string) bool {
	h, ok := normHost(reqHost)
	if !ok {
		return false
	}
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

// ── Classement des méthodes d'appel (contracts/auth.md §4, addendum 2) ──────────────────────────────────────────────

// Class : classe d'une méthode d'appel de l'interface.
type Class int

const (
	ClassR1          Class = iota // serveur et options globales : admin seulement (méthode non classée : ici aussi, échec fermé)
	ClassProject                  // projets et backends : libre pour un user
	ClassRead                     // lecture
	ClassTerminal                 // terminaux
	ClassProxyDenied              // refusé en mode proxy, tous rôles
)

var methodClass = map[string]Class{}

// r1Methods : méthodes R1 par nature (configSave / configApply : R1 selon le fichier, ConfigKindR1 / AdminFieldsChanged).
var r1Methods = map[string]bool{"configSave": true, "configApply": true, "backendSecretSet": true, "backendSecretDelete": true,
	"backendKnownHostsSet": true, "relaunch": true, "login": true}

func init() {
	for m := range r1Methods { // configSave / configApply : R1 ou projet selon le fichier ; classés « R1 » ici (échec fermé)
		methodClass[m] = ClassR1
	}
	for _, m := range []string{"create", "createCancel", "projectSettings", "projectApply", "execApply", "envTrustAnswer",
		"initProposalAnswer", "projectMove", "projectMoveResume", "projectMoveAbort", "categoryCreate", "backendTest",
		"backendHostScan", "open", "reattach", "refreshProject", "forceClose", "forceCloseConfirm", "close", "closeConfirm",
		"detach", "refresh", "rescan", "teamToggle", "relayout", "select", "cardsFor", "setPrefs", "setTheme", "dismiss", "alert"} {
		methodClass[m] = ClassProject
	}
	for _, m := range []string{"init", "clientInfo", "diag", "tools", "newInfo", "projectMoveInfo", "envTrustInfo", "projectBases",
		"projectCategories", "configFiles", "configOpen", "configValidate", "configModel", "backendStates", "launcherInfo", "backendSecretGet"} {
		methodClass[m] = ClassRead
	}
	for _, m := range []string{"termInput", "launchInput", "resizePane", "termResize", "termFocus", "termZoom", "termSync"} {
		methodClass[m] = ClassTerminal
	}
	for _, m := range []string{"quit", "webUIOpen", "webUIRegen", "clipboardRead", "clipboardWrite", "teamAgentSet", "secretReveal"} {
		methodClass[m] = ClassProxyDenied
	}
}

// ClassOf : classe d'une méthode ; known faux = méthode non classée (traitée comme R1, un test énumère le dispatch).
func ClassOf(method string) (c Class, known bool) {
	c, known = methodClass[method]
	if !known {
		return ClassR1, false
	}
	return c, true
}

// ClassifiedMethods : toutes les méthodes classées (tests).
func ClassifiedMethods() []string {
	out := make([]string, 0, len(methodClass))
	for m := range methodClass {
		out = append(out, m)
	}
	return out
}

// R1Method : l'appel est, au moins pour certains arguments, une écriture réservée à l'administrateur (configSave et
// configApply : voir ConfigR1 pour le fichier visé). Méthode non classée : R1.
func R1Method(method string) bool {
	c, _ := ClassOf(method)
	return c == ClassR1
}

// Refus du mode proxy (tous rôles), repris par le champ access.deny.
var proxyDeny = []string{"quit", "webUIOpen", "webUIRegen", "teamAgentSet", "secretReveal"}

// R1 par fichier de configuration (kind → R1 pour configSave / configApply) : config.conf, env.yaml du launcher, hub : R1 ;
// project-env : projet ; backends.yaml : R1 seulement si un champ R1 change (AdminFieldsChanged) — décidé par l'appelant.
func ConfigKindR1(kind string) bool {
	switch kind {
	case string(ConfConf), string(ConfLauncherEnv), "hub":
		return true
	}
	return false
}

// R1Allowed : un client web de ce mode et de ce rôle peut-il appeler une méthode R1 ? (fenêtre native : toujours, sans appel)
func R1Allowed(access string, id *Identity) bool {
	switch access {
	case AccessProxy:
		return id != nil && id.Role == RoleAdmin
	case AccessNone:
		return false
	}
	return true // jeton : le jeton désigne le propriétaire du poste
}

// AccessInfo : champ « access » d'init et de clientInfo (contracts/auth.md §3) — seule source du grisage côté client.
type AccessInfo struct {
	Mode                string   `json:"mode"`
	Role                string   `json:"role,omitempty"`
	Deny                []string `json:"deny"`
	BackendsAdminFields bool     `json:"backendsAdminFields"`
}

// AccessFor : capacités d'un client web selon le mode d'accès et l'identité.
func AccessFor(mode string, id *Identity) AccessInfo {
	if mode == "" {
		mode = AccessToken
	}
	r1 := []string{"configSave:config.conf", "configSave:env.yaml", "configSave:hub", "configApply:config.conf", "configApply:env.yaml",
		"configApply:hub", "backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "relaunch", "login"}
	ai := AccessInfo{Mode: mode, Deny: []string{}}
	switch mode {
	case AccessProxy:
		if id != nil {
			ai.Role = id.Role
		}
		if !R1Allowed(mode, id) {
			ai.Deny = append(ai.Deny, r1...)
			ai.BackendsAdminFields = true
		}
		ai.Deny = append(ai.Deny, proxyDeny...)
	case AccessNone:
		ai.Deny = append(ai.Deny, r1...)
		ai.BackendsAdminFields = true
	}
	return ai
}
