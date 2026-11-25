package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// #172 / #251 / #252 (v7.0.0) — chart Helm : authentification (sidecar oauth2-proxy), compte local `admin@local`, port de
// santé, ConfigMap des gabarits, variables réservées, contrôles de rendu. Écrit depuis contracts/helm-values.md
// « v7.0.0 » et contracts/auth.md §6–§8 (TDD) : RED attendu tant que le chart n'a pas ses valeurs `auth.*`.
// `helm lint` et `helm template` seulement (aucune installation) ; ignoré si helm est absent. Réutilise les aides de
// chart_helm_test.go. Hypothèses (formes de rendu non écrites au contrat) : _work/handoff/test-writer-lot2.md.
//
// NB : avec auth.enabled à true par défaut (P1), les tests v6.9.0 qui rendent baseValues() sans bloc `auth` doivent
// désormais poser auth.enabled: false (changement BREAKING du CHANGELOG) — mise à jour à la charge de l'infra.

const (
	t172Admin   = "zoe-admin" // pas « ccoupel » : présent dans le nom de l'image (ghcr.io/ccoupel/…)
	t172Release = "rel"
)

// t172Values : valeurs valides avec authentification GitHub et compte local désactivé.
func t172Values() m {
	v := baseValues()
	v["auth"] = m{
		"enabled":        true,
		"existingSecret": "gh-oauth",
		"admins":         []any{t172Admin},
		"localAdmin":     m{"enabled": false},
	}
	return v
}

// t172Local : valeurs avec compte local (secret généré) et GitHub.
func t172Local() m {
	v := t172Values()
	sub(v, "auth")["localAdmin"] = m{"enabled": true}
	return v
}

func t172Container(t *testing.T, o map[string][]m, name string) m {
	t.Helper()
	cs, _ := pod(t, o)["containers"].([]any)
	for _, c := range cs {
		if get(c, "name") == name {
			return c.(m)
		}
	}
	return nil
}

func t172Env(c m) map[string]string {
	out := map[string]string{}
	es, _ := c["env"].([]any)
	for _, e := range es {
		em := e.(m)
		if v, ok := em["value"].(string); ok {
			out[em["name"].(string)] = v
		} else {
			out[em["name"].(string)] = "<valueFrom>"
		}
	}
	return out
}

func t172Args(c m) []string {
	var out []string
	as, _ := c["args"].([]any)
	for _, a := range as {
		out = append(out, fmt.Sprint(a))
	}
	return out
}

func t172HasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func t172ArgPrefix(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return a
		}
	}
	return ""
}

func t172Refused(t *testing.T, v m, wants ...string) {
	t.Helper()
	_, out, err := render(t, v)
	if err == nil {
		t.Errorf("rendu accepté, refus attendu (%v)", wants)
		return
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("le message ne contient pas « %s » : %.300s", w, out)
		}
	}
}

// t172Notes : NOTES.txt rendu (helm template ne l'affiche pas : helm install --dry-run=client, sans cluster).
func t172Notes(t *testing.T, v m) string {
	t.Helper()
	out, errOut, err := helm(t, "install", "rel", chartDir, "-f", writeVals(t, v), "--dry-run=client")
	if err != nil {
		t.Fatalf("NOTES.txt : %v\n%s%s", err, out, errOut)
	}
	if i := strings.Index(out, "NOTES:"); i >= 0 {
		return out[i:]
	}
	t.Fatalf("NOTES.txt absent de la sortie de helm install --dry-run")
	return ""
}

func t172Named(o map[string][]m, kind, name string) m {
	for _, x := range o[kind] {
		if get(x, "metadata", "name") == name {
			return x
		}
	}
	return nil
}

// ── pod : sidecar, launcher en boucle locale, santé ─────────────────────────────────────────────────────────────

func TestChartAuth172PodWithProxy(t *testing.T) {
	needHelm(t)
	o := must(t, t172Values())
	px, l := t172Container(t, o, "auth-proxy"), t172Container(t, o, "launcher")
	if px == nil || l == nil {
		t.Fatalf("conteneurs auth-proxy (%v) et launcher (%v) attendus", px != nil, l != nil)
	}
	if t172Container(t, o, "auth-proxy-ext") != nil {
		t.Error("auth-proxy-ext rendu sans ingress externe")
	}
	if n := len(pod(t, o)["containers"].([]any)); n != 2 {
		t.Errorf("%d conteneurs, attendu 2 (launcher + auth-proxy)", n)
	}
	e := t172Env(l)
	for k, want := range map[string]string{
		"CL_WEB_ACCESS": "proxy", "CL_WEB_ADDR": "127.0.0.1:8081", "CL_HEALTH_ADDR": "0.0.0.0:8082",
		"CL_WEB_URL": "https://launcher-test.coupel.lan", "CL_AUTH_ADMINS": t172Admin,
	} {
		if e[k] != want {
			t.Errorf("launcher : %s = %q, attendu %q", k, e[k], want)
		}
	}
	if v, ok := e["CL_AUTH_LOCAL_ADMIN_HOSTS"]; ok && v != "" {
		t.Errorf("compte local désactivé : CL_AUTH_LOCAL_ADMIN_HOSTS = %q, attendu vide ou absent", v)
	}
	// le launcher n'expose que la santé ; le Service n'a que le port http, servi par le proxy
	var ports []string
	for _, p := range get(l, "ports").([]any) {
		ports = append(ports, fmt.Sprint(get(p, "name"), "/", get(p, "containerPort")))
	}
	if strings.Join(ports, ",") != "health/8082" {
		t.Errorf("ports du launcher = %v, attendu seulement health/8082", ports)
	}
	if ps := get(o["Service"][0], "spec", "ports").([]any); len(ps) != 1 || get(ps[0], "name") != "http" {
		t.Errorf("ports du Service = %v, attendu un seul port « http »", ps)
	}
	var pxPorts []string
	for _, p := range get(px, "ports").([]any) {
		pxPorts = append(pxPorts, fmt.Sprint(get(p, "name"), "/", get(p, "containerPort")))
	}
	if strings.Join(pxPorts, ",") != "http/8080" {
		t.Errorf("ports de auth-proxy = %v, attendu http/8080", pxPorts)
	}
	// sondes du launcher sur health (startup, liveness, readiness), du proxy sur /ping
	for _, probe := range []string{"startupProbe", "livenessProbe", "readinessProbe"} {
		if get(l, probe, "httpGet", "port") != "health" {
			t.Errorf("launcher : %s sur %v, attendu le port health", probe, get(l, probe, "httpGet", "port"))
		}
	}
	if get(l, "readinessProbe", "httpGet", "path") != "/readyz" {
		t.Errorf("launcher readiness : %v", get(l, "readinessProbe", "httpGet", "path"))
	}
	for _, probe := range []string{"livenessProbe", "readinessProbe"} {
		if get(px, probe, "httpGet", "path") != "/ping" {
			t.Errorf("auth-proxy : %s %v, attendu /ping", probe, get(px, probe, "httpGet", "path"))
		}
	}
}

// Arguments d'oauth2-proxy : liste du contrat ; identifiants par secretKeyRef, jamais en clair.
func TestChartAuth172ProxyArguments(t *testing.T) {
	needHelm(t)
	o := must(t, t172Values())
	px := t172Container(t, o, "auth-proxy")
	args := t172Args(px)
	for _, want := range []string{
		"--provider=github", "--email-domain=*", "--upstream=http://127.0.0.1:8081/", "--http-address=0.0.0.0:8080",
		"--reverse-proxy=true", "--pass-user-headers=true", "--pass-access-token=false", "--skip-provider-button=true",
		"--api-route=^/api/", "--skip-auth-route=GET=^/signed-out$", "--cookie-name=_cl_auth", "--cookie-secure=true",
		"--cookie-samesite=lax", "--cookie-expire=8h", "--cookie-refresh=1h", "--custom-templates-dir=/etc/cl-auth/templates",
		"--request-logging=false", "--auth-logging=true", "--ping-path=/ping", "--github-user=" + t172Admin,
	} {
		if !t172HasArg(args, want) {
			t.Errorf("argument %q absent de %v", want, args)
		}
	}
	for _, bad := range []string{"--htpasswd-file", "--display-htpasswd-form", "--client-secret", "--cookie-secret", "--client-id"} {
		if t172ArgPrefix(args, bad) != "" {
			t.Errorf("argument %s présent (identifiants par l'environnement du Secret ; compte local désactivé)", bad)
		}
	}
	// identifiants : secretKeyRef du Secret fourni (client-id, client-secret) et du Secret du cookie généré
	refs := map[string][2]string{}
	for _, e := range get(px, "env").([]any) {
		em := e.(m)
		if ref := get(em, "valueFrom", "secretKeyRef"); ref != nil {
			refs[em["name"].(string)] = [2]string{fmt.Sprint(get(ref, "name")), fmt.Sprint(get(ref, "key"))}
		}
		if v, ok := em["value"].(string); ok && (strings.Contains(em["name"].(string), "SECRET") || strings.Contains(em["name"].(string), "CLIENT")) && v != "" {
			t.Errorf("%s en clair dans l'environnement", em["name"])
		}
	}
	if r := refs["OAUTH2_PROXY_CLIENT_ID"]; r != [2]string{"gh-oauth", "client-id"} {
		t.Errorf("OAUTH2_PROXY_CLIENT_ID = %v, attendu gh-oauth/client-id", r)
	}
	if r := refs["OAUTH2_PROXY_CLIENT_SECRET"]; r != [2]string{"gh-oauth", "client-secret"} {
		t.Errorf("OAUTH2_PROXY_CLIENT_SECRET = %v, attendu gh-oauth/client-secret", r)
	}
	if r := refs["OAUTH2_PROXY_COOKIE_SECRET"]; r != [2]string{t172Release + "-auth-cookie", "cookie-secret"} {
		t.Errorf("OAUTH2_PROXY_COOKIE_SECRET = %v, attendu rel-auth-cookie/cookie-secret", r)
	}
	// image : jamais latest, tag du contrat ; sécurité
	if img, _ := px["image"].(string); !strings.HasPrefix(img, "quay.io/oauth2-proxy/oauth2-proxy:v7.") {
		t.Errorf("image = %q", img)
	}
	if get(px, "securityContext", "runAsNonRoot") != true || get(px, "securityContext", "readOnlyRootFilesystem") != true {
		t.Errorf("securityContext du proxy : %v", px["securityContext"])
	}
	if caps, _ := get(px, "securityContext", "capabilities", "drop").([]any); len(caps) == 0 || fmt.Sprint(caps[0]) != "ALL" {
		t.Errorf("capacités non retirées : %v", get(px, "securityContext", "capabilities"))
	}
	if get(px, "securityContext", "allowPrivilegeEscalation") == true {
		t.Error("allowPrivilegeEscalation à true")
	}
}

func TestChartAuth172ImageTagAndSessionValues(t *testing.T) {
	needHelm(t)
	// liste des utilisateurs : ajoutée à --github-user et à CL_AUTH_USERS
	v := t172Values()
	sub(v, "auth")["users"] = []any{"alice", "bob-1"}
	o := must(t, v)
	if got := t172ArgPrefix(t172Args(t172Container(t, o, "auth-proxy")), "--github-user="); got != "--github-user=zoe-admin,alice,bob-1" {
		t.Errorf("--github-user = %q (admins ∪ users)", got)
	}
	if e := t172Env(t172Container(t, o, "launcher")); e["CL_AUTH_USERS"] != "alice,bob-1" {
		t.Errorf("CL_AUTH_USERS = %q", e["CL_AUTH_USERS"])
	}
	// durée de session
	v = t172Values()
	sub(v, "auth")["sessionDuration"], sub(v, "auth")["sessionRefresh"] = "12h", "30m"
	args := t172Args(t172Container(t, must(t, v), "auth-proxy"))
	if !t172HasArg(args, "--cookie-expire=12h") || !t172HasArg(args, "--cookie-refresh=30m") {
		t.Errorf("durées non appliquées : %v", args)
	}
	// image : tag explicite, digest facultatif ; latest refusé
	v = t172Values()
	sub(v, "auth", "proxy", "image")["tag"] = "v7.9.9"
	if img := t172Container(t, must(t, v), "auth-proxy")["image"]; img != "quay.io/oauth2-proxy/oauth2-proxy:v7.9.9" {
		t.Errorf("image = %v", img)
	}
	for _, bad := range []string{"latest", "LATEST", ""} {
		v = t172Values()
		sub(v, "auth", "proxy", "image")["tag"] = bad
		t.Run("tag="+bad, func(t *testing.T) { t172Refused(t, v, "latest") })
	}
}

// ── contrôles de rendu ───────────────────────────────────────────────────────────────────────────────────────────

func TestChartAuth172RenderRefusals(t *testing.T) {
	needHelm(t)
	must(t, t172Values())
	for name, tc := range map[string]struct {
		mod   func(v m)
		wants []string
	}{
		"Secret OAuth absent":        {func(v m) { delete(sub(v, "auth"), "existingSecret") }, []string{"auth.existingSecret"}},
		"Secret OAuth vide":          {func(v m) { sub(v, "auth")["existingSecret"] = "" }, []string{"auth.existingSecret"}},
		"aucun administrateur":       {func(v m) { sub(v, "auth")["admins"] = []any{} }, []string{"auth.admins"}},
		"admins absent":              {func(v m) { delete(sub(v, "auth"), "admins") }, []string{"auth.admins"}},
		"login invalide":             {func(v m) { sub(v, "auth")["admins"] = []any{"zoe-admin", "pas valide"} }, []string{"pas valide"}},
		"login commençant par -":     {func(v m) { sub(v, "auth")["admins"] = []any{"-ccoupel"} }, []string{"-ccoupel"}},
		"login avec @":               {func(v m) { sub(v, "auth")["admins"] = []any{"admin@local"} }, []string{"admin@local"}},
		"doublon d'administrateurs":  {func(v m) { sub(v, "auth")["admins"] = []any{"zoe-admin", "ZOE-Admin"} }, []string{"zoe-admin"}},
		"user invalide":              {func(v m) { sub(v, "auth")["users"] = []any{"x y"} }, []string{"x y"}},
		"login dans les deux listes": {func(v m) { sub(v, "auth")["users"] = []any{"ZOE-Admin"} }, []string{"zoe-admin"}},
		"web.access posé avec auth":  {func(v m) { sub(v, "web")["access"] = "none" }, []string{"web.access est imposé à proxy par auth.enabled"}},
		"web.access token avec auth": {func(v m) { sub(v, "web")["access"] = "token" }, []string{"web.access"}},
		"aucune adresse https":       {func(v m) { v["ingress"] = m{"enabled": false} }, []string{"https"}},
		"session trop courte":        {func(v m) { sub(v, "auth")["sessionDuration"] = "10m" }, []string{"sessionDuration"}},
		"session trop longue":        {func(v m) { sub(v, "auth")["sessionDuration"] = "169h" }, []string{"sessionDuration"}},
		"session illisible":          {func(v m) { sub(v, "auth")["sessionDuration"] = "huit heures" }, []string{"sessionDuration"}},
		"refresh nul":                {func(v m) { sub(v, "auth")["sessionRefresh"] = "0" }, []string{"sessionRefresh"}},
		"refresh ≥ durée":            {func(v m) { sub(v, "auth")["sessionRefresh"] = "8h" }, []string{"sessionRefresh"}},
		"GitHub et local coupés":     {func(v m) { sub(v, "auth", "github")["enabled"] = false }, []string{"auth.github.enabled"}},
	} {
		t.Run(name, func(t *testing.T) {
			v := t172Values()
			tc.mod(v)
			t172Refused(t, v, tc.wants...)
		})
	}
	// bornes admises
	for _, d := range []string{"15m", "168h"} {
		v := t172Values()
		sub(v, "auth")["sessionDuration"], sub(v, "auth")["sessionRefresh"] = d, "10m"
		if _, out, err := render(t, v); err != nil {
			t.Errorf("sessionDuration %s refusée : %.200s", d, out)
		}
	}
}

// auth.enabled: false : comportement de v6.9.0 (web.access honoré, aucun sidecar, aucun Secret d'authentification).
func TestChartAuth172DisabledKeepsV690(t *testing.T) {
	needHelm(t)
	v := baseValues()
	v["auth"] = m{"enabled": false}
	for _, mode := range []string{"none", "token"} {
		sub(v, "web")["access"] = mode
		o := must(t, v)
		if e := env(t, o); e["CL_WEB_ACCESS"] != mode {
			t.Errorf("auth désactivée, web.access=%s : CL_WEB_ACCESS = %q", mode, e["CL_WEB_ACCESS"])
		}
		if n := len(pod(t, o)["containers"].([]any)); n != 1 {
			t.Errorf("%d conteneurs sans authentification, attendu 1", n)
		}
		for _, name := range []string{t172Release + "-auth-cookie", t172Release + "-local-admin", t172Release + "-auth-templates"} {
			for _, k := range []string{"Secret", "ConfigMap"} {
				if t172Named(o, k, name) != nil {
					t.Errorf("%s %s rendu avec auth.enabled: false", k, name)
				}
			}
		}
		e := env(t, o)
		for _, k := range []string{"CL_AUTH_ADMINS", "CL_AUTH_USERS", "CL_AUTH_LOCAL_ADMIN_HOSTS", "CL_WEB_ADDR", "CL_HEALTH_ADDR"} {
			if _, ok := e[k]; ok {
				t.Errorf("auth désactivée : %s posée", k)
			}
		}
	}
}

// Variables posées par le chart : réservées à extraEnv.
func TestChartAuth172ReservedEnv(t *testing.T) {
	needHelm(t)
	for _, name := range []string{"CL_WEB_ADDR", "CL_HEALTH_ADDR", "CL_AUTH_ADMINS", "CL_AUTH_USERS", "CL_AUTH_LOCAL_ADMIN_HOSTS", "CL_WEB_ALT_URLS"} {
		t.Run(name, func(t *testing.T) {
			v := t172Values()
			v["extraEnv"] = []any{m{"name": name, "value": "x"}}
			t172Refused(t, v, name)
		})
	}
}

// ── cookie, compte local ─────────────────────────────────────────────────────────────────────────────────────────

func t172SecretValue(t *testing.T, s m, key string) string {
	t.Helper()
	if sd, ok := get(s, "stringData", key).(string); ok {
		return sd
	}
	d, _ := get(s, "data", key).(string)
	b, err := base64.StdEncoding.DecodeString(d)
	if err != nil {
		t.Fatalf("clé %s illisible dans le Secret : %v", key, err)
	}
	return string(b)
}

func TestChartAuth172CookieSecret(t *testing.T) {
	needHelm(t)
	s := t172Named(must(t, t172Values()), "Secret", t172Release+"-auth-cookie")
	if s == nil {
		t.Fatal("Secret rel-auth-cookie absent")
	}
	if v := t172SecretValue(t, s, "cookie-secret"); len(v) != 32 {
		t.Errorf("cookie-secret : %d caractères, attendu 32", len(v))
	}
	if get(s, "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Errorf("annotation keep absente : %v", get(s, "metadata", "annotations"))
	}
}

func TestChartAuth172LocalAdminSecret(t *testing.T) {
	needHelm(t)
	o := must(t, t172Local())
	s := t172Named(o, "Secret", t172Release+"-local-admin")
	if s == nil {
		t.Fatal("Secret rel-local-admin absent")
	}
	pw, ht := t172SecretValue(t, s, "password"), t172SecretValue(t, s, "htpasswd")
	if !regexp.MustCompile(`^[A-Za-z0-9]{32}$`).MatchString(pw) {
		t.Errorf("mot de passe : %d caractères, attendu 32 alphanumériques", len(pw))
	}
	if m := regexp.MustCompile(`^admin@local:\$2[aby]\$(\d\d)\$[./A-Za-z0-9]{53}\s*$`).FindStringSubmatch(ht); m == nil {
		t.Errorf("htpasswd = %q, attendu « admin@local:$2y$NN$… » (bcrypt)", ht)
	} else if m[1] < "10" {
		t.Errorf("coût bcrypt %s < 10", m[1])
	}
	if strings.Count(strings.TrimSpace(ht), "\n") != 0 {
		t.Error("htpasswd : une seule ligne attendue")
	}
	if get(s, "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Errorf("annotation keep absente : %v", get(s, "metadata", "annotations"))
	}
	// jamais ailleurs : le mot de passe n'apparaît dans aucun autre objet ni dans NOTES.txt
	for kind, objs := range o {
		for _, x := range objs {
			if kind == "Secret" && get(x, "metadata", "name") == t172Release+"-local-admin" {
				continue
			}
			if b, _ := yaml.Marshal(x); strings.Contains(string(b), pw) {
				t.Errorf("mot de passe en clair dans %s %v", kind, get(x, "metadata", "name"))
			}
		}
	}
	notes := t172Notes(t, t172Local())
	if strings.Contains(notes, pw) {
		t.Error("mot de passe dans NOTES.txt")
	}
	if !strings.Contains(notes, "kubectl") || !strings.Contains(notes, t172Release+"-local-admin") || !strings.Contains(notes, "base64 -d") {
		t.Errorf("NOTES.txt sans la commande de lecture du mot de passe : %s", notes)
	}
}

// Arguments et variables du compte local ; Secret fourni : aucun Secret généré, aucun mot de passe en clair.
func TestChartAuth172LocalAdminWiring(t *testing.T) {
	needHelm(t)
	o := must(t, t172Local())
	args := t172Args(t172Container(t, o, "auth-proxy"))
	for _, want := range []string{"--htpasswd-file=/etc/cl-auth/local/htpasswd", "--display-htpasswd-form=true", "--skip-provider-button=false"} {
		if !t172HasArg(args, want) {
			t.Errorf("argument %q absent : %v", want, args)
		}
	}
	if t172HasArg(args, "--skip-provider-button=true") {
		t.Error("--skip-provider-button=true avec le compte local (la page de connexion doit s'afficher)")
	}
	if e := t172Env(t172Container(t, o, "launcher")); e["CL_AUTH_LOCAL_ADMIN_HOSTS"] != "launcher-test.coupel.lan" {
		t.Errorf("CL_AUTH_LOCAL_ADMIN_HOSTS = %q, attendu l'hôte interne par défaut", e["CL_AUTH_LOCAL_ADMIN_HOSTS"])
	}
	// montage en lecture seule du Secret, mode 0440
	var vol m
	for _, x := range pod(t, o)["volumes"].([]any) {
		if get(x, "secret", "secretName") == t172Release+"-local-admin" {
			vol = x.(m)
		}
	}
	if vol == nil {
		t.Fatal("volume du Secret du compte local absent")
	}
	// contrat (écart justifié, helm-values.md) : 0444 (292) — pas de fsGroup, un 0440 serait illisible par l'uid du pod
	if dm := fmt.Sprint(get(vol, "secret", "defaultMode")); dm != "292" {
		t.Errorf("defaultMode = %s, attendu 292 (0444)", dm)
	}
	ro := false
	for _, vm := range get(t172Container(t, o, "auth-proxy"), "volumeMounts").([]any) {
		if get(vm, "name") == vol["name"] && get(vm, "mountPath") == "/etc/cl-auth/local" && get(vm, "readOnly") == true {
			ro = true
		}
	}
	if !ro {
		t.Errorf("montage /etc/cl-auth/local en lecture seule absent : %v", get(t172Container(t, o, "auth-proxy"), "volumeMounts"))
	}
	// hôtes explicites
	v := t172Local()
	sub(v, "auth", "localAdmin")["hosts"] = []any{"a.coupel.lan", "b.coupel.lan"}
	if e := t172Env(t172Container(t, must(t, v), "launcher")); e["CL_AUTH_LOCAL_ADMIN_HOSTS"] != "a.coupel.lan,b.coupel.lan" {
		t.Errorf("hosts explicites : %q", e["CL_AUTH_LOCAL_ADMIN_HOSTS"])
	}
	// Secret fourni : rien de généré, le volume pointe vers lui
	v = t172Local()
	sub(v, "auth", "localAdmin")["existingSecret"] = "mon-htpasswd"
	o2 := must(t, v)
	if t172Named(o2, "Secret", t172Release+"-local-admin") != nil {
		t.Error("Secret local-admin généré malgré existingSecret")
	}
	found := false
	for _, x := range pod(t, o2)["volumes"].([]any) {
		if get(x, "secret", "secretName") == "mon-htpasswd" {
			found = true
		}
	}
	if !found {
		t.Error("le volume ne pointe pas vers auth.localAdmin.existingSecret")
	}
}

// Compte local désactivé : ni formulaire, ni --htpasswd-file, ni Secret, ni hôtes.
func TestChartAuth172LocalAdminDisabled(t *testing.T) {
	needHelm(t)
	o := must(t, t172Values())
	if t172Named(o, "Secret", t172Release+"-local-admin") != nil {
		t.Error("Secret local-admin rendu compte local désactivé")
	}
	args := t172Args(t172Container(t, o, "auth-proxy"))
	for _, bad := range []string{"--htpasswd-file", "--display-htpasswd-form"} {
		if t172ArgPrefix(args, bad) != "" {
			t.Errorf("%s présent compte local désactivé", bad)
		}
	}
	if !t172HasArg(args, "--skip-provider-button=true") {
		t.Error("--skip-provider-button=true attendu (redirection directe vers GitHub)")
	}
}

// GitHub coupé (QUALIF avant les applications GitHub) : client factice, pas de Secret OAuth exigé, bouton GitHub absent.
func TestChartAuth172GithubDisabled(t *testing.T) {
	needHelm(t)
	v := t172Local()
	sub(v, "auth", "github")["enabled"] = false
	delete(sub(v, "auth"), "existingSecret")
	delete(sub(v, "auth"), "admins")
	o := must(t, v)
	px := t172Container(t, o, "auth-proxy")
	args := t172Args(px)
	if !t172HasArg(args, "--client-id=disabled") {
		t.Errorf("--client-id=disabled absent : %v", args)
	}
	for _, e := range get(px, "env").([]any) {
		if ref := get(e, "valueFrom", "secretKeyRef"); ref != nil && get(ref, "name") == "gh-oauth" {
			t.Errorf("référence au Secret OAuth alors que GitHub est coupé : %v", e)
		}
	}
	if t172ArgPrefix(args, "--github-user=") != "" {
		t.Errorf("--github-user présent sans GitHub : %v", args)
	}
	cm := t172Named(o, "ConfigMap", t172Release+"-auth-templates")
	if cm == nil {
		t.Fatal("ConfigMap des gabarits absente")
	}
	si, _ := get(cm, "data", "sign_in.html").(string)
	if strings.Contains(si, "Se connecter avec GitHub") || !strings.Contains(si, "Compte local") {
		t.Errorf("sign_in.html sans GitHub : bouton GitHub présent ou colonne locale absente")
	}
}

// ── ConfigMap des gabarits (M2, M7 B) ────────────────────────────────────────────────────────────────────────────

func TestChartAuth172Templates(t *testing.T) {
	needHelm(t)
	for name, tc := range map[string]struct {
		v      m
		github bool
		local  bool
	}{
		"GitHub seul":     {t172Values(), true, false},
		"GitHub et local": {t172Local(), true, true},
	} {
		o := must(t, tc.v)
		cm := t172Named(o, "ConfigMap", t172Release+"-auth-templates")
		if cm == nil {
			t.Fatalf("%s : ConfigMap rel-auth-templates absente", name)
		}
		si, _ := get(cm, "data", "sign_in.html").(string)
		er, _ := get(cm, "data", "error.html").(string)
		if si == "" || er == "" {
			t.Fatalf("%s : error.html (%d octets) et sign_in.html (%d octets) attendus", name, len(er), len(si))
		}
		if has := strings.Contains(si, "Se connecter avec GitHub"); has != tc.github {
			t.Errorf("%s : bouton GitHub présent = %v, attendu %v", name, has, tc.github)
		}
		if has := strings.Contains(si, "Compte local"); has != tc.local {
			t.Errorf("%s : colonne « Compte local » présente = %v, attendu %v", name, has, tc.local)
		}
		if tc.local {
			for _, w := range []string{"Compte GitHub", "Identifiant ou mot de passe incorrect"} {
				if !strings.Contains(si, w) {
					t.Errorf("%s : sign_in.html sans « %s »", name, w)
				}
			}
		}
		for _, w := range []string{"Accès refusé", "Trop de tentatives", "/sign_out?rd=%2Fsigned-out"} { // {{.ProxyPrefix}} = /oauth2
			if !strings.Contains(er, w) {
				t.Errorf("%s : error.html sans « %s »", name, w)
			}
		}
		// aucune ressource externe, aucun script
		for fn, page := range map[string]string{"sign_in.html": si, "error.html": er} {
			low := strings.ToLower(page)
			if strings.Contains(low, "<script") || regexp.MustCompile(`(src|href)\s*=\s*["']https?://`).MatchString(low) ||
				strings.Contains(low, "@import") || regexp.MustCompile(`url\(\s*["']?https?:`).MatchString(low) {
				t.Errorf("%s / %s : script ou ressource externe", name, fn)
			}
		}
		// montage en lecture seule dans le proxy
		ro := false
		for _, vm := range get(t172Container(t, o, "auth-proxy"), "volumeMounts").([]any) {
			if get(vm, "mountPath") == "/etc/cl-auth/templates" && get(vm, "readOnly") == true {
				ro = true
			}
		}
		if !ro {
			t.Errorf("%s : /etc/cl-auth/templates non monté en lecture seule", name)
		}
	}
}

// ── limite d'essais (rateLimit sur /oauth2/) ─────────────────────────────────────────────────────────────────────

func TestChartAuth172RateLimitTraefik(t *testing.T) {
	needHelm(t)
	o := must(t, t172Local())
	var rl m
	for _, mw := range o["Middleware"] {
		if r, ok := get(mw, "spec", "rateLimit").(m); ok {
			rl = r
		}
	}
	if rl == nil {
		t.Fatal("aucun Middleware rateLimit")
	}
	if fmt.Sprint(rl["average"]) != "10" || fmt.Sprint(rl["burst"]) != "20" {
		t.Errorf("rateLimit = %v, attendu average 10, burst 20", rl)
	}
	// un Ingress dédié sur /oauth2 porte le middleware ; l'Ingress principal sert le reste
	var oauth m
	for _, ing := range o["Ingress"] {
		for _, r := range get(ing, "spec", "rules").([]any) {
			ps, _ := get(r, "http", "paths").([]any)
			for _, p := range ps {
				if strings.HasPrefix(fmt.Sprint(get(p, "path")), "/oauth2") {
					oauth = ing
				}
			}
		}
	}
	if oauth == nil {
		t.Fatal("aucun Ingress dédié à /oauth2")
	}
	if a := fmt.Sprint(get(oauth, "metadata", "annotations", "traefik.ingress.kubernetes.io/router.middlewares")); !strings.Contains(a, "ratelimit") && !strings.Contains(strings.ToLower(a), "rate") {
		t.Errorf("Ingress /oauth2 sans middleware de limite : %q", a)
	}
	// valeurs modifiables
	v := t172Local()
	sub(v, "auth")["rateLimit"] = m{"average": 5, "burst": 8}
	for _, mw := range must(t, v)["Middleware"] {
		if r, ok := get(mw, "spec", "rateLimit").(m); ok && (fmt.Sprint(r["average"]) != "5" || fmt.Sprint(r["burst"]) != "8") {
			t.Errorf("rateLimit configuré = %v", r)
		}
	}
}

func TestChartAuth172RateLimitNginx(t *testing.T) {
	needHelm(t)
	v := t172Local()
	sub(v, "ingress")["controller"] = "nginx"
	sub(v, "ingress")["className"] = "nginx"
	o := must(t, v)
	found := false
	for _, ing := range o["Ingress"] {
		for _, r := range get(ing, "spec", "rules").([]any) {
			ps, _ := get(r, "http", "paths").([]any)
			for _, p := range ps {
				if strings.HasPrefix(fmt.Sprint(get(p, "path")), "/oauth2") {
					found = true
					if get(ing, "metadata", "annotations", "nginx.ingress.kubernetes.io/limit-rpm") == nil {
						t.Errorf("Ingress /oauth2 sans limit-rpm : %v", get(ing, "metadata", "annotations"))
					}
				}
			}
		}
	}
	if !found {
		t.Error("aucun Ingress dédié à /oauth2 (nginx)")
	}
}

// ── NOTES.txt, version du chart ──────────────────────────────────────────────────────────────────────────────────

func TestChartAuth172Notes(t *testing.T) {
	needHelm(t)
	notes := t172Notes(t, t172Values())
	for _, w := range []string{"https://launcher-test.coupel.lan", "1 administrateur(s)", "gh-oauth"} {
		if !strings.Contains(notes, w) {
			t.Errorf("NOTES.txt sans « %s »", w)
		}
	}
	for _, bad := range []string{"aucune authentification", "?t=", "desktop-web-token"} {
		if strings.Contains(notes, bad) {
			t.Errorf("NOTES.txt contient « %s »", bad)
		}
	}
	if strings.Contains(notes, t172Admin) {
		t.Errorf("NOTES.txt liste les logins (« %s »)", t172Admin)
	}
	// addendum 2 (MAJ-5) : consigne de retirer le login avant de renommer ou supprimer son compte GitHub
	if !strings.Contains(strings.ToLower(notes), "renomm") {
		t.Error("NOTES.txt sans la consigne « retirer le login avant de renommer ou supprimer son compte GitHub »")
	}
}

func TestChartAuth172ChartVersion(t *testing.T) {
	b, err := os.ReadFile(chartDir + "/Chart.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.Version != "7.0.0" || c.AppVersion != "v7.0.0" {
		t.Errorf("Chart.yaml : version %q appVersion %q, attendu 7.0.0 et v7.0.0 (règle #204)", c.Version, c.AppVersion)
	}
}

func TestChartAuth172Lint(t *testing.T) {
	needHelm(t)
	for name, v := range map[string]m{"GitHub": t172Values(), "GitHub et local": t172Local()} {
		out, errOut, err := helm(t, "lint", chartDir, "--strict", "-f", writeVals(t, v))
		if err != nil {
			t.Errorf("%s : helm lint --strict : %v\n%s%s", name, err, out, errOut)
		}
	}
}

// ── addendum 2 (revue sécurité) : Authorization vidé, en-têtes, cookies distincts, hôte backend, 429, arguments ─────

// t172Ext : valeurs avec l'ingress externe (instance auth-proxy-ext, cookie distinct).
func t172Ext() m {
	v := t172Local()
	v["externalIngress"] = m{
		"enabled": true, "className": "traefik", "host": "launcher.example.org",
		"tls": m{"clusterIssuer": "letsencrypt-prod"},
	}
	return v
}

func t172Anno(ing m) string {
	a, _ := get(ing, "metadata", "annotations").(m)
	return fmt.Sprint(a["traefik.ingress.kubernetes.io/router.middlewares"])
}

// MAJ-1 : le chemin Basic-auth est fermé — middleware headers (Authorization vidé, en-têtes de sécurité, m7) référencé par
// CHAQUE ingress ; ipStrategy.depth 0 explicite sur ipAllowList et rateLimit.
func TestChartAuth172AuthorizationHeaderStrippedTraefik(t *testing.T) {
	needHelm(t)
	o := must(t, t172Ext())
	hd := t172Named(o, "Middleware", t172Release+"-headers")
	if hd == nil {
		t.Fatal("Middleware rel-headers absent")
	}
	if a, ok := get(hd, "spec", "headers", "customRequestHeaders", "Authorization").(string); !ok || a != "" {
		t.Errorf("customRequestHeaders.Authorization = %#v, attendu la chaîne vide (en-tête retiré)", get(hd, "spec", "headers", "customRequestHeaders"))
	}
	b, _ := yaml.Marshal(get(hd, "spec", "headers"))
	hs := strings.ToLower(string(b))
	for _, w := range []string{"stsseconds", "frame-ancestors 'none'", "nosniff", "no-referrer"} {
		if !strings.Contains(hs, w) {
			t.Errorf("en-têtes de sécurité (m7) : « %s » absent :\n%s", w, hs)
		}
	}
	if n := len(o["Ingress"]); n < 3 { // interne, /oauth2 interne, externe (au moins)
		t.Errorf("%d Ingress, attendu au moins 3 (interne, /oauth2, externe)", n)
	}
	for _, ing := range o["Ingress"] {
		if !strings.Contains(t172Anno(ing), t172Release+"-headers") {
			t.Errorf("Ingress %v sans middleware headers : %q", get(ing, "metadata", "name"), t172Anno(ing))
		}
	}
	for _, mw := range o["Middleware"] {
		for _, k := range []string{"ipAllowList", "rateLimit"} {
			if sp, ok := get(mw, "spec", k).(m); ok {
				path := []string{"ipStrategy", "depth"}
				if k == "rateLimit" { // Traefik : le critère de source porte la stratégie d'adresse
					path = []string{"sourceCriterion", "ipStrategy", "depth"}
				}
				if d, ok := get(sp, path...).(int); !ok || d != 0 {
					t.Errorf("%s de %v : %v = %#v, attendu 0 explicite", k, get(mw, "metadata", "name"), path, get(sp, path...))
				}
			}
		}
	}
}

func TestChartAuth172AuthorizationHeaderStrippedNginx(t *testing.T) {
	needHelm(t)
	v := t172Local()
	sub(v, "ingress")["controller"] = "nginx"
	sub(v, "ingress")["className"] = "nginx"
	o := must(t, v)
	for _, ing := range o["Ingress"] {
		ok := false
		for _, val := range get(ing, "metadata", "annotations").(m) {
			if strings.Contains(fmt.Sprint(val), `proxy_set_header Authorization ""`) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("Ingress %v sans proxy_set_header Authorization vide", get(ing, "metadata", "name"))
		}
	}
}

// Arguments explicites d'oauth2-proxy (m4) et exemption /too-many.
func TestChartAuth172ProxyHardeningArguments(t *testing.T) {
	needHelm(t)
	args := t172Args(t172Container(t, must(t, t172Local()), "auth-proxy"))
	for _, want := range []string{
		"--pass-basic-auth=false", "--pass-authorization-header=false", "--set-xauthrequest=false", "--skip-auth-strip-headers=true",
		"--redirect-url=https://launcher-test.coupel.lan/oauth2/callback", "--skip-auth-route=GET=^/signed-out$", "--skip-auth-route=GET=^/too-many$",
	} {
		if !t172HasArg(args, want) {
			t.Errorf("argument %q absent de %v", want, args)
		}
	}
	for _, bad := range []string{"--metrics-address", "--whitelist-domain"} {
		if a := t172ArgPrefix(args, bad); a != "" && a != bad+"=" {
			t.Errorf("%s présent (%q) : attendu absent ou vide", bad, a)
		}
	}
}

// Page 429 : middleware errors (status 429 → Service du launcher, port http, /too-many) qui ENCADRE rateLimit (listé avant lui :
// Traefik applique les middlewares dans l'ordre, errors doit être en amont pour intercepter le 429 du rateLimit).
func TestChartAuth172TooManyPage(t *testing.T) {
	needHelm(t)
	o := must(t, t172Local())
	er := t172Named(o, "Middleware", t172Release+"-errors")
	if er == nil {
		t.Fatal("Middleware rel-errors absent")
	}
	st := fmt.Sprint(get(er, "spec", "errors", "status"))
	if !strings.Contains(st, "429") || get(er, "spec", "errors", "query") != "/too-many" || fmt.Sprint(get(er, "spec", "errors", "service", "port")) != "http" {
		t.Errorf("errors = %v, attendu status [429], query /too-many, service port http", get(er, "spec", "errors"))
	}
	found := false
	for _, ing := range o["Ingress"] {
		for _, r := range get(ing, "spec", "rules").([]any) {
			ps, _ := get(r, "http", "paths").([]any)
			for _, p := range ps {
				if strings.HasPrefix(fmt.Sprint(get(p, "path")), "/oauth2") {
					found = true
					a := t172Anno(ing)
					ri, ei := strings.Index(a, "ratelimit"), strings.Index(a, "errors")
					if ri < 0 || ei < 0 || ei > ri {
						t.Errorf("middlewares de /oauth2 : %q, attendu errors puis rateLimit (errors encadre rateLimit)", a)
					}
				}
			}
		}
	}
	if !found {
		t.Error("aucun Ingress /oauth2")
	}
}

// m1 + m3 : instance externe — cookies distincts, redirect-url fixe, sans htpasswd, ConfigMap propre, ports du Service, NetworkPolicy.
func TestChartAuth172ExternalInstance(t *testing.T) {
	needHelm(t)
	o := must(t, t172Ext())
	in, ex := t172Container(t, o, "auth-proxy"), t172Container(t, o, "auth-proxy-ext")
	if ex == nil {
		t.Fatal("auth-proxy-ext absent avec l'ingress externe")
	}
	ia, ea := t172Args(in), t172Args(ex)
	if !t172HasArg(ia, "--cookie-name=_cl_auth") || !t172HasArg(ea, "--cookie-name=_cl_auth_ext") {
		t.Errorf("noms de cookie : interne %v / externe %v", t172ArgPrefix(ia, "--cookie-name="), t172ArgPrefix(ea, "--cookie-name="))
	}
	for _, bad := range []string{"--htpasswd-file", "--display-htpasswd-form"} {
		if t172ArgPrefix(ea, bad) != "" {
			t.Errorf("auth-proxy-ext : %s présent (le formulaire n'existe pas sur Internet)", bad)
		}
	}
	if !t172HasArg(ea, "--skip-provider-button=true") || !t172HasArg(ea, "--http-address=0.0.0.0:8083") ||
		!t172HasArg(ea, "--redirect-url=https://launcher.example.org/oauth2/callback") {
		t.Errorf("arguments de l'instance externe : %v", ea)
	}
	secrets := map[string]string{}
	for name, c := range map[string]m{"interne": in, "externe": ex} {
		for _, e := range get(c, "env").([]any) {
			if get(e, "name") == "OAUTH2_PROXY_COOKIE_SECRET" {
				secrets[name] = fmt.Sprint(get(e, "valueFrom", "secretKeyRef", "name"))
			}
		}
	}
	if secrets["interne"] != t172Release+"-auth-cookie" || secrets["externe"] != t172Release+"-auth-cookie-ext" {
		t.Errorf("Secrets de cookie : %v, attendu rel-auth-cookie et rel-auth-cookie-ext", secrets)
	}
	a, b := t172Named(o, "Secret", t172Release+"-auth-cookie"), t172Named(o, "Secret", t172Release+"-auth-cookie-ext")
	if a == nil || b == nil {
		t.Fatal("Secrets de cookie absents")
	}
	if t172SecretValue(t, a, "cookie-secret") == t172SecretValue(t, b, "cookie-secret") {
		t.Error("les deux cookie-secret sont identiques : un cookie interne ouvrirait une session externe")
	}
	if get(b, "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Error("cookie externe sans annotation keep")
	}
	// ConfigMap propre à l'externe : colonne GitHub seule
	cm := t172Named(o, "ConfigMap", t172Release+"-auth-templates-ext")
	if cm == nil {
		t.Fatal("ConfigMap rel-auth-templates-ext absente")
	}
	if si, _ := get(cm, "data", "sign_in.html").(string); strings.Contains(si, "Compte local") {
		t.Error("sign_in.html externe avec la colonne « Compte local »")
	}
	// Service : http et http-ext seulement ; jamais health ni 8081
	var ports []string
	for _, p := range get(o["Service"][0], "spec", "ports").([]any) {
		ports = append(ports, fmt.Sprint(get(p, "name"), "/", get(p, "port")))
	}
	if strings.Join(ports, ",") != "http/"+fmt.Sprint(get(o["Service"][0], "spec", "ports").([]any)[0].(m)["port"])+",http-ext/8083" {
		t.Errorf("ports du Service = %v, attendu http et http-ext/8083", ports)
	}
	for _, p := range ports {
		if strings.Contains(p, "health") || strings.HasSuffix(p, "/8081") || strings.HasSuffix(p, "/8082") {
			t.Errorf("port %s exposé par le Service", p)
		}
	}
	// NetworkPolicy : entrée sur http et http-ext
	if len(o["NetworkPolicy"]) > 0 {
		b, _ := yaml.Marshal(o["NetworkPolicy"])
		if !strings.Contains(string(b), "http-ext") && !strings.Contains(string(b), "8083") {
			t.Errorf("NetworkPolicy sans le port http-ext : %s", b)
		}
		if strings.Contains(string(b), "8082") || strings.Contains(string(b), "8081") {
			t.Errorf("NetworkPolicy ouvre 8081/8082 : %s", b)
		}
	}
	// l'Ingress externe vise http-ext, sans htpasswd (pas de /oauth2 local) ; adresses du launcher
	var extIng m
	for _, ing := range o["Ingress"] {
		if strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), "launcher.example.org") {
			extIng = ing
		}
	}
	if extIng == nil || !strings.Contains(fmt.Sprint(get(extIng, "spec", "rules")), "http-ext") {
		t.Errorf("Ingress externe absent ou ne visant pas http-ext : %v", get(extIng, "spec", "rules"))
	}
	e := t172Env(t172Container(t, o, "launcher"))
	if e["CL_WEB_URL"] != "https://launcher.example.org" || e["CL_WEB_ALT_URLS"] != "https://launcher-test.coupel.lan" {
		t.Errorf("CL_WEB_URL / CL_WEB_ALT_URLS = %q / %q", e["CL_WEB_URL"], e["CL_WEB_ALT_URLS"])
	}
	if e["CL_AUTH_LOCAL_ADMIN_HOSTS"] != "launcher-test.coupel.lan" {
		t.Errorf("CL_AUTH_LOCAL_ADMIN_HOSTS = %q : l'hôte externe n'y figure jamais", e["CL_AUTH_LOCAL_ADMIN_HOSTS"])
	}
	// rateLimit sur /oauth2/ de chaque ingress
	n := 0
	for _, ing := range o["Ingress"] {
		if strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), "/oauth2") {
			n++
		}
	}
	if n < 2 {
		t.Errorf("%d Ingress /oauth2, attendu un par hôte (interne et externe)", n)
	}
}

func TestChartAuth172ExternalRefusals(t *testing.T) {
	needHelm(t)
	must(t, t172Ext())
	v := t172Ext()
	sub(v, "auth", "localAdmin")["hosts"] = []any{"launcher-test.coupel.lan", "launcher.example.org"}
	t.Run("hôte externe dans localAdmin.hosts", func(t *testing.T) { t172Refused(t, v, "launcher.example.org") })
	v = t172Ext()
	sub(v, "auth")["enabled"] = false
	t.Run("externe sans authentification", func(t *testing.T) { t172Refused(t, v, "authentification désactivée") })
	v = t172Ext()
	sub(v, "externalIngress", "tls")["clusterIssuer"] = "interne" // émetteur interne de ingress.tls
	t.Run("émetteur interne sur l'externe", func(t *testing.T) { t172Refused(t, v, "clusterIssuer") })
}

// MAJ-3 : backend host interdit derrière le proxy d'authentification (une session tmux joindrait 127.0.0.1:8081).
func TestChartAuth172HostBackendRefused(t *testing.T) {
	needHelm(t)
	v := t172Values()
	v["allowHostBackend"] = true
	t.Run("allowHostBackend avec auth", func(t *testing.T) { t172Refused(t, v, "backend host interdit derrière le proxy d'authentification") })
	// sans authentification : permis (comportement de v6.9.0), CL_ALLOW_HOST_BACKEND posée
	v = baseValues()
	v["auth"] = m{"enabled": false}
	v["allowHostBackend"] = true
	if e := env(t, must(t, v)); e["CL_ALLOW_HOST_BACKEND"] != "1" && e["CL_ALLOW_HOST_BACKEND"] != "true" {
		t.Errorf("auth désactivée : CL_ALLOW_HOST_BACKEND = %q", e["CL_ALLOW_HOST_BACKEND"])
	}
	// jamais posée avec l'authentification
	if e := t172Env(t172Container(t, must(t, t172Values()), "launcher")); e["CL_ALLOW_HOST_BACKEND"] != "" {
		t.Errorf("CL_ALLOW_HOST_BACKEND = %q derrière le proxy", e["CL_ALLOW_HOST_BACKEND"])
	}
	// extraEnv ne la pose pas non plus
	v = t172Values()
	v["extraEnv"] = []any{m{"name": "CL_ALLOW_HOST_BACKEND", "value": "1"}}
	t.Run("extraEnv", func(t *testing.T) { t172Refused(t, v, "CL_ALLOW_HOST_BACKEND") })
}

// Variables de session : CL_AUTH_SESSION_DURATION / _REFRESH (réservées) = valeurs du chart.
func TestChartAuth172SessionEnv(t *testing.T) {
	needHelm(t)
	e := t172Env(t172Container(t, must(t, t172Values()), "launcher"))
	if e["CL_AUTH_SESSION_DURATION"] != "8h" || e["CL_AUTH_SESSION_REFRESH"] != "1h" {
		t.Errorf("session = %q / %q, attendu 8h / 1h", e["CL_AUTH_SESSION_DURATION"], e["CL_AUTH_SESSION_REFRESH"])
	}
	v := t172Values()
	sub(v, "auth")["sessionDuration"], sub(v, "auth")["sessionRefresh"] = "12h", "30m"
	e = t172Env(t172Container(t, must(t, v), "launcher"))
	if e["CL_AUTH_SESSION_DURATION"] != "12h" || e["CL_AUTH_SESSION_REFRESH"] != "30m" {
		t.Errorf("session configurée = %q / %q", e["CL_AUTH_SESSION_DURATION"], e["CL_AUTH_SESSION_REFRESH"])
	}
	for _, name := range []string{"CL_AUTH_SESSION_DURATION", "CL_AUTH_SESSION_REFRESH"} {
		v := t172Values()
		v["extraEnv"] = []any{m{"name": name, "value": "1h"}}
		t.Run("reserve/"+name, func(t *testing.T) { t172Refused(t, v, name) })
	}
}

// m7 : gabarits — aucune injection : jamais safeHTML ni script ; les valeurs du proxy passent par {{ .X }} (auto-échappé).
func TestChartAuth172TemplatesNoInjection(t *testing.T) {
	for _, f := range []string{"error.html", "sign_in.html", "sign_in.local-only.html"} {
		b, err := os.ReadFile(chartDir + "/files/auth-templates/" + f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, bad := range []string{"safeHTML", "safeJS", "safeURL", "safeHTMLAttr", "<script", "javascript:"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s : « %s » présent", f, bad)
			}
		}
		for _, a := range regexp.MustCompile(`\{\{[^}]*\}\}`).FindAllString(s, -1) {
			if bare := regexp.MustCompile(`"[^"]*"`).ReplaceAllString(a, `""`); strings.Contains(bare, "|") || regexp.MustCompile(`\b(html|js|urlquery|print)\b`).MatchString(bare) {
				t.Errorf("%s : action de gabarit suspecte %s (seules {{ .X }} / {{ if }} / {{ end }} sont admises)", f, a)
			}
		}
	}
}
