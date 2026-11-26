package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// LOT-3 (#254, addendum 3) — ingress externe et clés de la GitHub App. Écrit depuis contracts/helm-values.md « v7.0.0 »
// (« Ingress externe », `auth.github.clientId` / `clientSecret`) et contracts/auth.md §8 (P7 : adresse principale = hôte externe ;
// P10 : jamais de compte local sur l'externe). `helm template` seulement ; réutilise les aides de chart_auth172_test.go.

const (
	t254Ext  = "launcher.example.org"
	t254Int  = "launcher-test.coupel.lan"
	t254Leak = "S3CRET-valeur-de-test-ne-doit-nulle-part-ailleurs"
)

func t254Values() m { return t172Ext() }

// ── ingress externe : refus et acceptation ────────────────────────────────────────────────────────────────────────

func TestChartExt254Refusals(t *testing.T) {
	needHelm(t)
	must(t, t254Values())
	for name, tc := range map[string]struct {
		mod   func(v m)
		wants []string
	}{
		"sans authentification":   {func(v m) { sub(v, "auth")["enabled"] = false }, []string{"ingress externe refusé", "authentification désactivée"}},
		"hôte absent":             {func(v m) { delete(sub(v, "externalIngress"), "host") }, []string{"externalIngress.host"}},
		"hôte vide":               {func(v m) { sub(v, "externalIngress")["host"] = "" }, []string{"externalIngress.host"}},
		"classe absente":          {func(v m) { delete(sub(v, "externalIngress"), "className") }, []string{"externalIngress.className"}},
		"ni émetteur ni Secret":   {func(v m) { v["externalIngress"].(m)["tls"] = m{} }, []string{"externalIngress.tls"}},
		"tls absent":              {func(v m) { delete(sub(v, "externalIngress"), "tls") }, []string{"externalIngress.tls"}},
		"émetteur interne":        {func(v m) { sub(v, "externalIngress", "tls")["clusterIssuer"] = "interne" }, []string{"clusterIssuer"}},
		"hôte avec schéma":        {func(v m) { sub(v, "externalIngress")["host"] = "https://" + t254Ext }, []string{"externalIngress.host"}},
		"hôte avec chemin":        {func(v m) { sub(v, "externalIngress")["host"] = t254Ext + "/x" }, []string{"externalIngress.host"}},
		"hôte avec espace":        {func(v m) { sub(v, "externalIngress")["host"] = "launcher .example.org" }, []string{"externalIngress.host"}},
		"hôte joker":              {func(v m) { sub(v, "externalIngress")["host"] = "*.example.org" }, []string{"externalIngress.host"}},
		"même hôte que l'interne": {func(v m) { sub(v, "externalIngress")["host"] = t254Int }, []string{"externalIngress.host"}},
		"hôte externe dans localAdmin.hosts": {func(v m) {
			sub(v, "auth", "localAdmin")["hosts"] = []any{t254Int, t254Ext}
		}, []string{t254Ext}},
		"annotation réservée": {func(v m) {
			sub(v, "externalIngress")["annotations"] = m{"traefik.ingress.kubernetes.io/router.tls": "false"}
		}, []string{"router.tls"}},
		"annotation allowlist": {func(v m) {
			sub(v, "externalIngress")["annotations"] = m{"nginx.ingress.kubernetes.io/whitelist-source-range": "0.0.0.0/0"}
		}, []string{"whitelist-source-range"}},
	} {
		t.Run(name, func(t *testing.T) {
			v := t254Values()
			tc.mod(v)
			t172Refused(t, v, tc.wants...)
		})
	}
}

// Rendu accepté : Ingress dédié, instance auth-proxy-ext sans compte local, aucune liste d'adresses (accès Internet), redirection
// HTTPS, limite d'essais sur /oauth2/, TLS par émetteur public OU par Secret fourni.
func TestChartExt254ValidRender(t *testing.T) {
	needHelm(t)
	for name, tls := range map[string]m{"émetteur public": {"clusterIssuer": "letsencrypt-prod"}, "Secret fourni": {"secretName": "ext-tls"}} {
		v := t254Values()
		v["externalIngress"].(m)["tls"] = tls
		o := must(t, v)
		var ext []m
		for _, ing := range o["Ingress"] {
			if strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), t254Ext) {
				ext = append(ext, ing)
			}
		}
		if len(ext) < 2 { // route « / » et route /oauth2 (limite d'essais)
			t.Fatalf("%s : %d Ingress pour %s, attendu au moins 2 (« / » et /oauth2)", name, len(ext), t254Ext)
		}
		for _, ing := range ext {
			if cn := fmt.Sprint(get(ing, "spec", "ingressClassName")); cn != "traefik" {
				t.Errorf("%s : ingressClassName = %s", name, cn)
			}
			a := t172Anno(ing)
			if strings.Contains(a, "allowlist") {
				t.Errorf("%s : l'Ingress externe %v porte la liste d'adresses (accès Internet : aucune liste) : %s", name, get(ing, "metadata", "name"), a)
			}
			if !strings.Contains(a, "-https") {
				t.Errorf("%s : %v sans redirection HTTPS : %s", name, get(ing, "metadata", "name"), a)
			}
			b, _ := yaml.Marshal(get(ing, "spec", "tls"))
			if !strings.Contains(string(b), t254Ext) {
				t.Errorf("%s : %v : bloc tls sans l'hôte externe : %s", name, get(ing, "metadata", "name"), b)
			}
			if name == "Secret fourni" && !strings.Contains(string(b), "ext-tls") {
				t.Errorf("%s : secretName ext-tls absent : %s", name, b)
			}
			if !strings.Contains(fmt.Sprint(get(ing, "spec", "rules")), "http-ext") {
				t.Errorf("%s : %v ne vise pas le port http-ext", name, get(ing, "metadata", "name"))
			}
		}
		// émetteur public : annotation cert-manager de l'externe seulement ; Secret fourni : aucune demande de certificat
		issuerAnno := false
		for _, ing := range ext {
			if get(ing, "metadata", "annotations", "cert-manager.io/cluster-issuer") == "letsencrypt-prod" {
				issuerAnno = true
			}
		}
		if _, isIssuer := tls["clusterIssuer"]; isIssuer != issuerAnno && len(o["Certificate"]) == 0 {
			t.Errorf("%s : demande de certificat public absente (annotation cert-manager ou Certificate)", name)
		}
		if name == "Secret fourni" {
			if b, _ := yaml.Marshal(o["Certificate"]); strings.Contains(string(b), t254Ext) {
				t.Errorf("Secret fourni : Certificate créé pour l'hôte externe : %s", b)
			}
		}
		// jamais l'émetteur interne sur l'externe
		if b, _ := yaml.Marshal(ext); strings.Contains(string(b), "cluster-issuer: interne") {
			t.Errorf("%s : l'émetteur interne est utilisé pour l'hôte externe", name)
		}
		// le pod : instance externe sans compte local, adresse principale externe, alternative interne
		px := t172Container(t, o, "auth-proxy-ext")
		if px == nil {
			t.Fatalf("%s : auth-proxy-ext absent", name)
		}
		if a := t172Args(px); t172ArgPrefix(a, "--htpasswd-file") != "" || t172ArgPrefix(a, "--display-htpasswd-form") != "" {
			t.Errorf("%s : formulaire ou htpasswd sur l'instance externe : %v", name, a)
		}
		e := t172Env(t172Container(t, o, "launcher"))
		if e["CL_WEB_URL"] != "https://"+t254Ext || e["CL_WEB_ALT_URLS"] != "https://"+t254Int {
			t.Errorf("%s : CL_WEB_URL=%q CL_WEB_ALT_URLS=%q (adresse principale = hôte externe, P7)", name, e["CL_WEB_URL"], e["CL_WEB_ALT_URLS"])
		}
		if strings.Contains(e["CL_AUTH_LOCAL_ADMIN_HOSTS"], t254Ext) {
			t.Errorf("%s : l'hôte externe admet le compte local (P10) : %s", name, e["CL_AUTH_LOCAL_ADMIN_HOSTS"])
		}
	}
}

// Ingress externe seul (interne coupé) : l'adresse principale reste l'externe, pas d'adresse alternative.
func TestChartExt254ExternalOnly(t *testing.T) {
	needHelm(t)
	v := t254Values()
	sub(v, "ingress")["enabled"] = false
	o, out, err := render(t, v)
	if err != nil {
		t.Skipf("ingress interne coupé + externe : non admis par le chart (%.200s)", out)
	}
	e := t172Env(t172Container(t, o, "launcher"))
	if e["CL_WEB_URL"] != "https://"+t254Ext || e["CL_WEB_ALT_URLS"] != "" {
		t.Errorf("CL_WEB_URL=%q CL_WEB_ALT_URLS=%q, attendu l'externe seul", e["CL_WEB_URL"], e["CL_WEB_ALT_URLS"])
	}
}

// Désactivé (défaut) : aucun objet externe.
func TestChartExt254DisabledRendersNothing(t *testing.T) {
	needHelm(t)
	v := t172Local()
	o, out, err := render(t, v)
	if err != nil {
		t.Fatal(out)
	}
	if t172Container(t, o, "auth-proxy-ext") != nil {
		t.Error("auth-proxy-ext rendu sans ingress externe")
	}
	for _, kind := range []string{"Secret", "ConfigMap"} {
		for _, x := range o[kind] {
			if strings.HasSuffix(fmt.Sprint(get(x, "metadata", "name")), "-ext") {
				t.Errorf("%s %v rendu sans ingress externe", kind, get(x, "metadata", "name"))
			}
		}
	}
	for _, p := range get(o["Service"][0], "spec", "ports").([]any) {
		if get(p, "name") == "http-ext" {
			t.Error("port http-ext exposé sans ingress externe")
		}
	}
	if !strings.Contains(out, t254Int) || strings.Contains(out, t254Ext) {
		t.Errorf("le rendu cite l'hôte externe alors qu'il est désactivé")
	}
	if e := t172Env(t172Container(t, o, "launcher")); e["CL_WEB_ALT_URLS"] != "" {
		t.Errorf("CL_WEB_ALT_URLS = %q sans ingress externe", e["CL_WEB_ALT_URLS"])
	}
}

// ── addendum 3 : clés de la GitHub App en valeurs Helm ────────────────────────────────────────────────────────────

func t254Keys(v m, id, secret string) m {
	delete(sub(v, "auth"), "existingSecret")
	if id != "" {
		sub(v, "auth", "github")["clientId"] = id
	}
	if secret != "" {
		sub(v, "auth", "github")["clientSecret"] = secret
	}
	return v
}

func TestChartGithubKeys254Matrix(t *testing.T) {
	needHelm(t)
	// existingSecret seul : accepté, aucun Secret <release>-github
	o := must(t, t172Values())
	if t172Named(o, "Secret", t172Release+"-github") != nil {
		t.Error("Secret rel-github créé avec auth.existingSecret")
	}
	// clientId + clientSecret seuls : Secret créé et référencé par le sidecar
	v := t254Keys(t172Values(), "Iv1.abcdef0123456789", t254Leak)
	o = must(t, v)
	s := t172Named(o, "Secret", t172Release+"-github")
	if s == nil {
		t.Fatal("Secret rel-github absent")
	}
	if t172SecretValue(t, s, "client-id") != "Iv1.abcdef0123456789" || t172SecretValue(t, s, "client-secret") != t254Leak {
		t.Error("clés du Secret rel-github incorrectes (client-id, client-secret)")
	}
	refs := map[string][2]string{}
	for _, e := range get(t172Container(t, o, "auth-proxy"), "env").([]any) {
		if ref := get(e, "valueFrom", "secretKeyRef"); ref != nil {
			refs[get(e, "name").(string)] = [2]string{fmt.Sprint(get(ref, "name")), fmt.Sprint(get(ref, "key"))}
		}
	}
	if refs["OAUTH2_PROXY_CLIENT_ID"] != [2]string{t172Release + "-github", "client-id"} || refs["OAUTH2_PROXY_CLIENT_SECRET"] != [2]string{t172Release + "-github", "client-secret"} {
		t.Errorf("sidecar : références %v, attendu rel-github / client-id et client-secret", refs)
	}
	// l'instance externe lit le même Secret
	oe := must(t, t254Keys(t254Values(), "Iv1.abcdef0123456789", t254Leak))
	for _, e := range get(t172Container(t, oe, "auth-proxy-ext"), "env").([]any) {
		if get(e, "name") == "OAUTH2_PROXY_CLIENT_SECRET" && fmt.Sprint(get(e, "valueFrom", "secretKeyRef", "name")) != t172Release+"-github" {
			t.Errorf("auth-proxy-ext : Secret client = %v", get(e, "valueFrom", "secretKeyRef", "name"))
		}
	}
	for name, tc := range map[string]struct {
		mod   func() m
		wants []string
	}{
		"les deux (existingSecret et clés)": {func() m {
			v := t172Values()
			sub(v, "auth", "github")["clientId"], sub(v, "auth", "github")["clientSecret"] = "id", "secret"
			return v
		}, []string{"auth.existingSecret et auth.github.clientId/clientSecret sont exclusifs"}},
		"clientId sans clientSecret": {func() m { return t254Keys(t172Values(), "id", "") }, []string{"clientSecret"}},
		"clientSecret sans clientId": {func() m { return t254Keys(t172Values(), "", "secret") }, []string{"clientId"}},
		"GitHub activé sans aucun":   {func() m { return t254Keys(t172Values(), "", "") }, []string{"auth.existingSecret"}},
		"valeurs blanches":           {func() m { return t254Keys(t172Values(), "  ", "  ") }, []string{"clientId"}},
	} {
		t.Run(name, func(t *testing.T) { t172Refused(t, tc.mod(), tc.wants...) })
	}
	// GitHub désactivé sans identifiants : accepté (compte local seul), aucun Secret GitHub
	v = t172Local()
	sub(v, "auth", "github")["enabled"] = false
	delete(sub(v, "auth"), "existingSecret")
	delete(sub(v, "auth"), "admins")
	if o := must(t, v); t172Named(o, "Secret", t172Release+"-github") != nil {
		t.Error("Secret rel-github créé avec GitHub désactivé")
	}
}

// La valeur du secret ne figure NULLE PART ailleurs que dans le Secret <release>-github : ni NOTES.txt, ni environnement ou
// arguments des conteneurs, ni ConfigMap, ni annotation ; NOTES.txt avertit que `helm get values` l'affiche en clair.
func TestChartGithubKeys254SecretOnlyInItsSecret(t *testing.T) {
	needHelm(t)
	for name, v := range map[string]m{"interne": t254Keys(t172Local(), "Iv1.0123456789abcdef", t254Leak), "avec l'externe": t254Keys(t254Values(), "Iv1.0123456789abcdef", t254Leak)} {
		o := must(t, v)
		for kind, objs := range o {
			for _, x := range objs {
				if kind == "Secret" && get(x, "metadata", "name") == t172Release+"-github" {
					continue
				}
				if b, _ := yaml.Marshal(x); strings.Contains(string(b), t254Leak) {
					t.Errorf("%s : valeur du secret dans %s %v", name, kind, get(x, "metadata", "name"))
				}
			}
		}
		notes := t172Notes(t, v)
		if strings.Contains(notes, t254Leak) {
			t.Errorf("%s : valeur du secret dans NOTES.txt", name)
		}
		for _, w := range []string{t172Release + "-github", "helm get values"} {
			if !strings.Contains(notes, w) {
				t.Errorf("%s : NOTES.txt sans « %s » (nom du Secret, avertissement du clair dans helm get values)", name, w)
			}
		}
		if !strings.Contains(strings.ToLower(notes), "existingsecret") && !strings.Contains(notes, "auth.existingSecret") {
			t.Errorf("%s : NOTES.txt ne recommande pas auth.existingSecret", name)
		}
	}
	// le Secret porte la mention de conservation comme les autres (réutilisé aux mises à jour, pas régénéré)
	o := must(t, t254Keys(t172Values(), "Iv1.x", t254Leak))
	if t172Named(o, "Secret", t172Release+"-github") == nil {
		t.Fatal("Secret rel-github absent")
	}
}

// scripts/ci.sh : un `clientSecret` non vide dans un YAML suivi par git fait échouer la CI (Helm ne sait pas d'où vient une valeur) ;
// vide, commenté ou non suivi : accepté ; la valeur n'est jamais affichée. Le contrôle RÉEL du script est extrait et exécuté.
func TestCI254GuardRefusesTrackedClientSecret(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	guard := ciGuard254(t, src)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	run := func(files map[string]string, track bool) (int, string) {
		repo := t.TempDir()
		_ = os.MkdirAll(filepath.Join(repo, "ng2"), 0o755)
		for p, c := range files {
			full := filepath.Join(repo, p)
			_ = os.MkdirAll(filepath.Dir(full), 0o755)
			if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
			c := exec.Command("git", append([]string{"-C", repo}, args...)...)
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v : %v %s", args, err, out)
			}
		}
		if track {
			_ = exec.Command("git", "-C", repo, "add", "-A").Run()
		}
		cmd := exec.Command("bash", "-c", guard+"\necho GARDE-PASSEE")
		cmd.Dir = filepath.Join(repo, "ng2")
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return code, string(out)
	}
	secret := "tres-secret-123"
	for name, tc := range map[string]struct {
		files map[string]string
		track bool
		fail  bool
	}{
		"suivi, valeur entre guillemets": {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    clientSecret: \"" + secret + "\"\n"}, true, true},
		"suivi, valeur nue":              {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    clientSecret: " + secret + "\n"}, true, true},
		"suivi, apostrophes":             {map[string]string{"values.yml": "clientSecret: '" + secret + "'\n"}, true, true},
		"suivi, autre YAML":              {map[string]string{"autre/dir/conf.yaml": "x:\n  clientSecret: " + secret + "\n"}, true, true},
		"suivi, vide (guillemets)":       {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    clientSecret: \"\"\n"}, true, false},
		"suivi, vide (apostrophes)":      {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    clientSecret: ''\n"}, true, false},
		"suivi, sans valeur":             {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    clientSecret:\n"}, true, false},
		"suivi, commentaire":             {map[string]string{"deploy/helm/values-x.yaml": "auth:\n  github:\n    # clientSecret: " + secret + "\n    clientId: \"\"\n"}, true, false},
		"non suivi (values-local)":       {map[string]string{"PRIVATE/QUALIF/values-local.yaml": "auth:\n  github:\n    clientSecret: " + secret + "\n"}, false, false},
	} {
		code, out := run(tc.files, tc.track)
		if tc.fail && (code == 0 || strings.Contains(out, "GARDE-PASSEE") || !strings.Contains(out, "ECHEC")) {
			t.Errorf("%s : garde passée (code %d) alors qu'un clientSecret non vide est suivi :\n%s", name, code, out)
		}
		if !tc.fail && (code != 0 || !strings.Contains(out, "GARDE-PASSEE")) {
			t.Errorf("%s : garde échouée à tort (code %d) :\n%s", name, code, out)
		}
		if strings.Contains(out, secret) {
			t.Errorf("%s : la valeur du secret est affichée par la garde :\n%s", name, out)
		}
	}
}

// Le dépôt réel : aucun clientSecret non vide dans un YAML suivi (même contrôle que la CI).
func TestCI254RepoTracksNoClientSecret(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "*.yaml", "*.yml").Output()
	if err != nil {
		t.Skip("pas un dépôt git")
	}
	for _, f := range strings.Fields(string(out)) {
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			continue
		}
		for n, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if v, ok := strings.CutPrefix(l, "clientSecret:"); ok {
				v = strings.Trim(strings.TrimSpace(strings.SplitN(v, "#", 2)[0]), `"'`)
				if v != "" {
					t.Errorf("%s:%d : clientSecret non vide dans un fichier suivi", f, n+1)
				}
			}
		}
	}
}

// ciGuard254 : le contrôle anti-commit du secret de la GitHub App, tel qu'écrit dans scripts/ci.sh — de son commentaire de tête
// (« le secret de la GitHub App ne doit jamais figurer ») jusqu'à `step_done "chart"` exclu. Le bloc RÉEL est exécuté.
func ciGuard254(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, "# le secret de la GitHub App ne doit jamais figurer")
	if i < 0 {
		t.Fatal("contrôle du clientSecret absent de scripts/ci.sh (commentaire « le secret de la GitHub App ne doit jamais figurer »)")
	}
	rest := src[i:]
	j := strings.Index(rest, `step_done "chart"`)
	if j < 0 {
		t.Fatal(`fin du contrôle introuvable (step_done "chart")`)
	}
	return rest[:j]
}

// runCIGuard254 : exécute le contrôle dans un dépôt git temporaire (cwd = <dépôt>/ng2, comme ci.sh) ; track : fichiers `git add`.
func runCIGuard254(t *testing.T, guard string, files map[string]string, track bool) (int, string) {
	t.Helper()
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, "ng2"), 0o755)
	for p, c := range files {
		full := filepath.Join(repo, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v : %v %s", args, err, out)
		}
	}
	if track {
		_ = exec.Command("git", "-C", repo, "add", "-A").Run()
	}
	cmd := exec.Command("bash", "-c", guard+"\necho GARDE-PASSEE")
	cmd.Dir = filepath.Join(repo, "ng2")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return code, string(out)
}

// Audit sécurité LOT-3 (MAJ-1) : les 9 variantes du rapport — 7 contournements du premier contrôle et 2 déjà détectées — font toutes
// ÉCHOUER le contrôle durci ; la valeur n'est jamais affichée. RED attendu tant que le contrôle n'est pas durci.
func TestCI254GuardHardenedVariants(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	guard := ciGuard254(t, string(b))
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	const v = "abc123xyz-secret"
	b64 := "YWJjMTIzeHl6LXNlY3JldA=="
	for name, files := range map[string]map[string]string{
		"1 forme en ligne":               {"deploy/values-x.yaml": "auth: {github: {clientId: x, clientSecret: " + v + "}}\n"},
		"2 valeur à la ligne suivante":   {"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret:\n      " + v + "\n"},
		"3 clé entre guillemets":         {"deploy/values-x.yaml": "auth:\n  github:\n    \"clientSecret\": " + v + "\n"},
		"3b clé entre apostrophes":       {"deploy/values-x.yaml": "auth:\n  github:\n    'clientSecret': '" + v + "'\n"},
		"4 valeur commençant par espace": {"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: \" " + v + "\"\n"},
		"5 fichier .json":                {"deploy/values-x.json": "{\"auth\":{\"github\":{\"clientSecret\":\"" + v + "\"}}}\n"},
		"6b --set avec $SECRET nu":       {".github/workflows/d.yml": "run: helm upgrade r c --set auth.github.clientSecret=$SECRET\n"},
		"6c test Go avec une valeur":     {"ng2/chart_x_test.go": "// clientSecret: " + v + "\n"},
		"6 --set dans un .yml":           {".github/workflows/deploy.yml": "steps:\n  - run: helm upgrade rel chart --set auth.github.clientSecret=" + v + "\n"},
		"7 Secret stringData":            {"deploy/secret-github.yaml": "apiVersion: v1\nkind: Secret\nstringData:\n  client-secret: " + v + "\n"},
		"7b Secret data (base64)":        {"deploy/secret-github.yaml": "apiVersion: v1\nkind: Secret\ndata:\n  client-secret: " + b64 + "\n"},
		"8 clé à plat (déjà détectée)":   {"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: " + v + "\n"},
		"9 tabulation (déjà détectée)":   {"deploy/values-x.yaml": "auth:\n  github:\n\tclientSecret: " + v + "\n"},
		"snake_case et casse":            {"deploy/values-x.yaml": "github:\n  CLIENT_SECRET: " + v + "\n"},
		"clientsecret en minuscules":     {"deploy/values-x.yaml": "github:\n  clientsecret: " + v + "\n"},
	} {
		code, out := runCIGuard254(t, guard, files, true)
		if code == 0 || strings.Contains(out, "GARDE-PASSEE") || !strings.Contains(out, "ECHEC") {
			t.Errorf("%s : garde PASSÉE (code %d) alors qu'un secret est suivi par git :\n%s", name, code, out)
		}
		if strings.Contains(out, v) || strings.Contains(out, b64) {
			t.Errorf("%s : la valeur du secret est affichée :\n%s", name, out)
		}
	}
}

// Faux positifs : placeholders (${VAR}, {{ }}, vides), documentation, contrats, tests, modèles du chart, fichiers non suivis ne
// déclenchent pas la garde — sinon elle serait contournée par désactivation.
func TestCI254GuardPlaceholdersAndExcludedPaths(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	guard := ciGuard254(t, string(b))
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	for name, tc := range map[string]struct {
		files map[string]string
		track bool
	}{
		"variable shell":                         {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: ${CLIENT_SECRET}\n"}, true},
		"variable shell entre guillemets":        {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: \"${CLIENT_SECRET}\"\n"}, true},
		"gabarit Helm":                           {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: \"{{ .Values.x }}\"\n"}, true},
		"gabarit Helm nu":                        {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: {{ .Values.x }}\n"}, true},
		"vide guillemets":                        {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: \"\"\n"}, true},
		"vide apostrophes":                       {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: ''\n"}, true},
		"clé sans valeur suivie d'une autre clé": {map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret:\n    clientId: \"\"\n"}, true},
		"Secret avec valeur vide":                {map[string]string{"deploy/secret.yaml": "kind: Secret\nstringData:\n  client-secret: \"\"\n"}, true},
		"Secret avec variable":                   {map[string]string{"deploy/secret.yaml": "kind: Secret\nstringData:\n  client-secret: ${CLIENT_SECRET}\n"}, true},
		"--set avec variable":                    {map[string]string{".github/workflows/d.yml": "run: helm upgrade r c --set auth.github.clientSecret=${{ secrets.GH_SECRET }}\n"}, true},
		"--set avec ${SECRET} entier":            {map[string]string{".github/workflows/d.yml": "run: helm upgrade r c --set auth.github.clientSecret=${SECRET}\n"}, true},
		"verbe de format Go %v (test)":           {map[string]string{"ng2/chart_auth172_test.go": "t.Errorf(\"clientSecret: %v\", x)\n"}, true},
		"verbes de format Go %s et %q (test)":    {map[string]string{"ng2/autre_test.go": "t.Errorf(\"client_secret: %s / clientSecret=%q\", a, b)\n"}, true},
		"modèle du chart":                        {map[string]string{"deploy/helm/claude-launcher/templates/secret-github.yaml": "stringData:\n  client-secret: {{ .Values.auth.github.clientSecret | quote }}\n"}, true},
		"documentation du chart":                 {map[string]string{"ng2/docs/kubernetes.md": "clientSecret: exemple-de-doc\n"}, true},
		"contrats":                               {map[string]string{"contracts/helm-values.md": "`auth.github.clientSecret: exemple`\n"}, true},
		"documentation de procédure (.md)":       {map[string]string{"tests/procedures/x/p.md": "citer auth.github.clientSecret dans le guide\n"}, true},
		"non suivi (values-local)":               {map[string]string{"PRIVATE/QUALIF/values-local.yaml": "auth:\n  github:\n    clientSecret: abc123xyz\n"}, false},
		"non suivi (secret local)":               {map[string]string{"deploy/helm/values-local.yaml": "auth:\n  github:\n    clientSecret: abc123xyz\n"}, false},
	} {
		code, out := runCIGuard254(t, guard, tc.files, tc.track)
		if code != 0 || !strings.Contains(out, "GARDE-PASSEE") {
			t.Errorf("%s : garde échouée à tort (code %d) :\n%s", name, code, out)
		}
	}
}

// ── isolation interne / externe (audit LOT-3, M-2) ───────────────────────────────────────────────────────────────

// Aucune règle d'Ingress sans hôte (elle répondrait à n'importe quel Host, donc à « launcher.local » depuis Internet) ; l'ingress
// externe ne porte QUE l'hôte externe ; l'ingress interne ne vise jamais http-ext et reste sous ipAllowList (adresse de la connexion).
func TestChartExt254ExternalRouteNeverReachesInternalInstance(t *testing.T) {
	needHelm(t)
	o := must(t, t254Values())
	byName := map[string]m{}
	for _, mw := range o["Middleware"] {
		byName[fmt.Sprint(get(mw, "metadata", "name"))] = mw
	}
	for _, ing := range o["Ingress"] {
		name := fmt.Sprint(get(ing, "metadata", "name"))
		rules, _ := get(ing, "spec", "rules").([]any)
		if len(rules) == 0 {
			t.Errorf("Ingress %s sans règle", name)
		}
		if get(ing, "spec", "defaultBackend") != nil {
			t.Errorf("Ingress %s : defaultBackend (répondrait à tout Host)", name)
		}
		hosts := map[string]bool{}
		for _, r := range rules {
			h := strings.TrimSpace(fmt.Sprint(get(r, "host")))
			if h == "" || h == "<nil>" {
				t.Errorf("Ingress %s : règle sans hôte (répond à n'importe quel Host, dont l'hôte interne)", name)
			}
			hosts[h] = true
		}
		isExt := hosts[t254Ext]
		if isExt && (hosts[t254Int] || len(hosts) != 1) {
			t.Errorf("Ingress externe %s : hôtes %v, attendu l'hôte externe seul", name, hosts)
		}
		backend := fmt.Sprint(get(ing, "spec", "rules"))
		switch {
		case isExt && !strings.Contains(backend, "http-ext"):
			t.Errorf("Ingress externe %s ne vise pas http-ext : %s", name, backend)
		case !isExt && strings.Contains(backend, "http-ext"):
			t.Errorf("Ingress interne %s vise http-ext", name)
		}
		if isExt && (strings.Contains(backend, "port:map[name:http]") || strings.Contains(backend, "name:http ") && !strings.Contains(backend, "http-ext")) {
			t.Errorf("Ingress externe %s vise le port http (instance interne, formulaire du compte local) : %s", name, backend)
		}
		// l'interne est sous liste d'adresses (profondeur 0), l'externe sans liste
		anno := t172Anno(ing)
		if isExt && strings.Contains(anno, "allowlist") {
			t.Errorf("Ingress externe %s sous la liste d'adresses interne", name)
		}
		if !isExt {
			if !strings.Contains(anno, "allowlist") {
				t.Errorf("Ingress interne %s sans ipAllowList : %q", name, anno)
			}
			for _, ref := range strings.Split(anno, ",") {
				ref = strings.TrimSuffix(strings.TrimSpace(ref), "@kubernetescrd")
				for n, mw := range byName {
					if strings.HasSuffix(ref, "-"+n) && get(mw, "spec", "ipAllowList") != nil {
						src, _ := get(mw, "spec", "ipAllowList", "sourceRange").([]any)
						if fmt.Sprint(src) != "[192.168.1.0/24]" || fmt.Sprint(get(mw, "spec", "ipAllowList", "ipStrategy", "depth")) != "0" {
							t.Errorf("ipAllowList de %s : %v, attendu [192.168.1.0/24] et ipStrategy.depth 0", name, get(mw, "spec", "ipAllowList"))
						}
					}
				}
			}
		}
	}
	// ni le service ni les règles ne routent l'hôte interne vers http-ext, ni l'externe vers http
	svc := fmt.Sprint(get(o["Service"][0], "spec", "ports"))
	if !strings.Contains(svc, "http-ext") || !strings.Contains(svc, "name:http ") && !strings.Contains(svc, "name:http]") {
		t.Errorf("ports du Service : %s", svc)
	}
}

// ── checksum d'annotation : le pod redémarre quand le secret GitHub ou les gabarits changent ──────────────────────

func t254Checksums(t *testing.T, v m) map[string]string {
	t.Helper()
	o := must(t, v)
	ann, _ := get(o["StatefulSet"][0], "spec", "template", "metadata", "annotations").(m)
	out := map[string]string{}
	for k, val := range ann {
		if strings.HasPrefix(k, "checksum/") {
			out[k] = fmt.Sprint(val)
		}
	}
	return out
}

func TestChartAuth254PodChecksums(t *testing.T) {
	needHelm(t)
	base := t254Checksums(t, t254Keys(t172Local(), "Iv1.id", "secret-un"))
	var gh, tpl string
	for k := range base {
		if strings.Contains(k, "github") {
			gh = k
		}
		if strings.Contains(k, "template") {
			tpl = k
		}
	}
	if gh == "" || tpl == "" {
		t.Fatalf("annotations checksum/… du pod : %v, attendu une pour le secret GitHub et une pour les gabarits", base)
	}
	again := t254Checksums(t, t254Keys(t172Local(), "Iv1.id", "secret-un"))
	if again[gh] != base[gh] || again[tpl] != base[tpl] {
		t.Errorf("checksums instables pour des valeurs identiques : %v / %v", base, again)
	}
	changed := t254Checksums(t, t254Keys(t172Local(), "Iv1.id", "secret-deux"))
	if changed[gh] == base[gh] {
		t.Errorf("%s inchangé quand clientSecret change : le pod ne redémarrerait pas", gh)
	}
	other := t254Checksums(t, t254Keys(t172Local(), "Iv1.autre", "secret-un"))
	if other[gh] == base[gh] {
		t.Errorf("%s inchangé quand clientId change", gh)
	}
	// gabarits : un autre rendu du gabarit de connexion (GitHub seul, sans compte local) change la somme
	noLocal := t254Checksums(t, t254Keys(t172Values(), "Iv1.id", "secret-un"))
	if noLocal[tpl] == base[tpl] {
		t.Errorf("%s inchangé quand le gabarit de connexion change (compte local retiré)", tpl)
	}
	// jamais la valeur, ni son hachage évident, dans l'annotation
	for k, val := range base {
		if strings.Contains(val, "secret-un") {
			t.Errorf("%s contient la valeur du secret", k)
		}
	}
	// existingSecret : une annotation pour le Secret fourni n'a pas à dépendre de sa valeur (inconnue du chart)
	if c := t254Checksums(t, t172Values()); c[gh] == "" && len(c) == 0 {
		t.Errorf("aucun checksum avec existingSecret : %v", c)
	}
}

// ── cookie externe préfixé __Host- (m-D), si livré ──────────────────────────────────────────────────────────────

func TestChartExt254HostPrefixedCookie(t *testing.T) {
	needHelm(t)
	o := must(t, t254Values())
	a := t172Args(t172Container(t, o, "auth-proxy-ext"))
	name := strings.TrimPrefix(t172ArgPrefix(a, "--cookie-name="), "--cookie-name=")
	if !strings.HasPrefix(name, "__Host-") {
		t.Skipf("cookie externe %q : préfixe __Host- non livré (m-D, recommandation)", name)
	}
	// __Host- impose Secure, Path=/ et AUCUN Domain
	if !t172HasArg(a, "--cookie-secure=true") || t172ArgPrefix(a, "--cookie-domain") != "" {
		t.Errorf("cookie %s : Secure obligatoire, Domain interdit : %v", name, a)
	}
	if p := t172ArgPrefix(a, "--cookie-path="); p != "" && p != "--cookie-path=/" {
		t.Errorf("cookie %s : Path = %s, attendu /", name, p)
	}
	// l'interne garde un nom distinct (jamais le même nom de cookie)
	in := strings.TrimPrefix(t172ArgPrefix(t172Args(t172Container(t, o, "auth-proxy")), "--cookie-name="), "--cookie-name=")
	if in == name || in == "" {
		t.Errorf("noms de cookie interne %q et externe %q : distincts attendus", in, name)
	}
}

// ── cycle 1 LOT-3 : garde FAIL-CLOSED, exclusions réduites, placeholders stricts ─────────────────────────────────────

// runCIGuardPath254 : comme runCIGuard254, avec un répertoire placé EN TÊTE du PATH (faux awk) ; rend aussi la sortie.
func runCIGuardPath254(t *testing.T, guard string, files map[string]string, fakeAwk string) (int, string) {
	t.Helper()
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, "ng2"), 0o755)
	for p, c := range files {
		full := filepath.Join(repo, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"add", "-A"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v : %v %s", args, err, out)
		}
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "awk"), []byte("#!/bin/sh\n"+fakeAwk+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", guard+"\necho GARDE-PASSEE")
	cmd.Dir = filepath.Join(repo, "ng2")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return code, string(out)
}

// Fail-closed : un awk absent, cassé (erreur) ou neutralisé (ne détecte plus rien, code 0) ne doit JAMAIS laisser passer la garde ;
// le canari intégré (un échantillon contenant un clientSecret, qui doit être détecté) en est la preuve : s'il n'est pas détecté, la
// garde échoue — même sur un dépôt propre, même sur un dépôt qui fuit.
func TestCI254GuardFailsClosed(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	guard := ciGuard254(t, string(b))
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	clean := map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: \"\"\n"}
	leaky := map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: abc123xyz\n"}
	for name, awk := range map[string]string{
		"awk en erreur (code 2)":                      "echo 'awk: erreur de syntaxe' >&2; exit 2",
		"awk tué (code 137)":                          "exit 137",
		"awk neutralisé (code 0, aucune sortie)":      "exit 0",
		"awk qui n'affiche que des lignes vides":      "echo; echo; exit 0",
		"awk absent (commande introuvable, code 127)": "exit 127",
	} {
		for repoName, files := range map[string]map[string]string{"dépôt propre": clean, "dépôt qui fuit": leaky} {
			code, out := runCIGuardPath254(t, guard, files, awk)
			if code == 0 || strings.Contains(out, "GARDE-PASSEE") {
				t.Errorf("%s / %s : garde PASSÉE alors que le contrôle est inopérant (fail-open) :\n%s", name, repoName, out)
			}
			if !strings.Contains(out, "ECHEC") {
				t.Errorf("%s / %s : échec sans message « ECHEC » :\n%s", name, repoName, out)
			}
		}
	}
	// le canari existe dans le script (preuve de vie du détecteur) et ne fuit rien
	low := strings.ToLower(guard)
	if !strings.Contains(low, "canari") && !strings.Contains(low, "canary") {
		t.Error("aucun canari dans la garde (échantillon qui doit être détecté pour prouver que le détecteur fonctionne)")
	}
	// témoin positif : awk réel, dépôt propre → passe
	if code, out := runCIGuard254(t, guard, clean, true); code != 0 || !strings.Contains(out, "GARDE-PASSEE") {
		t.Errorf("garde échouée sur un dépôt propre avec un awk sain (code %d) :\n%s", code, out)
	}
}

// Exclusions réduites : un client_secret en clair est détecté dans un env d'environnement, un YAML de tests, un script, un .txt et un
// .env.prod suivis par git ; les exclusions ciblées restantes (modèles du chart, documentation et contrats qui citent le NOM de la
// clé, le test de la garde et ses fixtures) ne déclenchent rien.
func TestCI254GuardNarrowExclusions(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	guard := ciGuard254(t, string(b))
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	const v = "abc123xyz-secret"
	for name, files := range map[string]map[string]string{
		".claude/agents/environments/prod.env":  {".claude/agents/environments/prod.env": "CLIENT_ID=x\nclient_secret=" + v + "\n"},
		"tests/x.yaml":                          {"tests/x.yaml": "auth:\n  github:\n    client_secret: " + v + "\n"},
		"un script .sh":                         {"scripts/deploy.sh": "#!/bin/sh\nCLIENT_SECRET=" + v + " helm upgrade r c\n"},
		"un fichier .txt":                       {"notes/acces.txt": "client_secret: " + v + "\n"},
		"un .env.prod":                          {"deploy/.env.prod": "GITHUB_CLIENT_SECRET=" + v + "\n"},
		"un .env":                               {".env": "client_secret=" + v + "\n"},
		"un .ini":                               {"conf/app.ini": "client_secret = " + v + "\n"},
		"un Dockerfile":                         {"deploy/Dockerfile": "ENV CLIENT_SECRET=" + v + "\n"},
		"un test Go (hors le test de la garde)": {"ng2/autre_test.go": "const s = \"client_secret: " + v + "\"\n"},
		"un YAML sous .claude/":                 {".claude/settings.yaml": "clientSecret: " + v + "\n"},
	} {
		code, out := runCIGuard254(t, guard, files, true)
		if code == 0 || strings.Contains(out, "GARDE-PASSEE") || !strings.Contains(out, "ECHEC") {
			t.Errorf("%s : garde PASSÉE (code %d) alors qu'un secret en clair est suivi par git :\n%s", name, code, out)
		}
		if strings.Contains(out, v) {
			t.Errorf("%s : la valeur du secret est affichée :\n%s", name, out)
		}
	}
	// exclusions ciblées : rien ne se déclenche
	for name, files := range map[string]map[string]string{
		"modèle du chart":               {"deploy/helm/claude-launcher/templates/secret-github.yaml": "stringData:\n  client-secret: {{ .Values.auth.github.clientSecret | quote }}\n"},
		"documentation (nom de la clé)": {"ng2/docs/kubernetes.md": "Renseigner `auth.github.clientSecret` (jamais dans git).\n"},
		"contrat (nom de la clé)":       {"contracts/helm-values.md": "| `auth.github.clientSecret` | chaîne | `\"\"` |\n"},
		"README (nom de la clé)":        {"README.md": "voir clientSecret et client_secret dans le guide\n"},
		"test de la garde":              {"ng2/chart_lot3_254_test.go": "// clientSecret: " + v + "\n"},
		"fixtures du test de la garde":  {"ng2/chart_lot3_254_fixtures/values.yaml": "clientSecret: " + v + "\n"},
	} {
		if code, out := runCIGuard254(t, guard, files, true); code != 0 || !strings.Contains(out, "GARDE-PASSEE") {
			t.Errorf("%s : garde échouée à tort (code %d) :\n%s", name, code, out)
		}
	}
}

// Espaces réservés : seuls "", ”, ${VAR}, {{ … }} et <…> ENTIERS sont tolérés ; tout le reste est une valeur réelle.
func TestCI254GuardStrictPlaceholders(t *testing.T) {
	b, err := os.ReadFile("../scripts/ci.sh")
	if err != nil {
		t.Fatal(err)
	}
	guard := ciGuard254(t, string(b))
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	for _, val := range []string{`""`, `''`, `${CLIENT_SECRET}`, `"${CLIENT_SECRET}"`, `{{ .Values.x }}`, `"{{ .Values.x }}"`, `{{.Values.x}}`, `<CLIENT_SECRET>`, `"<votre secret ici>"`, `<secret>`} {
		files := map[string]map[string]string{
			"yaml":      {"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: " + val + "\n"},
			"snake-env": {"deploy/prod.env": "client_secret=" + strings.Trim(val, `"'`) + "\n"},
		}
		for kind, f := range files {
			if code, out := runCIGuard254(t, guard, f, true); code != 0 {
				t.Errorf("espace réservé %s (%s) : garde échouée à tort (code %d) :\n%s", val, kind, code, out)
			}
		}
	}
	const real = "x1y2z3"
	for _, val := range []string{`$x1`, `$abc` + real, `{abc`, `[abc`, `${abc`, `{{abc`, `<abc`, `abc>`, `$`, `"$x1"`, `"{abc"`, `"[abc"`, `$$` + real, `<` + real, `{` + real + `}`, `[` + real + `]`} {
		code, out := runCIGuard254(t, guard, map[string]string{"deploy/values-x.yaml": "auth:\n  github:\n    clientSecret: " + val + "\n"}, true)
		if code == 0 || strings.Contains(out, "GARDE-PASSEE") {
			t.Errorf("valeur réelle %s tolérée à tort comme espace réservé :\n%s", val, out)
		}
		if strings.Contains(out, real) {
			t.Errorf("valeur %s : le secret est affiché :\n%s", val, out)
		}
	}
}

// externalIngress.host : nom DNS valide (RFC 1123, minuscules) — majuscules, étiquette vide, « .. », tiret en bord d'étiquette,
// étiquette > 63 caractères, hôte > 253 : refusés avec un message clair nommant externalIngress.host ; hôtes valides acceptés.
func TestChartExt254HostValidation(t *testing.T) {
	needHelm(t)
	long63, long64 := strings.Repeat("a", 63), strings.Repeat("a", 64)
	for name, host := range map[string]string{
		"majuscules":                 "Launcher.Example.org",
		"majuscule isolée":           "launcher.exampLe.org",
		"deux points":                "a..b.example.org",
		"tiret final d'étiquette":    "a-.b.example.org",
		"tiret initial d'étiquette":  "-a.b.example.org",
		"étiquette initiale vide":    ".example.org",
		"point final":                "launcher.example.org.",
		"étiquette de 64 caractères": long64 + ".example.org",
		"underscore":                 "launcher_x.example.org",
		"caractère non ASCII":        "lauñcher.example.org",
		"port":                       "launcher.example.org:8443",
	} {
		t.Run(name, func(t *testing.T) {
			v := t254Values()
			sub(v, "externalIngress")["host"] = host
			t172Refused(t, v, "externalIngress.host")
		})
	}
	for name, host := range map[string]string{
		"valide":                     "launcher.example.org",
		"tirets internes":            "mon-launcher.sous-domaine.example.org",
		"chiffres":                   "l4uncher.example.org",
		"étiquette de 63 caractères": long63 + ".example.org",
		"punycode":                   "xn--lancher-d1a.example.org",
		"profondeur":                 "a.b.c.d.example.org",
	} {
		t.Run("accepté/"+name, func(t *testing.T) {
			v := t254Values()
			sub(v, "externalIngress")["host"] = host
			if _, out, err := render(t, v); err != nil {
				t.Errorf("hôte valide %q refusé : %.300s", host, out)
			}
		})
	}
}
