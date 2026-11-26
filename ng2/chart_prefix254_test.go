package main

import (
	"fmt"
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
				if pt := fmt.Sprint(get(p, "pathType")); pt != "Prefix" {
					t.Errorf("Ingress %s : pathType %q, attendu Prefix", name, pt)
				}
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
		"--cookie-name=__Secure-cl_auth_ext", "--cookie-secure=true", "--cookie-samesite=lax", "--skip-provider-button=true",
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
	if n := t172ArgPrefix(ex, "--cookie-name="); strings.Contains(n, "__Host-") {
		t.Errorf("cookie %q : __Host- impose Path=/ (incompatible avec le préfixe)", n)
	}
	// les routes publiques du proxy ne sont pas exemptées hors du préfixe
	for _, a := range ex {
		if strings.HasPrefix(a, "--skip-auth-route=") && !strings.Contains(a, t254Base) {
			t.Errorf("exemption hors du préfixe : %s", a)
		}
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

// Certificat : l'hôte partagé est servi par un certificat propre au namespace (annotation cert-manager de l'Ingress ou Certificate) ;
// avec tls.secretName : le Secret fourni, aucun Certificate.
func TestChartPrefix254Certificate(t *testing.T) {
	needHelm(t)
	o := must(t, t254Prefix())
	ext := t254ExtIngresses(o)
	hasTLS := false
	for _, ing := range ext {
		if b, _ := yaml.Marshal(get(ing, "spec", "tls")); strings.Contains(string(b), t254Shared) {
			hasTLS = true
		}
	}
	if !hasTLS {
		t.Error("aucun bloc tls pour l'hôte partagé")
	}
	v := t254Prefix()
	sub(v, "externalIngress")["tls"] = m{"secretName": "coupel-net-tls"}
	o2 := must(t, v)
	if b, _ := yaml.Marshal(o2["Certificate"]); strings.Contains(string(b), t254Shared) {
		t.Errorf("Certificate créé pour %s avec tls.secretName : %s", t254Shared, b)
	}
	found := false
	for _, ing := range t254ExtIngresses(o2) {
		if b, _ := yaml.Marshal(get(ing, "spec", "tls")); strings.Contains(string(b), "coupel-net-tls") {
			found = true
		}
	}
	if !found {
		t.Error("secretName coupel-net-tls absent des Ingress externes")
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
