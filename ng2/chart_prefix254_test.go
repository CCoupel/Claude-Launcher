package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// LOT-3 addendum 4 (#254) — accès public par sous-chemin : `externalIngress.pathPrefix` (contracts/auth.md §9, helm-values.md).
// URL publique `https://coupel.net/claude-qualif` ; préfixe jamais retiré par Traefik ; cookie `__Secure-cl_auth_ext` (jamais
// `__Host-`, qui impose Path=/) ; pas de HSTS (P13) ; l'Ingress ne capture AUCUNE autre route de l'hôte partagé. Écrits avant le code :
// RED attendu tant que le chart ne connaît pas pathPrefix.

const (
	t254Shared = "coupel.net"
	t254Base   = "/claude-qualif"
)

func t254Prefix() m {
	v := t254Values()
	sub(v, "externalIngress")["host"] = t254Shared
	sub(v, "externalIngress")["pathPrefix"] = t254Base
	// hôte partagé : certificat déjà en place, fourni par secretName (clusterIssuer + pathPrefix est refusé au rendu, voir plus bas)
	sub(v, "externalIngress")["tls"] = m{"secretName": "coupel-net-tls"}
	return v
}

// ── validation du préfixe ─────────────────────────────────────────────────────────────────────────────────────────

func TestChartPrefix254Validation(t *testing.T) {
	needHelm(t)
	must(t, t254Prefix())
	for name, p := range map[string]string{
		"double barre au début":  "//claude-qualif",
		"double barre au milieu": "/claude//qualif",
		"remontée ..":            "/claude-qualif/../emby",
		"remontée seule":         "/..",
		"point unique":           "/.",
		"majuscules":             "/Claude-Qualif",
		"barre finale":           "/claude-qualif/",
		"sans barre initiale":    "claude-qualif",
		"racine":                 "/",
		"espace":                 "/claude qualif",
		"underscore":             "/claude_qualif",
		"non ASCII":              "/clâude",
		"paramètres":             "/claude-qualif?x=1",
		"fragment":               "/claude-qualif#x",
		"point-virgule":          "/claude;qualif",
		"pourcentage":            "/claude%2fqualif",
		"joker":                  "/claude-*",
		"segment vide final":     "/claude-qualif//",
		"antislash":              "/claude\\qualif",
		"schéma":                 "https://coupel.net/claude-qualif",
	} {
		t.Run(name, func(t *testing.T) {
			v := t254Prefix()
			sub(v, "externalIngress")["pathPrefix"] = p
			t172Refused(t, v, "externalIngress.pathPrefix")
		})
	}
	// hôte et chemin confondus : refusé comme avant, nommant externalIngress.host (le chemin va dans pathPrefix)
	t.Run("hôte + chemin confondus", func(t *testing.T) {
		v := t254Prefix()
		delete(sub(v, "externalIngress"), "pathPrefix")
		sub(v, "externalIngress")["host"] = t254Shared + t254Base
		t172Refused(t, v, "externalIngress.host")
	})
	t.Run("hôte avec chemin ET préfixe", func(t *testing.T) {
		v := t254Prefix()
		sub(v, "externalIngress")["host"] = t254Shared + "/autre"
		t172Refused(t, v, "externalIngress.host")
	})
	for _, ok := range []string{"/claude-qualif", "/a", "/a-b/c-d", "/v1/claude-qualif", "/0/1/2", "/" + strings.Repeat("a", 60)} {
		v := t254Prefix()
		sub(v, "externalIngress")["pathPrefix"] = ok
		if _, out, err := render(t, v); err != nil {
			t.Errorf("préfixe valide %q refusé : %.300s", ok, out)
		}
	}
}

// Préfixe vide (ou absent) = comportement précédent : Ingress sur « / », cookie __Host-, HSTS posé.
func TestChartPrefix254EmptyKeepsPreviousBehaviour(t *testing.T) {
	needHelm(t)
	ref := must(t, t254Values())
	for name, mod := range map[string]func(v m){
		"absent": func(v m) {},
		"vide":   func(v m) { sub(v, "externalIngress")["pathPrefix"] = "" },
	} {
		v := t254Values()
		mod(v)
		o := must(t, v)
		a, _ := yaml.Marshal(o["Ingress"])
		b, _ := yaml.Marshal(ref["Ingress"])
		if string(a) != string(b) {
			t.Errorf("%s : les Ingress diffèrent du rendu sans préfixe", name)
		}
		args := t172Args(t172Container(t, o, "auth-proxy-ext"))
		if !strings.HasPrefix(strings.TrimPrefix(t172ArgPrefix(args, "--cookie-name="), "--cookie-name="), "__Host-") {
			t.Errorf("%s : cookie externe %q, attendu __Host- sans préfixe", name, t172ArgPrefix(args, "--cookie-name="))
		}
		if t172ArgPrefix(args, "--proxy-prefix=") != "" && t172ArgPrefix(args, "--proxy-prefix=") != "--proxy-prefix=/oauth2" {
			t.Errorf("%s : --proxy-prefix = %q", name, t172ArgPrefix(args, "--proxy-prefix="))
		}
		if e := t172Env(t172Container(t, o, "launcher")); e["CL_WEB_URL"] != "https://"+t254Ext {
			t.Errorf("%s : CL_WEB_URL = %q", name, e["CL_WEB_URL"])
		}
	}
}

// ── rendu avec préfixe ────────────────────────────────────────────────────────────────────────────────────────────

func t254ExtIngresses(o map[string][]m) []m {
	var out []m
	for _, ing := range o["Ingress"] {
		if strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), t254Shared) {
			out = append(out, ing)
		}
	}
	return out
}

// Aucune capture de « / » ni d'une autre route de l'hôte partagé : tous les chemins de l'externe commencent par le préfixe, en
// `Prefix` ; jamais de règle sans chemin, jamais « / », jamais de joker ; aucun StripPrefix (le préfixe n'est pas retiré).
func TestChartPrefix254IngressNeverCapturesOtherRoutes(t *testing.T) {
	needHelm(t)
	o := must(t, t254Prefix())
	ext := t254ExtIngresses(o)
	if len(ext) < 2 {
		t.Fatalf("%d Ingress pour %s, attendu au moins 2 (préfixe et son /oauth2)", len(ext), t254Shared)
	}
	sawOAuth, sawMain := false, false
	var rules [][2]string // (chemin, pathType) de tous les Ingress externes
	for _, ing := range ext {
		name := fmt.Sprint(get(ing, "metadata", "name"))
		for _, r := range get(ing, "spec", "rules").([]any) {
			if h := fmt.Sprint(get(r, "host")); h != t254Shared {
				t.Errorf("Ingress %s : hôte %q", name, h)
			}
			ps, _ := get(r, "http", "paths").([]any)
			if len(ps) == 0 {
				t.Errorf("Ingress %s : règle sans chemin (capturerait tout l'hôte)", name)
			}
			for _, p := range ps {
				path := fmt.Sprint(get(p, "path"))
				if path == "" || path == "<nil>" || path == "/" || !(path == t254Base || strings.HasPrefix(path, t254Base+"/")) {
					t.Errorf("Ingress %s : chemin %q hors du préfixe %s (capture d'une autre route de %s)", name, path, t254Base, t254Shared)
				}
				if strings.ContainsAny(path, "*()") {
					t.Errorf("Ingress %s : joker dans %q", name, path)
				}
				// le préfixe seul en Exact (jamais en Prefix : « /claude-qualif » Prefix capterait /claude-qualifx) ; le reste en Prefix
				pt := fmt.Sprint(get(p, "pathType"))
				if path == t254Base && pt != "Exact" {
					t.Errorf("Ingress %s : %s en %s, attendu Exact (un Prefix sans « / » final capturerait %sx)", name, path, pt, t254Base)
				}
				if path != t254Base && pt != "Prefix" {
					t.Errorf("Ingress %s : %s en %s, attendu Prefix", name, path, pt)
				}
				if pt == "Prefix" && !strings.HasSuffix(path, "/") && path != t254Base+"/oauth2" {
					t.Errorf("Ingress %s : Prefix %s sans « / » final", name, path)
				}
				rules = append(rules, [2]string{path, pt})
				if path == t254Base+"/oauth2" {
					sawOAuth = true
				}
				if path == t254Base {
					sawMain = true
				}
				if !strings.Contains(fmt.Sprint(get(p, "backend")), "http-ext") {
					t.Errorf("Ingress %s : chemin %s ne vise pas http-ext", name, path)
				}
			}
		}
		if get(ing, "spec", "defaultBackend") != nil {
			t.Errorf("Ingress %s : defaultBackend", name)
		}
		// la plus longue route (/oauth2) l'emporte sur le préfixe (Traefik : priorité = longueur de la règle)
		if a := fmt.Sprint(get(ing, "metadata", "annotations", "traefik.ingress.kubernetes.io/router.priority")); a != "<nil>" && a != "" {
			t.Errorf("Ingress %s : router.priority forcée (%s) — pourrait capturer les routes des autres applications", name, a)
		}
	}
	if !sawMain || !sawOAuth {
		t.Errorf("routes %s (%v) et %s/oauth2 (%v) attendues", t254Base, sawMain, t254Base, sawOAuth)
	}
	// correspondance telle que Traefik la fait (Exact = égalité ; Prefix = préfixe de CHAÎNE) : seules les routes du launcher
	matches := func(req string) bool {
		for _, r := range rules {
			if r[1] == "Exact" && req == r[0] || r[1] == "Prefix" && strings.HasPrefix(req, r[0]) {
				return true
			}
		}
		return false
	}
	for _, req := range []string{"/", "/emby", "/emby/", "/claude-qualifx", "/claude-qualification", "/claude-qualif-other/", "/claude-qualifier/x",
		"/claude-qualifx/", "/claude", "/claude-qualif%2f", "/other/claude-qualif/", "/Claude-Qualif/"} {
		if matches(req) {
			t.Errorf("%s capturé par l'Ingress externe (autre route de %s)", req, t254Shared)
		}
	}
	for _, req := range []string{"/claude-qualif", "/claude-qualif/", "/claude-qualif/api/call", "/claude-qualif/oauth2/callback", "/claude-qualif/files/p/x.html"} {
		if !matches(req) {
			t.Errorf("%s non routé vers le launcher", req)
		}
	}
	for _, mw := range o["Middleware"] {
		if get(mw, "spec", "stripPrefix") != nil || get(mw, "spec", "stripPrefixRegex") != nil || get(mw, "spec", "replacePathRegex") != nil || get(mw, "spec", "replacePath") != nil {
			t.Errorf("Middleware %v : le préfixe ne doit jamais être retiré ni réécrit (pas de StripPrefix)", get(mw, "metadata", "name"))
		}
	}
	// l'ingress interne garde sa route « / » sur son propre hôte (inchangé)
	for _, ing := range o["Ingress"] {
		if strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), t254Int) && strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), t254Base) {
			t.Errorf("l'Ingress interne %v porte le préfixe", get(ing, "metadata", "name"))
		}
	}
}

// oauth2-proxy externe : --proxy-prefix, --cookie-path, --redirect-url avec préfixe ; cookie __Secure-cl_auth_ext (jamais __Host-,
// qui impose Path=/) ; SameSite=Lax ; exemptions de /signed-out et /too-many sous le préfixe ; instance interne inchangée.
func TestChartPrefix254ProxyArguments(t *testing.T) {
	needHelm(t)
	o := must(t, t254Prefix())
	ex := t172Args(t172Container(t, o, "auth-proxy-ext"))
	for _, want := range []string{
		"--proxy-prefix=" + t254Base + "/oauth2", "--cookie-path=" + t254Base, "--redirect-url=https://" + t254Shared + t254Base + "/oauth2/callback",
		"--cookie-name=__Secure-cl_auth_ext_claude-qualif", "--cookie-secure=true", "--request-logging=false", "--cookie-samesite=lax", "--skip-provider-button=true",
	} {
		if !t172HasArg(ex, want) {
			t.Errorf("auth-proxy-ext : argument %q absent de %v", want, ex)
		}
	}
	for _, bad := range []string{"--htpasswd-file", "--display-htpasswd-form", "--cookie-domain"} {
		if t172ArgPrefix(ex, bad) != "" {
			t.Errorf("auth-proxy-ext : %s présent", bad)
		}
	}
	// exemptions : /signed-out et /too-many sous le préfixe — en une règle (a|b) ou deux, jamais hors du préfixe
	var skips []string
	for _, a := range ex {
		if strings.HasPrefix(a, "--skip-auth-route=") {
			skips = append(skips, a)
		}
	}
	all := strings.Join(skips, " ")
	for _, page := range []string{"signed-out", "too-many"} {
		if !strings.Contains(all, "^"+t254Base+"/") || !strings.Contains(all, page) {
			t.Errorf("exemption de %s sous %s absente : %v", page, t254Base, skips)
		}
	}
	if n := t172ArgPrefix(ex, "--cookie-name="); strings.Contains(n, "__Host-") || n == "--cookie-name=__Secure-cl_auth_ext" {
		t.Errorf("cookie %q : __Host- impose Path=/ (incompatible avec le préfixe)", n)
	}
	// les routes publiques du proxy ne sont pas exemptées hors du préfixe
	for _, a := range ex {
		if strings.HasPrefix(a, "--skip-auth-route=") && !strings.Contains(a, t254Base) {
			t.Errorf("exemption hors du préfixe : %s", a)
		}
	}
	// le nom du cookie dépend du préfixe (un cookie d'un autre préfixe de l'hôte partagé n'est jamais lu) : segments joints par « - »
	for prefix, want := range map[string]string{"/claude-qualif": "__Secure-cl_auth_ext_claude-qualif", "/a": "__Secure-cl_auth_ext_a", "/v1/qualif": "__Secure-cl_auth_ext_v1-qualif", "/x/y/z": "__Secure-cl_auth_ext_x-y-z"} {
		v := t254Prefix()
		sub(v, "externalIngress")["pathPrefix"] = prefix
		got := strings.TrimPrefix(t172ArgPrefix(t172Args(t172Container(t, must(t, v), "auth-proxy-ext")), "--cookie-name="), "--cookie-name=")
		if got != want {
			t.Errorf("préfixe %s : cookie %q, attendu %q", prefix, got, want)
		}
	}
	// sans préfixe : __Host-_cl_auth_ext ; l'interne garde _cl_auth ; les deux instances ne journalisent pas les requêtes
	if got := strings.TrimPrefix(t172ArgPrefix(t172Args(t172Container(t, must(t, t254Values()), "auth-proxy-ext")), "--cookie-name="), "--cookie-name="); got != "__Host-_cl_auth_ext" {
		t.Errorf("sans préfixe : cookie %q, attendu __Host-_cl_auth_ext", got)
	}
	if !t172HasArg(t172Args(t172Container(t, o, "auth-proxy")), "--request-logging=false") {
		t.Error("auth-proxy interne sans --request-logging=false")
	}
	// instance interne : inchangée (chemin racine, cookie _cl_auth)
	in := t172Args(t172Container(t, o, "auth-proxy"))
	if !t172HasArg(in, "--cookie-name=_cl_auth") || strings.Contains(strings.Join(in, " "), t254Base) {
		t.Errorf("auth-proxy interne modifié par le préfixe : %v", in)
	}
	// launcher : adresse principale avec chemin, alternative interne sans chemin, compte local jamais sur l'externe
	e := t172Env(t172Container(t, o, "launcher"))
	if e["CL_WEB_URL"] != "https://"+t254Shared+t254Base || e["CL_WEB_ALT_URLS"] != "https://"+t254Int {
		t.Errorf("CL_WEB_URL=%q CL_WEB_ALT_URLS=%q", e["CL_WEB_URL"], e["CL_WEB_ALT_URLS"])
	}
	if strings.Contains(e["CL_AUTH_LOCAL_ADMIN_HOSTS"], t254Shared) {
		t.Errorf("l'hôte partagé admet le compte local : %s", e["CL_AUTH_LOCAL_ADMIN_HOSTS"])
	}
}

// Page 429 : le middleware errors renvoie vers <préfixe>/too-many (route publique du launcher sous sa base).
func TestChartPrefix254TooManyPath(t *testing.T) {
	needHelm(t)
	o := must(t, t254Prefix())
	found := false
	for _, mw := range o["Middleware"] {
		if q := get(mw, "spec", "errors", "query"); q != nil {
			if q == t254Base+"/too-many" {
				found = true
			}
		}
	}
	if !found {
		var qs []string
		for _, mw := range o["Middleware"] {
			if q := get(mw, "spec", "errors", "query"); q != nil {
				qs = append(qs, fmt.Sprint(q))
			}
		}
		t.Errorf("middleware errors : queries %v, attendu %s/too-many pour l'ingress externe", qs, t254Base)
	}
}

// P13 : HSTS non posé sur l'accès par sous-chemin (il s'appliquerait à tout l'hôte partagé, dont des applications en http) ; le reste
// des en-têtes de sécurité reste ; l'ingress interne et l'externe sans préfixe gardent HSTS.
func TestChartPrefix254NoHSTSOnSharedHost(t *testing.T) {
	needHelm(t)
	hsts := func(o map[string][]m, ing m) bool {
		byName := map[string]m{}
		for _, mw := range o["Middleware"] {
			byName[fmt.Sprint(get(mw, "metadata", "name"))] = mw
		}
		on := false
		for _, ref := range strings.Split(t172Anno(ing), ",") {
			ref = strings.TrimSuffix(strings.TrimSpace(ref), "@kubernetescrd")
			for n, mw := range byName {
				if strings.HasSuffix(ref, "-"+n) {
					h, _ := get(mw, "spec", "headers").(m)
					if s := fmt.Sprint(h["stsSeconds"]); h["stsSeconds"] != nil && s != "0" {
						on = true
					}
					for _, k := range []string{"stsIncludeSubdomains", "stsPreload"} {
						if h[k] == true {
							on = true
						}
					}
					if b, _ := yaml.Marshal(h); strings.Contains(strings.ToLower(string(b)), "strict-transport-security") {
						on = true
					}
				}
			}
		}
		return on
	}
	o := must(t, t254Prefix())
	for _, ing := range t254ExtIngresses(o) {
		if hsts(o, ing) {
			t.Errorf("Ingress %v : HSTS posé sur l'hôte partagé %s (P13)", get(ing, "metadata", "name"), t254Shared)
		}
		a := t172Anno(ing)
		for _, w := range []string{"-headers", "-https"} {
			if !strings.Contains(a, w) {
				t.Errorf("Ingress %v sans %s : %s", get(ing, "metadata", "name"), w, a)
			}
		}
	}
	// les autres en-têtes de sécurité restent sur le middleware de l'externe
	for _, ing := range t254ExtIngresses(o) {
		for _, ref := range strings.Split(t172Anno(ing), ",") {
			for _, mw := range o["Middleware"] {
				n := fmt.Sprint(get(mw, "metadata", "name"))
				if strings.HasSuffix(strings.TrimSuffix(strings.TrimSpace(ref), "@kubernetescrd"), "-"+n) && strings.HasSuffix(n, "-headers") {
					b, _ := yaml.Marshal(get(mw, "spec", "headers"))
					for _, w := range []string{"nosniff", "no-referrer"} {
						if !strings.Contains(strings.ToLower(string(b)), w) {
							t.Errorf("middleware %s sans %s : %s", n, w, b)
						}
					}
					if strings.Contains(strings.ToLower(string(b)), "content-security-policy") || strings.Contains(strings.ToLower(string(b)), "contentsecuritypolicy") {
						t.Errorf("middleware %s : CSP posé (écraserait le sandbox de /files)", n)
					}
				}
			}
		}
	}
	// sans préfixe : HSTS conservé sur l'ingress interne et sur l'externe en sous-domaine
	o2 := must(t, t254Values())
	for _, ing := range o2["Ingress"] {
		if !hsts(o2, ing) {
			t.Errorf("sans préfixe : Ingress %v sans HSTS", get(ing, "metadata", "name"))
		}
	}
}

// Certificat : avec pathPrefix l'hôte partagé est servi par le Secret fourni (`tls.secretName`) — repris tel quel dans le bloc tls des
// Ingress externes ; aucune demande de certificat (annotation cert-manager.io/*, Certificate) pour cet hôte.
func TestChartPrefix254Certificate(t *testing.T) {
	needHelm(t)
	o := must(t, t254Prefix())
	ext := t254ExtIngresses(o)
	if len(ext) < 2 {
		t.Fatalf("%d Ingress externes", len(ext))
	}
	for _, ing := range ext {
		name := fmt.Sprint(get(ing, "metadata", "name"))
		tls, _ := get(ing, "spec", "tls").([]any)
		if len(tls) == 0 {
			t.Errorf("Ingress %s sans bloc tls", name)
		}
		for _, tl := range tls {
			if fmt.Sprint(get(tl, "hosts")) != "["+t254Shared+"]" || get(tl, "secretName") != "coupel-net-tls" {
				t.Errorf("Ingress %s : tls = %v, attendu hosts [%s] et secretName coupel-net-tls", name, tl, t254Shared)
			}
		}
		for _, k := range []string{"cert-manager.io/cluster-issuer", "cert-manager.io/issuer", "cert-manager.io/common-name"} {
			if get(ing, "metadata", "annotations", k) != nil {
				t.Errorf("Ingress %s : annotation %s avec un sous-chemin", name, k)
			}
		}
	}
	for _, c := range o["Certificate"] {
		if b, _ := yaml.Marshal(c); strings.Contains(string(b), t254Shared) {
			t.Errorf("Certificate pour %s avec un sous-chemin : %s", t254Shared, b)
		}
	}
}

// Réserve QA R1 : pathPrefix + tls.clusterIssuer est REFUSÉ au rendu (le certificat de l'hôte partagé n'est pas demandé par ce chart :
// un Certificate/une annotation cert-manager viendrait en doublon de celui des autres applications) avec un message qui cite pathPrefix
// ET clusterIssuer ; pathPrefix + tls.secretName seul est accepté ; sans préfixe, clusterIssuer reste accepté comme avant.
func TestChartPrefix254ClusterIssuerRefusedWithPrefix(t *testing.T) {
	needHelm(t)
	must(t, t254Prefix())
	for name, tls := range map[string]m{
		"clusterIssuer seul":          {"clusterIssuer": "letsencrypt-prod"},
		"clusterIssuer et secretName": {"clusterIssuer": "letsencrypt-prod", "secretName": "coupel-net-tls"},
	} {
		t.Run(name, func(t *testing.T) {
			v := t254Prefix()
			sub(v, "externalIngress")["tls"] = tls
			if name == "clusterIssuer seul" {
				t172Refused(t, v, "pathPrefix", "clusterIssuer")
			} else {
				t172Refused(t, v, "clusterIssuer") // refusé aussi (exclusivité secretName / clusterIssuer)
			}
		})
	}
	// secretName seul : accepté (déjà couvert par t254Prefix) ; sans préfixe : clusterIssuer accepté comme avant, sans secretName
	if _, out, err := render(t, t254Values()); err != nil {
		t.Errorf("sans préfixe, clusterIssuer refusé : %.300s", out)
	}
	for _, p := range []string{"", "/claude-qualif"} {
		v := t254Values()
		sub(v, "externalIngress")["pathPrefix"] = p
		sub(v, "externalIngress")["tls"] = m{"secretName": "ext-tls"}
		if _, out, err := render(t, v); err != nil {
			t.Errorf("secretName seul refusé (préfixe %q) : %.300s", p, out)
		}
	}
}

// Désactivé : pathPrefix posé sans externalIngress.enabled ne rend rien d'externe et ne casse pas le rendu interne.
func TestChartPrefix254IgnoredWhenDisabled(t *testing.T) {
	needHelm(t)
	v := t172Local()
	v["externalIngress"] = m{"enabled": false, "pathPrefix": t254Base, "host": t254Shared}
	o, out, err := render(t, v)
	if err != nil {
		t.Fatalf("rendu : %.300s", out)
	}
	if t172Container(t, o, "auth-proxy-ext") != nil || strings.Contains(out, t254Base) {
		t.Error("objets externes ou préfixe rendus alors que l'ingress externe est désactivé")
	}
}

// externalIngress.sessionDuration : défaut 4 h (l'externe est plus court que l'interne, 8 h), 15 m à 168 h, strictement supérieure
// à auth.sessionRefresh ; hors plage ou illisible → refus nommant la valeur.
func TestChartPrefix254ExternalSessionDuration(t *testing.T) {
	needHelm(t)
	dur := func(v m, container string) (string, string) {
		o := must(t, v)
		a := t172Args(t172Container(t, o, container))
		return strings.TrimPrefix(t172ArgPrefix(a, "--cookie-expire="), "--cookie-expire="), strings.TrimPrefix(t172ArgPrefix(a, "--cookie-refresh="), "--cookie-refresh=")
	}
	for name, v := range map[string]m{"avec préfixe": t254Prefix(), "sans préfixe": t254Values()} {
		if e, _ := dur(v, "auth-proxy-ext"); e != "4h" {
			t.Errorf("%s : session externe %q, attendu 4h par défaut", name, e)
		}
		if e, _ := dur(v, "auth-proxy"); e != "8h" {
			t.Errorf("%s : session interne %q, attendu 8h (inchangée)", name, e)
		}
	}
	v := t254Prefix()
	sub(v, "externalIngress")["sessionDuration"] = "2h"
	if e, r := dur(v, "auth-proxy-ext"); e != "2h" || r != "1h" {
		t.Errorf("sessionDuration 2h : expire %q refresh %q", e, r)
	}
	if e, _ := dur(v, "auth-proxy"); e != "8h" {
		t.Errorf("l'interne suit l'externe : %q", e)
	}
	for _, ok := range []string{"15m", "90m", "168h"} {
		v := t254Prefix()
		sub(v, "auth")["sessionRefresh"] = "10m"
		sub(v, "externalIngress")["sessionDuration"] = ok
		if _, out, err := render(t, v); err != nil {
			t.Errorf("sessionDuration %s refusée : %.200s", ok, out)
		}
	}
	for name, d := range map[string]string{"trop court": "10m", "juste sous le minimum": "14m", "trop long": "169h", "illisible": "quatre heures", "zéro": "0", "négatif": "-4h", "vide non nul": " "} {
		t.Run(name, func(t *testing.T) {
			v := t254Prefix()
			sub(v, "externalIngress")["sessionDuration"] = d
			t172Refused(t, v, "sessionDuration")
		})
	}
	// strictement supérieure à auth.sessionRefresh
	for name, tc := range map[string][2]string{"egale_au_refresh": {"1h", "1h"}, "inferieure_au_refresh": {"30m", "1h"}, "refresh_2h_duree_90m": {"90m", "2h"}} {
		t.Run(name, func(t *testing.T) {
			v := t254Prefix()
			sub(v, "externalIngress")["sessionDuration"] = tc[0]
			sub(v, "auth")["sessionRefresh"] = tc[1]
			t172Refused(t, v, "sessionDuration", "sessionRefresh")
		})
	}
}

// error.html : « Se déconnecter » vise <base>/signed-out (rd= substitué par Helm), jamais /sign_in ni /../ ; interne : /signed-out ;
// externe sans préfixe : /signed-out ; externe avec préfixe : <base>/signed-out. Échoue si la substitution ne remplace plus rien.
func TestChartErrorHTML254RdPinned(t *testing.T) {
	needHelm(t)
	multi := t254Prefix()
	sub(multi, "externalIngress")["pathPrefix"] = "/v1/qualif"
	for name, tc := range map[string]struct {
		v        m
		internal string
		external string
	}{
		"sans préfixe":        {t254Values(), "%2Fsigned-out", "%2Fsigned-out"},
		"avec préfixe":        {t254Prefix(), "%2Fsigned-out", "%2Fclaude-qualif%2Fsigned-out"},
		"préfixe à 2 niveaux": {multi, "%2Fsigned-out", "%2Fv1%2Fqualif%2Fsigned-out"},
	} {
		o := must(t, tc.v)
		for cmName, want := range map[string]string{t172Release + "-auth-templates": tc.internal, t172Release + "-auth-templates-ext": tc.external} {
			cm := t172Named(o, "ConfigMap", cmName)
			if cm == nil {
				t.Errorf("%s : ConfigMap %s absente", name, cmName)
				continue
			}
			er, _ := get(cm, "data", "error.html").(string)
			if er == "" {
				t.Errorf("%s / %s : error.html absent", name, cmName)
				continue
			}
			if n := strings.Count(er, "/sign_out?rd="); n != 1 {
				t.Errorf("%s / %s : %d liens sign_out?rd=, attendu 1", name, cmName, n)
			}
			if !strings.Contains(er, `/sign_out?rd=`+want+`"`) {
				t.Errorf("%s / %s : rd ne vaut pas %s (cible <base>/signed-out) : %s", name, cmName, want, regexpFind254(er))
			}
			// le lien de déconnexion (et lui seul : « Réessayer » a sa propre cible) ne vise jamais /sign_in ni /../
			href := ""
			if mm := regexp.MustCompile(`href="([^"]*sign_out\?rd=[^"]*)"`).FindStringSubmatch(er); mm != nil {
				href = mm[1]
			}
			if href == "" {
				t.Errorf("%s / %s : lien de déconnexion introuvable", name, cmName)
			}
			for _, bad := range []string{"sign_in", "/../", "{{.ProxyPrefix}}%2F", "rd={{", "%2F%2F"} {
				if strings.Contains(strings.TrimPrefix(href, "{{.ProxyPrefix}}"), bad) {
					t.Errorf("%s / %s : lien de déconnexion %q contient %q (rd mal substitué)", name, cmName, href, bad)
				}
			}
			// la déconnexion de l'interne ne pointe jamais vers la base de l'externe, et réciproquement
			if cmName == t172Release+"-auth-templates" && strings.Contains(er, "claude-qualif") {
				t.Errorf("%s : l'error.html interne cite la base de l'externe", name)
			}
		}
	}
}

func regexpFind254(s string) string {
	i := strings.Index(s, "sign_out?rd=")
	if i < 0 {
		return "(pas de sign_out)"
	}
	j := i + 60
	if j > len(s) {
		j = len(s)
	}
	return s[i:j]
}
