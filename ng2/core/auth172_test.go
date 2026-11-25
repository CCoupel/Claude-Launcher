package core

// Tests de spécification #172 / #251 / #252 (v7.0.0) — contracts/auth.md §2 (mode proxy, identité, refus de démarrer,
// adresses publiques), §4 (règle R1), §5 (méthodes fréquentes). Écrits depuis le contrat avant le code (TDD).
// Aucun appel réseau ni fichier : fonctions pures.

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func conf172(t *testing.T, admins, users, hosts string) AuthConf {
	t.Helper()
	c, err := ParseAuthConf(admins, users, hosts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Listes : virgules, espaces ignorés, forme canonique en minuscules ; entrée invalide → refus qui la nomme.
func TestAuth172ParseLogins(t *testing.T) {
	t.Parallel()
	got, err := ParseLogins("CL_AUTH_ADMINS", " CCoupel , alice-1,,bob ")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for k := range got {
		names = append(names, k)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "alice-1,bob,ccoupel" {
		t.Errorf("logins = %v", names)
	}
	for _, bad := range []string{"-lead", "trail-", "a b", "alice@x", "a_b", "é", "ccoupel;rm", strings.Repeat("a", 40)} {
		_, err := ParseLogins("CL_AUTH_USERS", "ok,"+bad)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(bad)) {
			t.Errorf("%q : refus nommant l'entrée attendu, obtenu %v", bad, err)
		}
	}
	if m, err := ParseLogins("X", ""); err != nil || len(m) != 0 {
		t.Errorf("liste vide : %v %v", m, err)
	}
	if _, err := ParseLogins("X", strings.Repeat("a", 39)); err != nil {
		t.Errorf("39 caractères (maximum d'un login GitHub) refusé : %v", err)
	}
}

// Identité : un seul en-tête valide ; rôle d'après les listes ; casse ignorée ; reste = 401 (identité) ou 403 (user).
func TestAuth172Identify(t *testing.T) {
	t.Parallel()
	c := conf172(t, "CCoupel", "alice", "launcher.internal")
	type want struct {
		login, role, refusal string
	}
	for name, tc := range map[string]struct {
		hdr  []string
		host string
		want want
	}{
		"admin":                    {[]string{"ccoupel"}, "launcher.internal", want{"ccoupel", "admin", ""}},
		"admin casse ignorée":      {[]string{"CCOUPEL"}, "launcher.internal", want{"ccoupel", "admin", ""}},
		"user":                     {[]string{"alice"}, "launcher.internal", want{"alice", "user", ""}},
		"espaces autour":           {[]string{"  alice "}, "launcher.internal", want{"alice", "user", ""}},
		"login inconnu":            {[]string{"mallory"}, "launcher.internal", want{"", "", RefusalUser}},
		"en-tête absent":           {nil, "launcher.internal", want{"", "", RefusalIdentity}},
		"valeur vide":              {[]string{""}, "launcher.internal", want{"", "", RefusalIdentity}},
		"valeur invalide":          {[]string{"a b"}, "launcher.internal", want{"", "", RefusalIdentity}},
		"valeur avec @":            {[]string{"ccoupel@example.com"}, "launcher.internal", want{"", "", RefusalIdentity}},
		"plusieurs en-têtes":       {[]string{"ccoupel", "ccoupel"}, "launcher.internal", want{"", "", RefusalIdentity}},
		"deux dont un autre":       {[]string{"alice", "ccoupel"}, "launcher.internal", want{"", "", RefusalIdentity}},
		"injection de ligne":       {[]string{"alice\r\nX-Evil: 1"}, "launcher.internal", want{"", "", RefusalIdentity}},
		"admin@local hôte interne": {[]string{"admin@local"}, "launcher.internal", want{"admin@local", "admin", ""}},
		"admin@local avec port":    {[]string{"admin@local"}, "launcher.internal:8080", want{"admin@local", "admin", ""}},
		"admin@local hôte casse":   {[]string{"admin@local"}, "Launcher.Internal", want{"admin@local", "admin", ""}},
		"admin@local hôte externe": {[]string{"admin@local"}, "launcher.example.org", want{"", "", RefusalUser}},
		"admin@local sans hôte":    {[]string{"admin@local"}, "", want{"", "", RefusalUser}},
		"admin@local majuscules":   {[]string{"ADMIN@LOCAL"}, "launcher.internal", want{"", "", RefusalIdentity}},
	} {
		id, why := c.Identify(tc.hdr, tc.host)
		if id.Login != tc.want.login || id.Role != tc.want.role || why != tc.want.refusal {
			t.Errorf("%s : (%q, %q, %q), attendu %+v", name, id.Login, id.Role, why, tc.want)
		}
	}
}

// admin@local est admin quel que soit CL_AUTH_ADMINS ; compte local désactivé (aucun hôte) : refusé partout.
func TestAuth172LocalAccount(t *testing.T) {
	t.Parallel()
	off := conf172(t, "ccoupel", "", "")
	if off.LocalEnabled() {
		t.Error("LocalEnabled avec aucun hôte")
	}
	if _, why := off.Identify([]string{"admin@local"}, "launcher.internal"); why != RefusalUser {
		t.Errorf("compte local désactivé : refus %q, attendu %q", why, RefusalUser)
	}
	on := conf172(t, "", "", "a.internal, b.internal")
	if !on.LocalEnabled() {
		t.Error("LocalEnabled faux avec des hôtes")
	}
	for _, h := range []string{"a.internal", "b.internal"} {
		if id, why := on.Identify([]string{"admin@local"}, h); why != "" || id.Role != RoleAdmin {
			t.Errorf("hôte %s : %+v %q", h, id, why)
		}
	}
	if !(Identity{Login: "admin@local"}).Local() || (Identity{Login: "alice"}).Local() {
		t.Error("Identity.Local")
	}
}

// Refus de démarrer (§2) : messages exacts.
func TestAuth172StartRefusal(t *testing.T) {
	t.Parallel()
	pub := func(s string) *PublicURL {
		p, err := ParsePublicURL(s)
		if err != nil {
			t.Fatal(err)
		}
		return &p
	}
	withAdmin := conf172(t, "ccoupel", "", "")
	local := conf172(t, "", "", "launcher.internal")
	none := conf172(t, "", "alice", "")
	for name, tc := range map[string]struct {
		c        AuthConf
		loopback bool
		pub      *PublicURL
		want     string
	}{
		"ok admin":                  {withAdmin, true, pub("https://launcher.example"), ""},
		"ok compte local seul":      {local, true, pub("https://launcher.example"), ""},
		"hors boucle locale":        {withAdmin, false, pub("https://launcher.example"), "accès proxy : écoute en boucle locale exigée (le proxy d'authentification est le seul client)"},
		"URL absente":               {withAdmin, true, nil, "accès proxy : adresse publique https exigée (CL_WEB_URL)"},
		"URL http":                  {withAdmin, true, pub("http://launcher.example"), "accès proxy : adresse publique https exigée (CL_WEB_URL)"},
		"aucun administrateur":      {none, true, pub("https://launcher.example"), "accès proxy : aucun administrateur (CL_AUTH_ADMINS ou compte local)"},
		"users sans admin ni local": {none, true, pub("https://launcher.example"), "accès proxy : aucun administrateur (CL_AUTH_ADMINS ou compte local)"},
	} {
		if got := tc.c.StartRefusal(tc.loopback, tc.pub); got != tc.want {
			t.Errorf("%s : %q, attendu %q", name, got, tc.want)
		}
	}
}

// CL_WEB_ALT_URLS : https, sans chemin, mêmes règles que CL_WEB_URL ; invalide → erreur nommant la variable.
func TestAuth172AltURLs(t *testing.T) {
	t.Parallel()
	got, err := ParseAltURLs("https://a.example, https://b.example:8443")
	if err != nil || len(got) != 2 || got[0].Origin != "https://a.example" || got[1].Origin != "https://b.example:8443" {
		t.Fatalf("ParseAltURLs : %+v %v", got, err)
	}
	if got, err := ParseAltURLs(""); err != nil || len(got) != 0 {
		t.Errorf("vide : %v %v", got, err)
	}
	for _, bad := range []string{"http://a.example", "a.example", "https://a.example/chemin", "https://user@a.example", "ftp://a.example"} {
		if _, err := ParseAltURLs("https://ok.example," + bad); err == nil || !strings.Contains(err.Error(), "CL_WEB_ALT_URLS") {
			t.Errorf("%q : erreur nommant CL_WEB_ALT_URLS attendue, obtenu %v", bad, err)
		}
	}
}

// Le mode proxy est un mode d'accès admis (variable et réglage).
func TestAuth172EffectiveAccessProxy(t *testing.T) {
	t.Setenv(WebAccessVar, "proxy")
	if a, err := EffectiveWebAccess(DesktopWebConf{}); err != nil || a != AccessProxy {
		t.Errorf("CL_WEB_ACCESS=proxy : %q %v", a, err)
	}
	t.Setenv(WebAccessVar, "")
	if a, err := EffectiveWebAccess(DesktopWebConf{Access: "proxy"}); err != nil || a != AccessProxy {
		t.Errorf("desktop_web.access: proxy : %q %v", a, err)
	}
}

// Classement des méthodes (contracts/auth.md §4, addendum 2) : liste exacte de chaque classe — ni plus ni moins.
func TestAuth172MethodClasses(t *testing.T) {
	t.Parallel()
	want := map[Class][]string{
		ClassR1: {"configSave", "configApply", "backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "relaunch", "login"},
		ClassProject: {"create", "createCancel", "projectSettings", "projectApply", "execApply", "envTrustAnswer", "initProposalAnswer",
			"projectMove", "projectMoveResume", "projectMoveAbort", "categoryCreate", "backendTest", "backendHostScan", "open", "reattach",
			"refreshProject", "forceClose", "forceCloseConfirm", "close", "closeConfirm", "detach", "refresh", "rescan", "teamToggle",
			"relayout", "select", "cardsFor", "setPrefs", "setTheme", "dismiss", "alert"},
		ClassRead: {"init", "clientInfo", "diag", "tools", "newInfo", "projectMoveInfo", "envTrustInfo", "projectBases", "projectCategories",
			"configFiles", "configOpen", "configValidate", "configModel", "backendStates", "launcherInfo", "backendSecretGet"},
		ClassTerminal:    {"termInput", "launchInput", "resizePane", "termResize", "termFocus", "termZoom", "termSync"},
		ClassProxyDenied: {"quit", "webUIOpen", "webUIRegen", "clipboardRead", "clipboardWrite", "teamAgentSet", "secretReveal"},
	}
	total := 0
	for class, ms := range want {
		for _, m := range ms {
			total++
			if c, known := ClassOf(m); !known || c != class {
				t.Errorf("%s : classe %v (connue %v), attendu %v", m, c, known, class)
			}
		}
	}
	if got := len(ClassifiedMethods()); got != total {
		t.Errorf("%d méthodes classées, attendu %d (liste du contrat) : %v", got, total, ClassifiedMethods())
	}
	// R1Method : seulement la classe R1 (les écritures de projets et de backends ne le sont plus — décision utilisateur)
	for _, m := range []string{"projectSettings", "projectApply", "execApply", "envTrustAnswer", "initProposalAnswer", "backendHostScan",
		"backendSecretGet", "refresh", "termInput", "init", "create", "projectMove"} {
		if R1Method(m) {
			t.Errorf("%s ne doit plus être R1", m)
		}
	}
	for _, m := range want[ClassR1] {
		if !R1Method(m) {
			t.Errorf("%s devrait être R1", m)
		}
	}
}

// Échec fermé : une méthode non classée est R1 (et signalée « non connue »), quelle que soit la casse ou la forme.
func TestAuth172UnknownMethodIsR1(t *testing.T) {
	t.Parallel()
	for _, m := range []string{"", "inconnue", "CONFIGSAVE", "configsave", "refresh ", "future_method", "../relaunch"} {
		c, known := ClassOf(m)
		if known || c != ClassR1 || !R1Method(m) {
			t.Errorf("%q : classe %v connue %v R1 %v, attendu non connue et R1", m, c, known, R1Method(m))
		}
	}
}

// configSave / configApply : R1 selon le fichier (config.conf, env.yaml du launcher, hub) ; project-env : libre.
func TestAuth172ConfigKindR1(t *testing.T) {
	t.Parallel()
	for kind, want := range map[string]bool{"config.conf": true, "env.yaml": true, "hub": true, "project-env": false, "backends.yaml": false, "": false, "autre": false} {
		if got := ConfigKindR1(kind); got != want {
			t.Errorf("ConfigKindR1(%q) = %v, attendu %v (backends.yaml : décidé par AdminFieldsChanged)", kind, got, want)
		}
	}
}

// backends.yaml : R1 seulement quand un champ R1 change (rubriques globales, kubeconfig, context, volumes, namespace, type host,
// clés de pod sensibles) ; image, shared, host ssh, ressources, placement : libres ; YAML illisible = changé (échec fermé).
func TestAuth172AdminFieldsChanged(t *testing.T) {
	t.Parallel()
	const base = "backends:\n  k: {type: k8s, namespace: n, context: c, volumes: {/a: pvc}, image: img:1, shared: [/a]}\n  s: {type: ssh, host: h1, shared: [/a]}\n"
	for name, tc := range map[string]struct {
		old, new string
		want     bool
	}{
		"identique":                      {base, base, false},
		"vide vers vide":                 {"", "", false},
		"image changée":                  {base, strings.Replace(base, "img:1", "img:2", 1), false},
		"shared changé":                  {base, strings.Replace(base, "shared: [/a]}\n  s", "shared: [/a, /b]}\n  s", 1), false},
		"hôte ssh changé":                {base, strings.Replace(base, "host: h1", "host: h2", 1), false},
		"backend ssh ajouté":             {base, base + "  t: {type: ssh, host: h3, shared: [/a]}\n", false},
		"backend supprimé":               {base, "backends:\n  s: {type: ssh, host: h1, shared: [/a]}\n", true}, // k portait volumes, context, namespace
		"ressources du pod":              {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {resources: {limits: {memory: 4Gi}}}", 1), false},
		"volumes changé":                 {base, strings.Replace(base, "{/a: pvc}", "{/a: autre}", 1), true},
		"namespace changé":               {base, strings.Replace(base, "namespace: n", "namespace: kube-system", 1), true},
		"context changé":                 {base, strings.Replace(base, "context: c", "context: admin", 1), true},
		"kubeconfig ajouté":              {base, strings.Replace(base, "image: img:1", "image: img:1, kubeconfig: /etc/k", 1), true},
		"type host ajouté":               {base, base + "  h: {type: host}\n", true},
		"serviceAccountName":             {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {serviceAccountName: admin}", 1), true},
		"automountServiceAccountToken":   {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {automountServiceAccountToken: true}", 1), true},
		"securityContext":                {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {securityContext: {runAsUser: 0}}", 1), true},
		"nodeName":                       {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {nodeName: n1}", 1), true},
		"runtimeClassName":               {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {runtimeClassName: x}", 1), true},
		"hostAliases":                    {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {hostAliases: [{ip: 1.2.3.4, hostnames: [a]}]}", 1), true},
		"dnsConfig":                      {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {dnsConfig: {nameservers: [1.2.3.4]}}", 1), true},
		"tolerations (m2)":               {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {tolerations: [{key: node-role.kubernetes.io/control-plane, operator: Exists}]}", 1), true},
		"priorityClassName (m2)":         {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {priorityClassName: system-cluster-critical}", 1), true},
		"schedulerName (m2)":             {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {schedulerName: autre}", 1), true},
		"imagePullSecrets (m2)":          {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {imagePullSecrets: [{name: s}]}", 1), true},
		"dnsPolicy":                      {base, strings.Replace(base, "image: img:1", "image: img:1, pod: {dnsPolicy: None}", 1), true},
		"rubrique globale desktop_web":   {base, base + "desktop_web: {enabled: true}\n", true},
		"rubrique globale browser_paths": {base, base + "browser_paths: [{local: a, remote: b}]\n", true},
		"rubrique globale hubs":          {base, base + "hubs: {x: {}}\n", true},
		"défauts globaux":                {base, "defaults:\n  browser_server: {listen: off}\n" + base, true},
		"YAML illisible (nouveau)":       {base, "backends: [\n", true},
		"YAML illisible (ancien)":        {"backends: [\n", base, true},
		"deux illisibles":                {"{{", "[[", true},
	} {
		if got := AdminFieldsChanged([]byte(tc.old), []byte(tc.new)); got != tc.want {
			t.Errorf("%s : %v, attendu %v", name, got, tc.want)
		}
	}
}

// access (init, clientInfo) : capacités d'un client web, seule source du grisage ; deny calculé par le serveur.
func TestAuth172AccessFor(t *testing.T) {
	t.Parallel()
	admin, user := &Identity{Login: "a", Role: RoleAdmin}, &Identity{Login: "u", Role: RoleUser}
	r1 := []string{"configSave:config.conf", "configSave:env.yaml", "configSave:hub", "configApply:config.conf", "configApply:env.yaml", "configApply:hub",
		"backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "relaunch", "login"}
	denied := []string{"quit", "webUIOpen", "webUIRegen", "teamAgentSet", "secretReveal"}
	has := func(ai AccessInfo, ms []string) []string {
		var miss []string
		for _, m := range ms {
			found := false
			for _, d := range ai.Deny {
				if d == m {
					found = true
				}
			}
			if !found {
				miss = append(miss, m)
			}
		}
		return miss
	}
	ai := AccessFor(AccessProxy, user)
	if ai.Mode != "proxy" || ai.Role != "user" || !ai.BackendsAdminFields || len(has(ai, r1)) != 0 || len(has(ai, denied)) != 0 {
		t.Errorf("proxy user = %+v, manque %v %v", ai, has(ai, r1), has(ai, denied))
	}
	ai = AccessFor(AccessProxy, admin)
	if ai.Role != "admin" || ai.BackendsAdminFields || len(has(ai, r1)) != len(r1) || len(has(ai, denied)) != 0 {
		t.Errorf("proxy admin = %+v : seuls les refus du mode proxy attendus", ai)
	}
	ai = AccessFor(AccessProxy, nil)
	if len(has(ai, r1)) != 0 || !ai.BackendsAdminFields {
		t.Errorf("proxy sans identité = %+v : tout refusé", ai)
	}
	ai = AccessFor(AccessNone, nil)
	if ai.Mode != "none" || ai.Role != "" || !ai.BackendsAdminFields || len(has(ai, r1)) != 0 {
		t.Errorf("none = %+v, attendu toutes les méthodes R1 refusées, sans rôle", ai)
	}
	for _, mode := range []string{AccessToken, ""} {
		ai = AccessFor(mode, nil)
		if ai.Mode != "token" || len(ai.Deny) != 0 || ai.BackendsAdminFields {
			t.Errorf("mode %q = %+v, attendu token sans refus", mode, ai)
		}
	}
	if b, _ := json.Marshal(AccessFor(AccessToken, nil)); !strings.Contains(string(b), `"deny":[]`) {
		t.Errorf("deny doit être [] (pas null) : %s", b)
	}
}

// Comparaison d'hôtes (m2) : minuscules, port retiré, point final retiré, puis ÉGALITÉ stricte ; valeur illisible → refus.
func TestAuth172HostComparison(t *testing.T) {
	t.Parallel()
	c := conf172(t, "ccoupel", "", "Launcher.Internal.")
	for host, want := range map[string]bool{
		"launcher.internal": true, "LAUNCHER.INTERNAL": true, "launcher.internal.": true, "Launcher.Internal.:8443": true,
		"launcher.internal:443": true, "launcher.internal.evil.com": false, "evil-launcher.internal": false, "xlauncher.internal": false,
		"launcher.internal@evil.com": false, "evil.com#launcher.internal": false, "launcher.internal..": false, "launcher.internal ": false,
		" launcher.internal": false, "launcher.internal/x": false, "[::1]": false, "": false, "launcher.internal:notaport": false,
	} {
		_, why := c.Identify([]string{"admin@local"}, host)
		if (why == "") != want {
			t.Errorf("Host %q : admis %v, attendu %v (refus %q)", host, why == "", want, why)
		}
	}
}

// P3 : méthodes fréquentes agrégées — liste exacte du contrat.
func TestAuth172AuditFrequentList(t *testing.T) {
	t.Parallel()
	want := []string{"termInput", "termResize", "termFocus", "termSync", "termZoom", "resizePane", "launchInput", "cardsFor",
		"select", "init", "clientInfo", "backendStates", "setPrefs"}
	if len(auditFrequent) != len(want) {
		t.Errorf("%d méthodes fréquentes, attendu %d : %v", len(auditFrequent), len(want), auditFrequent)
	}
	for _, m := range want {
		if !auditFrequent[m] {
			t.Errorf("%s devrait être agrégée", m)
		}
	}
	for m := range r1Methods {
		if auditFrequent[m] {
			t.Errorf("%s est R1 : jamais agrégée (une entrée par appel)", m)
		}
	}
}

// Pas de dérive des listes : le champ access.deny est GÉNÉRÉ depuis le classement (toute méthode de la classe R1 sauf
// configSave / configApply — refusés par kind —, et toute méthode refusée en proxy, clipboard* compris).
func TestAuth172AccessDenyDerivedFromClasses(t *testing.T) {
	t.Parallel()
	user := AccessFor(AccessProxy, &Identity{Login: "u", Role: RoleUser})
	admin := AccessFor(AccessProxy, &Identity{Login: "a", Role: RoleAdmin})
	has := func(ai AccessInfo, m string) bool {
		for _, d := range ai.Deny {
			if d == m {
				return true
			}
		}
		return false
	}
	for _, m := range ClassifiedMethods() {
		c, _ := ClassOf(m)
		switch {
		case c == ClassProxyDenied:
			if !has(user, m) || !has(admin, m) {
				t.Errorf("%s (refusée en proxy) absente de access.deny (user %v, admin %v)", m, has(user, m), has(admin, m))
			}
		case c == ClassR1 && m != "configSave" && m != "configApply":
			if !has(user, m) || has(admin, m) {
				t.Errorf("%s (R1) : deny user %v, admin %v, attendu vrai et faux", m, has(user, m), has(admin, m))
			}
		case c != ClassR1 && c != ClassProxyDenied:
			if has(user, m) || has(admin, m) {
				t.Errorf("%s (libre) présente dans access.deny", m)
			}
		}
	}
	for _, kind := range []string{"config.conf", "env.yaml", "hub"} {
		for _, m := range []string{"configSave", "configApply"} {
			if !has(user, m+":"+kind) || has(admin, m+":"+kind) {
				t.Errorf("%s:%s : user %v admin %v", m, kind, has(user, m+":"+kind), has(admin, m+":"+kind))
			}
		}
	}
}
