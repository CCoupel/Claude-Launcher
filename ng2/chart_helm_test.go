package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// LOT-4.2 (#170, #173) — chart Helm du launcher : lint, rendu contrôlé, refus des expositions, liste d'adresses,
// TLS obligatoire, variables et contexte de sécurité du pod. Écrit depuis contracts/helm-values.md (TDD). Aucune
// installation dans un cluster : `helm lint` et `helm template` seulement. Ignoré si helm est absent.
// Lot : tests/integration/helm/ (voir son README).

const chartDir = "../deploy/helm/claude-launcher"

type m = map[string]any

func needHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm absent")
	}
	if _, err := os.Stat(chartDir); err != nil {
		t.Fatalf("chart absent (%s) : %v", chartDir, err)
	}
}

// baseValues : valeurs minimales valides (Traefik, ClusterIssuer).
func baseValues() m {
	return m{
		"rootBase": "/data/p",
		"volume":   m{"existingClaim": "claude-projets"},
		"ingress": m{
			"controller":          "traefik",
			"className":           "traefik",
			"host":                "launcher-test.coupel.lan",
			"allowedSourceRanges": []any{"192.168.1.0/24"},
			"tls":                 m{"clusterIssuer": "interne"},
		},
	}
}

func clone(v m) m {
	b, _ := yaml.Marshal(v)
	var out m
	_ = yaml.Unmarshal(b, &out)
	return out
}

func sub(v m, path ...string) m {
	for _, k := range path {
		n, ok := v[k].(m)
		if !ok {
			n = m{}
			v[k] = n
		}
		v = n
	}
	return v
}

func writeVals(t *testing.T, v m) string {
	t.Helper()
	b, _ := yaml.Marshal(v)
	p := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func helm(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	c := exec.Command("helm", args...)
	var so, se bytes.Buffer
	c.Stdout, c.Stderr = &so, &se
	c.Env = append(os.Environ(), "HELM_CACHE_HOME="+t.TempDir(), "HELM_CONFIG_HOME="+t.TempDir(), "HELM_DATA_HOME="+t.TempDir())
	err := c.Run()
	return so.String(), se.String(), err
}

// render : helm template de la release « rel » avec les valeurs v ; rend les objets par genre.
func render(t *testing.T, v m, extra ...string) (map[string][]m, string, error) {
	t.Helper()
	args := append([]string{"template", "rel", chartDir, "-f", writeVals(t, v)}, extra...)
	out, errOut, err := helm(t, args...)
	objs := map[string][]m{}
	if err != nil {
		return objs, errOut + out, err
	}
	d := yaml.NewDecoder(strings.NewReader(out))
	for {
		var o m
		if e := d.Decode(&o); e == io.EOF {
			break
		} else if e != nil {
			t.Fatalf("rendu illisible : %v\n%.300s", e, out)
		}
		if o == nil {
			continue
		}
		k, _ := o["kind"].(string)
		objs[k] = append(objs[k], o)
	}
	return objs, out, nil
}

func must(t *testing.T, v m, extra ...string) map[string][]m {
	t.Helper()
	o, out, err := render(t, v, extra...)
	if err != nil {
		t.Fatalf("helm template : %v\n%s", err, out)
	}
	return o
}

func get(o any, path ...string) any {
	for _, k := range path {
		switch x := o.(type) {
		case m:
			o = x[k]
		default:
			return nil
		}
	}
	return o
}

func pod(t *testing.T, o map[string][]m) m {
	t.Helper()
	ss := o["StatefulSet"]
	if len(ss) != 1 {
		t.Fatalf("%d StatefulSet, attendu 1", len(ss))
	}
	return get(ss[0], "spec", "template", "spec").(m)
}

func env(t *testing.T, o map[string][]m) map[string]string {
	t.Helper()
	cs, _ := pod(t, o)["containers"].([]any)
	if len(cs) == 0 {
		t.Fatal("aucun conteneur")
	}
	out := map[string]string{}
	for _, e := range get(cs[0], "env").([]any) {
		em := e.(m)
		if v, ok := em["value"].(string); ok {
			out[em["name"].(string)] = v
		} else {
			out[em["name"].(string)] = "<valueFrom>"
		}
	}
	return out
}

// ── lint ─────────────────────────────────────────────────────────────────────

func TestChartLint(t *testing.T) {
	needHelm(t)
	out, errOut, err := helm(t, "lint", chartDir, "--strict", "-f", writeVals(t, baseValues()))
	if err != nil {
		t.Errorf("helm lint --strict : %v\n%s%s", err, out, errOut)
	}
}

// ── valeurs obligatoires nommées ─────────────────────────────────────────────

func TestChartRequiredValuesAreNamed(t *testing.T) {
	needHelm(t)
	must(t, baseValues()) // le cas valide doit rendre
	for _, tc := range []struct {
		name string
		mod  func(v m)
		want string
	}{
		{"rootBase", func(v m) { delete(v, "rootBase") }, "rootBase"},
		{"volume.existingClaim", func(v m) { delete(sub(v, "volume"), "existingClaim") }, "existingClaim"},
		{"ingress.controller", func(v m) { delete(sub(v, "ingress"), "controller") }, "controller"},
		{"ingress.className", func(v m) { delete(sub(v, "ingress"), "className") }, "className"},
		{"ingress.host", func(v m) { delete(sub(v, "ingress"), "host") }, "host"},
		{"ingress.allowedSourceRanges", func(v m) { delete(sub(v, "ingress"), "allowedSourceRanges") }, "allowedSourceRanges"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := clone(baseValues())
			tc.mod(v)
			_, out, err := render(t, v)
			if err == nil {
				t.Fatalf("rendu accepté sans %s", tc.name)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("le message n'identifie pas la valeur manquante (%s) :\n%s", tc.want, out)
			}
		})
	}
}

// ── objets créés et refusés ──────────────────────────────────────────────────

func TestChartObjectsAndNoClusterWideRights(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	for _, k := range []string{"ServiceAccount", "Role", "RoleBinding", "StatefulSet", "Service", "Ingress", "Middleware", "Certificate"} {
		if len(o[k]) == 0 {
			t.Errorf("objet %s absent du rendu", k)
		}
	}
	for _, k := range []string{"ClusterRole", "ClusterRoleBinding", "NetworkPolicy", "PersistentVolumeClaim", "Deployment", "Namespace"} {
		if len(o[k]) != 0 {
			t.Errorf("objet %s créé (refusé par le contrat)", k)
		}
	}
	// Secret du jeton de l'interface : non créé (risque accepté)
	for _, s := range o["Secret"] {
		t.Errorf("Secret créé : %v (le jeton reste dans le cache, sur le volume)", get(s, "metadata", "name"))
	}
}

func TestChartRoleLimitedToLauncherRights(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	allowed := map[string]bool{"statefulsets": true, "statefulsets/scale": true, "pods": true, "pods/exec": true, "events": true, "persistentvolumeclaims": true}
	if len(o["Role"]) != 1 {
		t.Fatalf("%d Role, attendu 1", len(o["Role"]))
	}
	for _, r := range get(o["Role"][0], "rules").([]any) {
		rm := r.(m)
		for _, res := range rm["resources"].([]any) {
			if !allowed[res.(string)] {
				t.Errorf("ressource %v hors du périmètre du launcher", res)
			}
		}
		for _, grp := range []string{"resources", "verbs", "apiGroups"} {
			for _, x := range rm[grp].([]any) {
				if x == "*" {
					t.Errorf("joker « * » dans %s d'une règle : %v", grp, rm)
				}
			}
		}
	}
	rb := o["RoleBinding"][0]
	if get(rb, "roleRef", "kind") != "Role" {
		t.Errorf("RoleBinding vers %v, attendu Role (jamais ClusterRole)", get(rb, "roleRef", "kind"))
	}
}

// ── StatefulSet : 1 réplique, mise à jour, contexte de sécurité ──────────────

func TestChartStatefulSetSingleReplicaAndStrategy(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	ss := o["StatefulSet"][0]
	if r := get(ss, "spec", "replicas"); r != 1 {
		t.Errorf("replicas = %v, attendu 1", r)
	}
	if st := get(ss, "spec", "updateStrategy", "type"); st == "OnDelete" {
		t.Errorf("updateStrategy OnDelete : un changement d'image ne remplacerait jamais le pod")
	}
	if len(o["Deployment"]) != 0 {
		t.Errorf("Deployment créé : un RollingUpdate démarrerait le nouveau pod avant l'arrêt de l'ancien (garde de présence CL_HUB_NAME)")
	}
	if get(pod(t, o), "terminationGracePeriodSeconds") == nil {
		t.Errorf("terminationGracePeriodSeconds absent")
	}
	// aucune valeur ne règle le nombre de répliques : soit la valeur inconnue est REFUSÉE (schéma strict), soit elle
	// est ignorée et le StatefulSet garde 1 réplique — jamais plus
	for _, set := range []string{"replicas=3", "replicaCount=3"} {
		o2, _, err := render(t, baseValues(), "--set", set)
		if err == nil {
			if r := get(o2["StatefulSet"][0], "spec", "replicas"); r != 1 {
				t.Errorf("--set %s : replicas = %v, attendu 1", set, r)
			}
		}
	}
}

func TestChartPodSecurityContext(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	p := pod(t, o)
	sc, _ := p["securityContext"].(m)
	if sc["runAsUser"] != 1000 || sc["runAsGroup"] != 1000 || sc["runAsNonRoot"] != true {
		t.Errorf("securityContext du pod : %v, attendu 1000:1000 non root", sc)
	}
	if _, has := sc["fsGroup"]; has {
		t.Errorf("fsGroup posé (changement de propriétaire récursif sur le partage) : %v", sc)
	}
	c := p["containers"].([]any)[0].(m)
	csc, _ := c["securityContext"].(m)
	if csc["allowPrivilegeEscalation"] != false {
		t.Errorf("allowPrivilegeEscalation : %v", csc["allowPrivilegeEscalation"])
	}
	if drop, _ := get(csc, "capabilities", "drop").([]any); len(drop) == 0 || drop[0] != "ALL" {
		t.Errorf("capacités non retirées : %v", csc["capabilities"])
	}
	if csc["privileged"] == true {
		t.Errorf("conteneur privilégié")
	}
	for _, k := range []string{"hostNetwork", "hostPID", "hostIPC"} {
		if p[k] == true {
			t.Errorf("%s activé", k)
		}
	}
	// uid/gid remplaçables
	v := baseValues()
	v["runAsUser"], v["runAsGroup"] = 2000, 2001
	sc2 := get(pod(t, must(t, v)), "securityContext").(m)
	if sc2["runAsUser"] != 2000 || sc2["runAsGroup"] != 2001 {
		t.Errorf("runAsUser/runAsGroup non pris en compte : %v", sc2)
	}
}

func TestChartProbesVolumesAndPort(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	c := pod(t, o)["containers"].([]any)[0].(m)
	if p := get(c, "livenessProbe", "httpGet", "path"); p != "/healthz" {
		t.Errorf("sonde de vie : %v, attendu /healthz", p)
	}
	if p := get(c, "readinessProbe", "httpGet", "path"); p != "/readyz" {
		t.Errorf("sonde de disponibilité : %v, attendu /readyz", p)
	}
	mounts := map[string]string{}
	for _, x := range c["volumeMounts"].([]any) {
		mounts[x.(m)["mountPath"].(string)] = x.(m)["name"].(string)
	}
	for _, mp := range []string{"/data/p", "/home/launcher", "/tmp"} {
		if mounts[mp] == "" {
			t.Errorf("point de montage %s absent : %v", mp, mounts)
		}
	}
	claim, empty := false, 0
	for _, x := range pod(t, o)["volumes"].([]any) {
		vm := x.(m)
		if get(vm, "persistentVolumeClaim", "claimName") == "claude-projets" {
			claim = true
		}
		if vm["emptyDir"] != nil {
			empty++
		}
	}
	if !claim || empty < 2 {
		t.Errorf("volumes : PVC existant %v, emptyDir %d (attendu le PVC et 2 emptyDir)", claim, empty)
	}
}

// ── Service : ClusterIP seulement ────────────────────────────────────────────

func TestChartServiceIsAlwaysClusterIP(t *testing.T) {
	needHelm(t)
	if typ := get(must(t, baseValues())["Service"][0], "spec", "type"); typ != "ClusterIP" {
		t.Errorf("Service %v, attendu ClusterIP", typ)
	}
	for _, typ := range []string{"NodePort", "LoadBalancer"} {
		o, _, err := render(t, baseValues(), "--set", "service.type="+typ)
		if err == nil {
			if got := get(o["Service"][0], "spec", "type"); got != "ClusterIP" {
				t.Errorf("service.type=%s accepté et rendu %v", typ, got)
			}
		}
	}
	// aucun NodePort ni loadBalancer dans le rendu
	_, out, _ := render(t, baseValues())
	for _, bad := range []string{"nodePort", "loadBalancerIP", "externalIPs"} {
		if strings.Contains(out, bad) {
			t.Errorf("« %s » dans le rendu", bad)
		}
	}
}

// ── liste d'adresses : Traefik ET nginx, obligatoire, 0.0.0.0/0 refusé ───────

func TestChartAllowListTraefik(t *testing.T) {
	needHelm(t)
	v := baseValues()
	sub(v, "ingress")["allowedSourceRanges"] = []any{"192.168.1.0/24", "10.8.0.0/16"}
	o := must(t, v)
	// Middlewares : exactement UN ipAllowList (la liste d'adresses) ; les autres ne sont que la redirection
	// HTTP → HTTPS du contrat (redirectScheme), jamais une seconde liste ni un autre filtre
	var allow m
	for _, mw := range o["Middleware"] {
		switch {
		case get(mw, "spec", "ipAllowList") != nil:
			if allow != nil {
				t.Fatalf("deux Middleware ipAllowList")
			}
			allow = mw
		case get(mw, "spec", "redirectScheme") != nil:
			if get(mw, "spec", "redirectScheme", "scheme") != "https" {
				t.Errorf("redirection vers %v, attendu https", get(mw, "spec", "redirectScheme", "scheme"))
			}
		default:
			t.Errorf("Middleware inattendu : %v", get(mw, "spec"))
		}
	}
	if allow == nil {
		t.Fatalf("aucun Middleware ipAllowList parmi %d Middleware", len(o["Middleware"]))
	}
	rng, _ := get(allow, "spec", "ipAllowList", "sourceRange").([]any)
	if len(rng) != 2 || rng[0] != "192.168.1.0/24" || rng[1] != "10.8.0.0/16" {
		t.Errorf("ipAllowList.sourceRange = %v", rng)
	}
	ann, _ := get(o["Ingress"][0], "metadata", "annotations").(m)
	ref, _ := ann["traefik.ingress.kubernetes.io/router.middlewares"].(string)
	name, _ := get(allow, "metadata", "name").(string)
	if !strings.Contains(ref, name) {
		t.Errorf("l'Ingress ne référence pas le Middleware ipAllowList %q : %q", name, ref)
	}
	// l'ipAllowList s'applique avant toute redirection : il est référencé (jamais contourné par un routeur sans lui)
	for _, mw := range o["Middleware"] {
		if n, _ := get(mw, "metadata", "name").(string); get(mw, "spec", "redirectScheme") != nil && strings.Contains(ref, n) && strings.Index(ref, n) < strings.Index(ref, name) {
			t.Errorf("la redirection précède la liste d'adresses dans router.middlewares : %q", ref)
		}
	}
}

func TestChartAllowListNginx(t *testing.T) {
	needHelm(t)
	v := baseValues()
	ing := sub(v, "ingress")
	ing["controller"], ing["className"] = "nginx", "nginx"
	ing["allowedSourceRanges"] = []any{"192.168.1.0/24", "10.8.0.0/16"}
	o := must(t, v)
	if len(o["Middleware"]) != 0 {
		t.Errorf("Middleware Traefik créé avec le contrôleur nginx")
	}
	ann, _ := get(o["Ingress"][0], "metadata", "annotations").(m)
	if got := ann["nginx.ingress.kubernetes.io/whitelist-source-range"]; got != "192.168.1.0/24,10.8.0.0/16" {
		t.Errorf("whitelist-source-range = %v", got)
	}
	if ann["nginx.ingress.kubernetes.io/proxy-buffering"] != "off" {
		t.Errorf("proxy-buffering = %v, attendu off (flux d'évènements)", ann["nginx.ingress.kubernetes.io/proxy-buffering"])
	}
	for _, k := range []string{"proxy-read-timeout", "proxy-send-timeout"} {
		if ann["nginx.ingress.kubernetes.io/"+k] != "3600" {
			t.Errorf("%s = %v, attendu 3600", k, ann["nginx.ingress.kubernetes.io/"+k])
		}
	}
}

func TestChartAllowListMandatoryAndNeverOpenToAll(t *testing.T) {
	needHelm(t)
	must(t, baseValues()) // sans cela un chart qui ne rend jamais « refuserait » tout
	for _, ctl := range []string{"traefik", "nginx"} {
		for _, tc := range []struct {
			name   string
			ranges any
		}{
			{"liste vide", []any{}},
			{"0.0.0.0/0", []any{"0.0.0.0/0"}},
			{"::/0", []any{"::/0"}},
			{"0.0.0.0/0 parmi d'autres", []any{"192.168.1.0/24", "0.0.0.0/0"}},
			{"CIDR invalide", []any{"pas-un-cidr"}},
		} {
			t.Run(ctl+"/"+tc.name, func(t *testing.T) {
				v := baseValues()
				ing := sub(v, "ingress")
				ing["controller"], ing["className"] = ctl, ctl
				ing["allowedSourceRanges"] = tc.ranges
				if _, out, err := render(t, v); err == nil {
					t.Errorf("accepté : %v\n%.200s", tc.ranges, out)
				}
			})
		}
	}
}

// ── TLS obligatoire ──────────────────────────────────────────────────────────

func TestChartTLSMandatory(t *testing.T) {
	needHelm(t)
	must(t, baseValues()) // le cas valide rend : seul le TLS manquant doit faire échouer
	// ni Secret fourni ni émetteur : refusé
	v := baseValues()
	delete(sub(v, "ingress"), "tls")
	if _, out, err := render(t, v); err == nil {
		t.Errorf("Ingress sans TLS accepté\n%.200s", out)
	} else if !strings.Contains(strings.ToLower(out), "tls") {
		t.Errorf("le message ne nomme pas TLS : %s", out)
	}
	// émetteur : Certificate + Secret <release>-tls
	o := must(t, baseValues())
	if len(o["Certificate"]) != 1 {
		t.Fatalf("%d Certificate", len(o["Certificate"]))
	}
	if get(o["Certificate"][0], "spec", "issuerRef", "name") != "interne" || get(o["Certificate"][0], "spec", "secretName") != "rel-tls" {
		t.Errorf("Certificate : %v", get(o["Certificate"][0], "spec"))
	}
	tls := get(o["Ingress"][0], "spec", "tls").([]any)
	if len(tls) != 1 || get(tls[0], "secretName") != "rel-tls" {
		t.Errorf("Ingress.tls = %v", tls)
	}
	if hs, _ := get(tls[0], "hosts").([]any); len(hs) != 1 || hs[0] != "launcher-test.coupel.lan" {
		t.Errorf("hôtes TLS = %v", hs)
	}
	// Secret fourni : pas de Certificate
	v2 := baseValues()
	sub(v2, "ingress")["tls"] = m{"secretName": "mon-secret-tls"}
	o2 := must(t, v2)
	if len(o2["Certificate"]) != 0 {
		t.Errorf("Certificate créé avec un Secret fourni")
	}
	if get(get(o2["Ingress"][0], "spec", "tls").([]any)[0], "secretName") != "mon-secret-tls" {
		t.Errorf("Secret TLS fourni non utilisé")
	}
}

// ── Ingress désactivé ────────────────────────────────────────────────────────

func TestChartIngressDisabled(t *testing.T) {
	needHelm(t)
	v := baseValues()
	v["ingress"] = m{"enabled": false}
	o := must(t, v)
	for _, k := range []string{"Ingress", "Middleware", "Certificate"} {
		if len(o[k]) != 0 {
			t.Errorf("%s créé avec ingress.enabled=false", k)
		}
	}
	if _, has := env(t, o)["CL_WEB_URL"]; has {
		t.Errorf("CL_WEB_URL posée sans Ingress")
	}
}

// ── variables du pod ─────────────────────────────────────────────────────────

func TestChartPodEnvironment(t *testing.T) {
	needHelm(t)
	e := env(t, must(t, baseValues()))
	want := map[string]string{
		"CL_ROOT_BASE":   "/data/p",
		"CL_HUB_NAME":    "rel",
		"CL_CACHE_DIR":   "/data/p/.cache/claude-launcher-ng2/rel",
		"CL_K8S_VOLUMES": "/data/p=claude-projets",
		"CL_WEB_URL":     "https://launcher-test.coupel.lan",
		"CL_UPDATE_MODE": "notify",
	}
	for k, w := range want {
		if e[k] != w {
			t.Errorf("%s = %q, attendu %q", k, e[k], w)
		}
	}
	if _, has := e["CL_ALLOW_HOST_BACKEND"]; has {
		t.Errorf("CL_ALLOW_HOST_BACKEND posée par défaut : %q", e["CL_ALLOW_HOST_BACKEND"])
	}
	if v := e["CL_IN_CLUSTER"]; v != "" && v != "1" {
		t.Errorf("CL_IN_CLUSTER = %q (détection automatique ou 1)", v)
	}
	// hubName, mountPath différent, allowHostBackend
	v := baseValues()
	v["hubName"], v["rootBase"], v["allowHostBackend"] = "cluster-qualif", "/data/p/racine", true
	sub(v, "volume")["mountPath"] = "/data/p"
	e2 := env(t, must(t, v))
	if e2["CL_HUB_NAME"] != "cluster-qualif" || e2["CL_CACHE_DIR"] != "/data/p/racine/.cache/claude-launcher-ng2/cluster-qualif" {
		t.Errorf("nom de hub et cache : %q %q", e2["CL_HUB_NAME"], e2["CL_CACHE_DIR"])
	}
	if e2["CL_K8S_VOLUMES"] != "/data/p=claude-projets" || e2["CL_ALLOW_HOST_BACKEND"] != "1" {
		t.Errorf("volumes / host : %q %q", e2["CL_K8S_VOLUMES"], e2["CL_ALLOW_HOST_BACKEND"])
	}
}

func TestChartRootBaseMustBeUnderMountPath(t *testing.T) {
	needHelm(t)
	must(t, baseValues())
	v := baseValues()
	v["rootBase"] = "/ailleurs"
	sub(v, "volume")["mountPath"] = "/data/p"
	if _, out, err := render(t, v); err == nil {
		t.Errorf("rootBase hors du volume monté accepté\n%.200s", out)
	}
	v2 := baseValues()
	v2["rootBase"] = "relatif/chemin"
	if _, _, err := render(t, v2); err == nil {
		t.Errorf("rootBase relatif accepté (chemin absolu attendu)")
	}
}

// Valeurs par environnement : rendent sans erreur, noms de hub distincts, hôtes dédiés.
func TestChartEnvironmentValuesFiles(t *testing.T) {
	needHelm(t)
	hubs := map[string]string{}
	for _, env := range []string{"qualif", "prod"} {
		f := "../deploy/helm/values-" + env + ".yaml"
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s absent", f)
		}
		out, errOut, err := helm(t, "template", "rel", chartDir, "-f", f)
		if err != nil {
			t.Errorf("%s : %v\n%s", env, err, errOut)
			continue
		}
		if strings.Contains(out, "0.0.0.0/0") {
			t.Errorf("%s : liste ouverte à tous", env)
		}
		var vals m
		b, _ := os.ReadFile(f)
		_ = yaml.Unmarshal(b, &vals)
		hubs[env], _ = vals["hubName"].(string)
		if get(vals, "ingress", "controller") != "traefik" {
			t.Errorf("%s : contrôleur %v, attendu traefik", env, get(vals, "ingress", "controller"))
		}
		if rs, _ := get(vals, "ingress", "allowedSourceRanges").([]any); len(rs) == 0 {
			t.Errorf("%s : liste d'adresses vide", env)
		}
	}
	if hubs["qualif"] == "" || hubs["prod"] == "" || hubs["qualif"] == hubs["prod"] {
		t.Errorf("hubName de QUALIF et de PROD : %q / %q — doivent être renseignés et distincts", hubs["qualif"], hubs["prod"])
	}
}
