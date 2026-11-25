package core

// Tests de spécification #172 / #251 / #252 (v7.0.0) — contracts/auth.md §2 (mode proxy, identité, refus de démarrer,
// adresses publiques), §4 (règle R1), §5 (méthodes fréquentes). Écrits depuis le contrat avant le code (TDD).
// Aucun appel réseau ni fichier : fonctions pures.

import (
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

// R1 : la liste exacte du contrat (P4) — ni plus ni moins ; backendSecretGet et les lectures n'en sont pas.
func TestAuth172R1MethodList(t *testing.T) {
	t.Parallel()
	want := []string{"configSave", "configApply", "projectSettings", "projectApply", "execApply", "envTrustAnswer",
		"initProposalAnswer", "backendSecretSet", "backendSecretDelete", "backendKnownHostsSet", "backendHostScan", "relaunch"}
	for _, m := range want {
		if !R1Method(m) {
			t.Errorf("%s devrait être R1", m)
		}
	}
	for _, m := range []string{"backendSecretGet", "secretReveal", "configModel", "teamAgentSet", "init", "clientInfo", "refresh",
		"termInput", "select", "cardsFor", "setPrefs", "", "CONFIGSAVE"} {
		if R1Method(m) {
			t.Errorf("%q ne doit pas être R1", m)
		}
	}
}

// R1 selon le mode et le rôle (tableau §4).
func TestAuth172R1Allowed(t *testing.T) {
	t.Parallel()
	admin, user := &Identity{Login: "ccoupel", Role: RoleAdmin}, &Identity{Login: "alice", Role: RoleUser}
	for name, tc := range map[string]struct {
		access string
		id     *Identity
		want   bool
	}{
		"jeton":                  {AccessToken, nil, true},
		"défaut (vide)":          {"", nil, true},
		"proxy admin":            {AccessProxy, admin, true},
		"proxy admin@local":      {AccessProxy, &Identity{Login: LocalAdminLogin, Role: RoleAdmin}, true},
		"proxy user":             {AccessProxy, user, false},
		"proxy sans identité":    {AccessProxy, nil, false},
		"proxy rôle inconnu":     {AccessProxy, &Identity{Login: "x", Role: ""}, false},
		"none":                   {AccessNone, nil, false},
		"none avec un « admin »": {AccessNone, admin, false},
	} {
		if got := R1Allowed(tc.access, tc.id); got != tc.want {
			t.Errorf("%s : %v, attendu %v", name, got, tc.want)
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
