package main

import (
	"bytes"
	"fmt"
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
		// v7.0.0 : l'authentification est activée par défaut (P1) ; les tests du chart d'avant l'authentification la coupent
		"auth": m{"enabled": false},
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

// projectPVCs : les PVC rendus hors ancien coffre <release>-secrets (plus rendu depuis #201) — ce sont les
// PVC du volume des projets, seuls visés par volume.create.
func projectPVCs(o map[string][]m) []m {
	var out []m
	for _, p := range o["PersistentVolumeClaim"] {
		if n, _ := get(p, "metadata", "name").(string); !strings.HasSuffix(n, "-secrets") {
			out = append(out, p)
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
	for _, k := range []string{"ClusterRole", "ClusterRoleBinding", "Deployment", "Namespace"} {
		if len(o[k]) != 0 {
			t.Errorf("objet %s créé (refusé par le contrat)", k)
		}
	}
	// NetworkPolicy activée par défaut (#200) : une seule, celle du pod (contrôle détaillé : chart_networkpolicy200_test.go)
	if n := len(o["NetworkPolicy"]); n != 1 {
		t.Errorf("%d NetworkPolicy rendues, attendu 1 (activée par défaut, #200)", n)
	}
	// aucun PVC pour les projets sans volume.create ; le coffre n'a plus de PVC (#201)
	if n := len(projectPVCs(o)); n != 0 {
		t.Errorf("%d PVC de projets créé(s) sans volume.create (refusé par le contrat)", n)
	}
	if n := len(o["PersistentVolumeClaim"]); n != 0 {
		t.Errorf("%d PVC rendus, attendu 0 (plus de PVC <release>-secrets, #201)", n)
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
	sub(v, "web")["access"] = "token" // LOT-4.4 : le défaut « none » exige une adresse publique https, donc un Ingress
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
		out, errOut, err := helm(t, "template", "rel", chartDir, "-f", f, "--set", "ingress.tls.clusterIssuer=interne-test") // l'émetteur « a-renseigner » est refusé par le chart : testé à part
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

// ── cycle 1 (revue + audit sécurité du chart) ────────────────────────────────

// validOK : le cas de référence rend (précondition de tous les refus ci-dessous : aucun refus vacueux).
func validOK(t *testing.T) {
	t.Helper()
	must(t, baseValues())
}

// Liste d'adresses : chaque entrée validée (octets 0–255, masques valides), tout préfixe /0 refusé quelle que soit
// l'écriture, préfixes trop larges refusés (< /8 en IPv4, < /16 en IPv6) ; les plages raisonnables restent admises.
func TestChartAllowListEntriesAreValidatedStrictly(t *testing.T) {
	needHelm(t)
	validOK(t)
	refused := []string{
		"0.0.0.0/00", "10.0.0.0/0", "0::/0", "0:0:0:0:0:0:0:0/0", "::0/0", "::/00", "0.0.0.0/000",
		"1.2.3.4/33", "256.1.1.1/24", "10.0.0.0/-1", "10.0.0/24", "10.0.0.0.0/24", "a.b.c.d/8", "10.0.0.0/",
		"10.0.0.0/7", "128.0.0.0/1", "2001:db8::/15", "::/129", "2001:db8::/-1", "fd00:::1/64",
	}
	accepted := []string{"10.0.0.0/8", "192.168.1.0/24", "192.168.1.7/32", "172.16.0.0/12", "2001:db8::/32", "fd00::/16", "fd00::1/128"}
	for _, ctl := range []string{"traefik", "nginx"} {
		for _, bad := range refused {
			t.Run(ctl+"/refus/"+bad, func(t *testing.T) {
				v := baseValues()
				ing := sub(v, "ingress")
				ing["controller"], ing["className"] = ctl, ctl
				ing["allowedSourceRanges"] = []any{"192.168.1.0/24", bad}
				if _, out, err := render(t, v); err == nil {
					t.Errorf("%q accepté\n%.200s", bad, out)
				}
			})
		}
		for _, ok := range accepted {
			t.Run(ctl+"/admis/"+ok, func(t *testing.T) {
				v := baseValues()
				ing := sub(v, "ingress")
				ing["controller"], ing["className"] = ctl, ctl
				ing["allowedSourceRanges"] = []any{ok}
				if _, out, err := render(t, v); err != nil {
					t.Errorf("%q refusé à tort : %s", ok, out)
				}
			})
		}
	}
}

// rootBase : chemin absolu NORMALISÉ — « .. », « . », « // » et « / » final refusés.
func TestChartRootBaseMustBeNormalised(t *testing.T) {
	needHelm(t)
	v := baseValues()
	v["rootBase"] = "/data/p"
	sub(v, "volume")["mountPath"] = "/data"
	must(t, v)
	for _, bad := range []string{"/data/p/../q", "/data/../data/p", "/data//p", "/data/./p", "/data/p/", "/data/p/.."} {
		t.Run(bad, func(t *testing.T) {
			v := baseValues()
			v["rootBase"] = bad
			sub(v, "volume")["mountPath"] = "/data"
			if _, out, err := render(t, v); err == nil {
				t.Errorf("rootBase %q accepté\n%.200s", bad, out)
			}
		})
	}
}

// Système de fichiers racine en lecture seule (home et tmp sont des emptyDir) ; startupProbe /healthz si présente.
func TestChartReadOnlyRootFilesystem(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	c := pod(t, o)["containers"].([]any)[0].(m)
	if get(c, "securityContext", "readOnlyRootFilesystem") != true {
		t.Errorf("readOnlyRootFilesystem = %v, attendu true", get(c, "securityContext", "readOnlyRootFilesystem"))
	}
	mounted := map[string]bool{}
	for _, x := range c["volumeMounts"].([]any) {
		mounted[x.(m)["mountPath"].(string)] = true
	}
	for _, mp := range []string{"/home/launcher", "/tmp"} {
		if !mounted[mp] {
			t.Errorf("%s non monté (inscriptible) : le conteneur en lecture seule ne pourrait pas écrire", mp)
		}
	}
	if sp, ok := c["startupProbe"]; ok {
		if get(sp, "httpGet", "path") != "/healthz" {
			t.Errorf("startupProbe : %v, attendu httpGet /healthz", sp)
		}
	}
}

// Valeurs réservées : extraEnv ne redéfinit aucune CL_* posée par le chart ; ingress.annotations ne remplace aucune
// clé réservée (liste d'adresses, redirection) — sinon la liste d'adresses pourrait être contournée.
func TestChartReservedValuesAreRefused(t *testing.T) {
	needHelm(t)
	validOK(t)
	for _, name := range []string{"CL_ROOT_BASE", "CL_HUB_NAME", "CL_CACHE_DIR", "CL_K8S_VOLUMES", "CL_WEB_URL", "CL_UPDATE_MODE", "CL_ALLOW_HOST_BACKEND", "CL_IN_CLUSTER"} {
		t.Run("extraEnv/"+name, func(t *testing.T) {
			v := baseValues()
			v["extraEnv"] = []any{m{"name": name, "value": "x"}}
			if _, out, err := render(t, v); err == nil {
				t.Errorf("extraEnv %s accepté\n%.200s", name, out)
			} else if !strings.Contains(out, name) {
				t.Errorf("le message ne nomme pas %s : %s", name, out)
			}
		})
	}
	v := baseValues()
	v["extraEnv"] = []any{m{"name": "TW_AUTRE", "value": "ok"}}
	if e := env(t, must(t, v)); e["TW_AUTRE"] != "ok" {
		t.Errorf("extraEnv inoffensif non posé : %v", e["TW_AUTRE"])
	}
	for _, key := range []string{
		"nginx.ingress.kubernetes.io/whitelist-source-range", "nginx.ingress.kubernetes.io/ssl-redirect",
		"nginx.ingress.kubernetes.io/force-ssl-redirect", "nginx.ingress.kubernetes.io/allowlist-source-range",
		"nginx.ingress.kubernetes.io/denylist-source-range", "nginx.ingress.kubernetes.io/configuration-snippet",
		"nginx.ingress.kubernetes.io/server-snippet", "nginx.ingress.kubernetes.io/use-forwarded-headers", "traefik.ingress.kubernetes.io/router.tls",
	} {
		t.Run("annotation/"+key, func(t *testing.T) {
			v := baseValues()
			sub(v, "ingress")["annotations"] = m{key: "x"}
			if _, out, err := render(t, v); err == nil {
				t.Errorf("annotation réservée %s acceptée\n%.200s", key, out)
			}
		})
	}
	v2 := baseValues()
	sub(v2, "ingress")["annotations"] = m{"exemple.coupel.lan/note": "ok"}
	ann, _ := get(must(t, v2)["Ingress"][0], "metadata", "annotations").(m)
	if ann["exemple.coupel.lan/note"] != "ok" {
		t.Errorf("annotation inoffensive non posée : %v", ann)
	}
}

// ClusterIssuer : la valeur d'attente « a-renseigner » (valeurs QUALIF/PROD) est refusée explicitement.
func TestChartPlaceholderClusterIssuerIsRefused(t *testing.T) {
	needHelm(t)
	validOK(t)
	v := baseValues()
	sub(v, "ingress", "tls")["clusterIssuer"] = "a-renseigner"
	_, out, err := render(t, v)
	if err == nil {
		t.Fatalf("clusterIssuer « a-renseigner » accepté")
	}
	if !strings.Contains(out, "clusterIssuer") && !strings.Contains(out, "renseigner") {
		t.Errorf("le message ne dit pas quoi renseigner : %s", out)
	}
}

// Role : tous les droits que le launcher contrôle (core/k8s.go : k8sRequiredRights) sont accordés, et pods/exec
// n'a que « create » (kubectl exec n'exige pas « get »).
func TestChartRoleCoversLauncherChecksWithoutExtraExecRights(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	granted := map[string]map[string]bool{} // ressource → verbes
	for _, r := range get(o["Role"][0], "rules").([]any) {
		rm := r.(m)
		for _, res := range rm["resources"].([]any) {
			if granted[res.(string)] == nil {
				granted[res.(string)] = map[string]bool{}
			}
			for _, vb := range rm["verbs"].([]any) {
				granted[res.(string)][vb.(string)] = true
			}
		}
	}
	for _, r := range [][2]string{{"get", "statefulsets"}, {"list", "statefulsets"}, {"create", "statefulsets"}, {"patch", "statefulsets"},
		{"delete", "statefulsets"}, {"patch", "statefulsets/scale"}, {"get", "pods"}, {"list", "pods"}, {"delete", "pods"},
		{"create", "pods/exec"}, {"list", "events"}, {"get", "persistentvolumeclaims"}} {
		if !granted[r[1]][r[0]] {
			t.Errorf("droit « %s %s » contrôlé par le launcher mais non accordé par le Role", r[0], r[1])
		}
	}
	if granted["pods/exec"]["get"] {
		t.Errorf("pods/exec « get » accordé : inutile (kubectl exec ne demande que create)")
	}
}

// Environnements (décision du 2026-11-17, remplace C1 et claude-ng2-qualif) : QUALIF dans le namespace `claude-qualif`
// sur SA racine (/share/MFS/ROOT/CLAUDE-QUALIF, volume propre) ; PROD inchangée (namespace claude-ng2, racine
// /share/MFS/ROOT/CLAUDE_PROJETS, PVC claude-projets). Le pod de QUALIF ne monte jamais la racine de PROD.
const (
	qualifRoot = "/share/MFS/ROOT/CLAUDE-QUALIF"
	prodRoot   = "/share/MFS/ROOT/CLAUDE_PROJETS"
)

func readValues(t *testing.T, env string) m {
	t.Helper()
	b, err := os.ReadFile("../deploy/helm/values-" + env + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	var v m
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestChartQualifNamespaceAndRootAreSeparateFromProd(t *testing.T) {
	needHelm(t)
	read := func(f string) string {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	qtxt, ptxt := read("../deploy/helm/values-qualif.yaml"), read("../deploy/helm/values-prod.yaml")
	if !strings.Contains(qtxt, "claude-qualif") || strings.Contains(qtxt, "claude-ng2-qualif") {
		t.Errorf("values-qualif.yaml : namespace claude-qualif attendu (claude-ng2-qualif est abandonné)")
	}
	if strings.Contains(qtxt, "-n claude-ng2 ") || strings.Contains(qtxt, "-n claude-ng2\n") {
		t.Errorf("values-qualif.yaml installe encore dans le namespace de la PROD (claude-ng2)")
	}
	if !strings.Contains(ptxt, "claude-ng2") || strings.Contains(ptxt, "qualif") && strings.Contains(strings.ReplaceAll(ptxt, "claude-launcher-qualif", ""), "claude-qualif") {
		t.Errorf("values-prod.yaml : namespace claude-ng2 attendu, sans mention du namespace de la QUALIF")
	}
	q, p := readValues(t, "qualif"), readValues(t, "prod")
	// PROD : réglages actuels inchangés
	if p["rootBase"] != prodRoot || get(p, "volume", "existingClaim") != "claude-projets" {
		t.Errorf("PROD : rootBase %v / PVC %v, attendu %s / claude-projets (réglages actuels)", p["rootBase"], get(p, "volume", "existingClaim"), prodRoot)
	}
	// QUALIF : racine propre, volume propre, jamais celui de la PROD
	if q["rootBase"] != qualifRoot {
		t.Errorf("QUALIF : rootBase %v, attendu %s", q["rootBase"], qualifRoot)
	}
	if mp, _ := get(q, "volume", "mountPath").(string); mp != "" && mp != qualifRoot {
		t.Errorf("QUALIF : volume.mountPath %v, attendu %s (ou absent)", mp, qualifRoot)
	}
	if get(q, "volume", "existingClaim") == get(p, "volume", "existingClaim") || get(q, "volume", "existingClaim") == "claude-projets" {
		t.Errorf("QUALIF : PVC %v identique à celui de la PROD — le pod de QUALIF monterait la racine de PROD", get(q, "volume", "existingClaim"))
	}
	for _, r := range []any{q["rootBase"], get(q, "volume", "mountPath")} {
		if rs, _ := r.(string); rs != "" && (rs == prodRoot || strings.HasPrefix(rs, prodRoot+"/")) {
			t.Errorf("QUALIF : chemin %s dans la racine de PROD", rs)
		}
	}
	if q["hubName"] == "" || q["hubName"] == p["hubName"] {
		t.Errorf("hubName : %v / %v — distincts attendus", q["hubName"], p["hubName"])
	}
	// rendu QUALIF dans claude-qualif : variables sous la racine de QUALIF, objets dans le namespace de la release
	out, errOut, err := helm(t, "template", "claude-launcher-qualif", chartDir, "-n", "claude-qualif",
		"-f", "../deploy/helm/values-qualif.yaml", "--set", "ingress.tls.clusterIssuer=interne-test")
	if err != nil {
		t.Fatalf("rendu QUALIF dans claude-qualif : %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "namespace: claude-qualif") || strings.Contains(out, "namespace: claude-ng2\n") || strings.Contains(out, prodRoot) {
		t.Errorf("le rendu QUALIF doit rester dans claude-qualif et ne jamais citer la racine de PROD")
	}
	o, _, _ := render(t, q, "-n", "claude-qualif", "--set", "ingress.tls.clusterIssuer=interne-test")
	e := env(t, o)
	if e["CL_ROOT_BASE"] != qualifRoot || !strings.HasPrefix(e["CL_CACHE_DIR"], qualifRoot+"/.cache/claude-launcher-ng2/") {
		t.Errorf("variables du pod de QUALIF : CL_ROOT_BASE=%q CL_CACHE_DIR=%q", e["CL_ROOT_BASE"], e["CL_CACHE_DIR"])
	}
	if e["CL_HUB_NAME"] == p["hubName"] {
		t.Errorf("le pod de QUALIF porte le nom de hub de la PROD (%q)", e["CL_HUB_NAME"])
	}
	// script de déploiement : un compte par namespace (QUALIF dans claude-qualif)
	if b, _ := os.ReadFile("../.claude-launcher/k8s-acces-deploy.sh"); len(b) > 0 {
		if !strings.Contains(string(b), "claude-qualif") || !strings.Contains(string(b), "claude-ng2") || strings.Contains(string(b), "claude-ng2-qualif") {
			t.Errorf(".claude-launcher/k8s-acces-deploy.sh : claude-qualif et claude-ng2 attendus, plus claude-ng2-qualif")
		}
	}
}

// ── volume.create : le chart peut créer le PVC (et, en option, le PV statique) de la racine ──────────────────────
// Contrat : contracts/helm-values.md (volume.create.*). Le PVC et le PV portent helm.sh/resource-policy: keep :
// `helm uninstall` ne les supprime jamais (ni les fichiers de la racine).

func withVolumeCreate(v m, mod func(c m)) m {
	v = clone(v)
	c := sub(v, "volume", "create")
	c["enabled"] = true
	sub(c, "persistentVolume")["name"] = "claude-projets-pv" // le nom du PV est OBLIGATOIRE avec volume.create (liaison statique explicite)
	if mod != nil {
		mod(c)
	}
	return v
}

func annotationsOf(o m) m {
	a, _ := get(o, "metadata", "annotations").(m)
	return a
}

func TestChartCreatesNoVolumeByDefault(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	if n := len(projectPVCs(o)); n != 0 {
		t.Errorf("PVC de projets créé sans volume.create.enabled (le PVC existant n'est ni créé ni supprimé par le chart)")
	}
	if n := len(o["PersistentVolume"]); n != 0 {
		t.Errorf("PersistentVolume créé sans volume.create.enabled")
	}
	// environnement de PROD : aucun PV/PVC rendu
	out, errOut, err := helm(t, "template", "claude-launcher", chartDir, "-n", "claude-ng2", "-f", "../deploy/helm/values-prod.yaml", "--set", "ingress.tls.clusterIssuer=interne-test")
	if err != nil {
		t.Fatalf("rendu PROD : %v\n%s", err, errOut)
	}
	// seul le coffre <release>-secrets est rendu (« kind: PersistentVolumeClaim » ne suffit plus à déceler un PVC de projets)
	if strings.Contains(out, "kind: PersistentVolume\n") {
		t.Errorf("PV rendu par les valeurs de PROD (inchangées)")
	}
	if strings.Contains(out, "name: claude-projets\n") && strings.Contains(out, "kind: PersistentVolumeClaim") {
		for _, doc := range strings.Split(out, "\n---") {
			if strings.Contains(doc, "kind: PersistentVolumeClaim") && !strings.Contains(doc, "-secrets") {
				t.Errorf("PVC de projets rendu par les valeurs de PROD (le PVC claude-projets existe déjà) :\n%s", doc)
			}
		}
	}
}

func TestChartCreatesPVCKeptAtUninstall(t *testing.T) {
	needHelm(t)
	o := must(t, withVolumeCreate(baseValues(), nil))
	pvcs := projectPVCs(o)
	if len(pvcs) != 1 {
		t.Fatalf("%d PVC de projets, attendu 1", len(pvcs))
	}
	pvc := pvcs[0]
	if get(pvc, "metadata", "name") != "claude-projets" {
		t.Errorf("PVC nommé %v, attendu volume.existingClaim (claude-projets)", get(pvc, "metadata", "name"))
	}
	if annotationsOf(pvc)["helm.sh/resource-policy"] != "keep" {
		t.Errorf("annotation helm.sh/resource-policy = %v, attendu keep (jamais supprimé par helm uninstall)", annotationsOf(pvc)["helm.sh/resource-policy"])
	}
	spec, _ := pvc["spec"].(m)
	if modes, _ := spec["accessModes"].([]any); len(modes) != 1 || modes[0] != "ReadWriteMany" {
		t.Errorf("accessModes = %v, attendu [ReadWriteMany]", spec["accessModes"])
	}
	if get(spec, "resources", "requests", "storage") != "100Gi" {
		t.Errorf("taille par défaut = %v, attendu 100Gi", get(spec, "resources", "requests", "storage"))
	}
	if sc, ok := spec["storageClassName"]; !ok || sc != "" {
		t.Errorf("storageClassName = %v, attendu \"\" explicite (liaison statique, pas de StorageClass par défaut)", spec["storageClassName"])
	}
	// le nom du PV est obligatoire : volumeName est TOUJOURS rendu (jamais de liaison à « n'importe quel PV compatible »)
	if spec["volumeName"] != "claude-projets-pv" {
		t.Errorf("volumeName = %v, attendu le nom du PV (claude-projets-pv), toujours rendu", spec["volumeName"])
	}
	if len(o["PersistentVolume"]) != 0 {
		t.Errorf("PV créé alors que persistentVolume.enabled est faux (le PV est celui de l'administrateur)")
	}
	// le StatefulSet monte bien ce PVC
	found := false
	for _, x := range pod(t, o)["volumes"].([]any) {
		if get(x, "persistentVolumeClaim", "claimName") == "claude-projets" {
			found = true
		}
	}
	if !found {
		t.Errorf("le pod ne monte pas le PVC")
	}
	// valeurs personnalisées
	o2 := must(t, withVolumeCreate(baseValues(), func(c m) {
		c["storage"] = "5Ti"
		sub(c, "persistentVolume")["name"] = "pv-admin"
	}))
	spec2 := get(projectPVCs(o2)[0], "spec").(m)
	if get(spec2, "resources", "requests", "storage") != "5Ti" || spec2["volumeName"] != "pv-admin" {
		t.Errorf("taille / PV nommé non pris en compte : %v / %v", get(spec2, "resources", "requests", "storage"), spec2["volumeName"])
	}
	if len(o2["PersistentVolume"]) != 0 {
		t.Errorf("PV créé avec le seul nom (sans persistentVolume.enabled)")
	}
}

func pvValues(mod func(src m)) m {
	return withVolumeCreate(baseValues(), func(c m) {
		pv := sub(c, "persistentVolume")
		pv["enabled"] = true
		src := m{"nfs": m{"server": "10.0.0.5", "path": "/data/p"}}
		if mod != nil {
			mod(src)
		}
		pv["source"] = src
	})
}

func TestChartCreatesStaticPVRetainedAndBound(t *testing.T) {
	needHelm(t)
	o := must(t, pvValues(nil))
	if len(o["PersistentVolume"]) != 1 || len(projectPVCs(o)) != 1 {
		t.Fatalf("%d PV et %d PVC de projets, attendu 1 et 1", len(o["PersistentVolume"]), len(projectPVCs(o)))
	}
	pv := o["PersistentVolume"][0]
	if get(pv, "metadata", "name") != "claude-projets-pv" {
		t.Errorf("PV nommé %v, attendu <existingClaim>-pv", get(pv, "metadata", "name"))
	}
	if annotationsOf(pv)["helm.sh/resource-policy"] != "keep" {
		t.Errorf("PV sans helm.sh/resource-policy: keep")
	}
	spec := pv["spec"].(m)
	if spec["persistentVolumeReclaimPolicy"] != "Retain" {
		t.Errorf("persistentVolumeReclaimPolicy = %v, attendu Retain", spec["persistentVolumeReclaimPolicy"])
	}
	if modes, _ := spec["accessModes"].([]any); len(modes) != 1 || modes[0] != "ReadWriteMany" {
		t.Errorf("accessModes du PV = %v", spec["accessModes"])
	}
	if get(spec, "claimRef", "name") != "claude-projets" || get(spec, "claimRef", "namespace") == nil {
		t.Errorf("claimRef = %v, attendu le PVC du chart (nom et namespace)", spec["claimRef"])
	}
	if get(spec, "nfs", "path") != "/data/p" || get(spec, "nfs", "server") != "10.0.0.5" {
		t.Errorf("source recopiée telle quelle : %v", spec["nfs"])
	}
	if get(spec, "capacity", "storage") != "100Gi" {
		t.Errorf("capacité du PV = %v, attendu celle du PVC (100Gi)", get(spec, "capacity", "storage"))
	}
	if ns := get(pv, "metadata", "namespace"); ns != nil {
		t.Errorf("PV avec un namespace (%v) : objet du cluster", ns)
	}
	// le PV est un objet du cluster : aucun ClusterRole n'est créé pour autant
	if len(o["ClusterRole"]) != 0 || len(o["ClusterRoleBinding"]) != 0 {
		t.Errorf("ClusterRole créé pour gérer le PV (droits d'administrateur au déploiement, pas au pod)")
	}
	pvcSpec := get(projectPVCs(o)[0], "spec").(m)
	if pvcSpec["volumeName"] != "claude-projets-pv" {
		t.Errorf("le PVC n'est pas lié au PV créé : %v", pvcSpec["volumeName"])
	}
}

func TestChartPVSourceMandatoryAndPlatformKeysRefused(t *testing.T) {
	needHelm(t)
	must(t, pvValues(nil)) // précondition : une configuration valide rend
	// PV demandé sans source
	v := withVolumeCreate(baseValues(), func(c m) { sub(c, "persistentVolume")["enabled"] = true })
	if _, out, err := render(t, v); err == nil {
		t.Errorf("PV sans source accepté\n%.200s", out)
	} else if !strings.Contains(out, "source") {
		t.Errorf("le message ne nomme pas « source » : %s", out)
	}
	// clés posées par le chart, refusées dans la source
	for _, key := range []string{"capacity", "accessModes", "claimRef", "persistentVolumeReclaimPolicy", "storageClassName", "volumeMode"} {
		t.Run(key, func(t *testing.T) {
			var val any = "x"
			switch key {
			case "capacity", "claimRef":
				val = m{"name": "x"}
			case "accessModes":
				val = []any{"ReadWriteOnce"}
			}
			v := pvValues(func(src m) { src[key] = val })
			if _, out, err := render(t, v); err == nil {
				t.Errorf("clé %s acceptée dans persistentVolume.source\n%.200s", key, out)
			} else if !strings.Contains(out, key) {
				t.Errorf("le message ne nomme pas %s : %s", key, out)
			}
		})
	}
}

// Valeurs de QUALIF : PVC créé par le chart (claude-projects-qualif, lié au PV de l'administrateur claude-qualif-pv),
// aucun PV rendu ; rootBase de QUALIF montée seulement.
func TestChartQualifValuesCreateItsOwnPVC(t *testing.T) {
	needHelm(t)
	q := readValues(t, "qualif")
	o, out, err := render(t, q, "-n", "claude-qualif", "--set", "ingress.tls.clusterIssuer=interne-test")
	if err != nil {
		t.Fatalf("rendu QUALIF : %v\n%s", err, out)
	}
	if len(projectPVCs(o)) != 1 {
		t.Fatalf("%d PVC de projets rendus pour la QUALIF, attendu 1", len(projectPVCs(o)))
	}
	pvc := projectPVCs(o)[0]
	if get(pvc, "metadata", "name") != "claude-projects-qualif" || (get(pvc, "metadata", "namespace") != nil && get(pvc, "metadata", "namespace") != "claude-qualif") {
		t.Errorf("PVC de QUALIF : %v / %v", get(pvc, "metadata", "name"), get(pvc, "metadata", "namespace"))
	}
	if get(pvc, "spec", "volumeName") != "claude-qualif-pv" {
		t.Errorf("volumeName = %v, attendu claude-qualif-pv", get(pvc, "spec", "volumeName"))
	}
	if annotationsOf(pvc)["helm.sh/resource-policy"] != "keep" {
		t.Errorf("PVC de QUALIF supprimable par helm uninstall")
	}
	if len(o["PersistentVolume"]) != 0 {
		t.Errorf("PV rendu par défaut pour la QUALIF (créé par l'administrateur)")
	}
	if strings.Contains(out, "claimName: claude-projets\n") {
		t.Errorf("le pod de QUALIF monte le PVC de la PROD")
	}
}

// Nom du PV obligatoire (décision CDP du 2026-11-17) : sans lui, le PVC se lierait à n'importe quel PV compatible.
func TestChartVolumeCreateRequiresPersistentVolumeName(t *testing.T) {
	needHelm(t)
	must(t, withVolumeCreate(baseValues(), nil)) // précondition : avec le nom, ça rend
	v := withVolumeCreate(baseValues(), func(c m) { delete(sub(c, "persistentVolume"), "name") })
	_, out, err := render(t, v)
	if err == nil {
		t.Fatalf("volume.create.enabled sans persistentVolume.name accepté (liaison à n'importe quel PV compatible)")
	}
	if !strings.Contains(out, "persistentVolume.name") && !strings.Contains(out, "persistentVolume") {
		t.Errorf("le message ne nomme pas persistentVolume.name : %s", out)
	}
	// avec le PV créé par le chart, le nom reste obligatoire
	v2 := pvValues(nil)
	delete(sub(v2, "volume", "create", "persistentVolume"), "name")
	if _, out, err := render(t, v2); err == nil {
		t.Errorf("PV créé sans nom accepté\n%.200s", out)
	}
	// pas de création : le nom n'est pas exigé
	must(t, baseValues())
}

// StorageClass (provisionnement dynamique) : le nom du PV n'est pas exigé, mais persistentVolume.enabled est exclu
// (un dossier neuf, pas rootBase).
func TestChartVolumeCreateStorageClassExcludesPersistentVolume(t *testing.T) {
	needHelm(t)
	v := withVolumeCreate(baseValues(), func(c m) {
		delete(sub(c, "persistentVolume"), "name")
		c["storageClassName"] = "moosefs-dyn"
	})
	o, out, err := render(t, v)
	if err != nil {
		t.Fatalf("StorageClass sans nom de PV refusée : %s", out)
	}
	spec := get(projectPVCs(o)[0], "spec").(m)
	if spec["storageClassName"] != "moosefs-dyn" {
		t.Errorf("storageClassName = %v", spec["storageClassName"])
	}
	if _, has := spec["volumeName"]; has {
		t.Errorf("volumeName posé avec une StorageClass (provisionnement dynamique) : %v", spec["volumeName"])
	}
	v2 := pvValues(nil)
	sub(v2, "volume", "create")["storageClassName"] = "moosefs-dyn"
	if _, out, err := render(t, v2); err == nil {
		t.Errorf("StorageClass ET PV créé acceptés\n%.200s", out)
	}
	// StorageClass ET nom de PV (liaison statique) : exclusifs aussi, même sans création du PV
	v3 := withVolumeCreate(baseValues(), func(c m) { c["storageClassName"] = "moosefs-dyn" })
	_, out3, err3 := render(t, v3)
	if err3 == nil {
		t.Errorf("StorageClass ET persistentVolume.name acceptés (liaison statique et provisionnement dynamique)")
	} else if !strings.Contains(out3, "exclusif") {
		t.Errorf("le message ne dit pas que les deux sont exclusifs : %s", out3)
	}
}

// Valeurs volume.create.persistentVolume.* posées SANS volume.create.enabled : refusées (elles seraient ignorées en
// silence : on croirait un PV créé alors qu'aucun objet ne l'est). Observation QA LOT-4.2.
func TestChartPersistentVolumeValuesWithoutCreateAreRefused(t *testing.T) {
	needHelm(t)
	must(t, baseValues()) // précondition
	must(t, pvValues(nil))
	for _, tc := range []struct {
		name string
		mod  func(pv m)
	}{
		{"enabled", func(pv m) {
			pv["enabled"] = true
			pv["name"] = "claude-projets-pv"
			pv["source"] = m{"nfs": m{"server": "10.0.0.5", "path": "/data/p"}}
		}},
		{"source", func(pv m) { pv["source"] = m{"nfs": m{"server": "10.0.0.5", "path": "/data/p"}} }},
		{"name", func(pv m) { pv["name"] = "claude-projets-pv" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := clone(baseValues())
			tc.mod(sub(v, "volume", "create", "persistentVolume"))
			_, out, err := render(t, v)
			if err == nil {
				t.Fatalf("persistentVolume.%s posé sans volume.create.enabled accepté (ignoré en silence)", tc.name)
			}
			if !strings.Contains(out, "volume.create.enabled") {
				t.Errorf("le message ne nomme pas volume.create.enabled : %s", out)
			}
		})
	}
	// create.enabled explicitement faux : même refus
	v := clone(baseValues())
	c := sub(v, "volume", "create")
	c["enabled"] = false
	sub(c, "persistentVolume")["enabled"] = true
	if _, _, err := render(t, v); err == nil {
		t.Errorf("persistentVolume.enabled avec volume.create.enabled=false accepté")
	}
}

// ── image : paquets ghcr séparés QUALIF / PROD, image.digest ─────────────────────────────────────────────────
// Décision du 2026-11-17 : l'image de QUALIF vit dans un paquet SÉPARÉ (ghcr.io/ccoupel/claude-launcher-qualif), jamais
// dans celui de PROD (ghcr.io/ccoupel/claude-launcher, écrit par la CI au tag seulement).

const (
	prodImageRepo   = "ghcr.io/ccoupel/claude-launcher"
	qualifImageRepo = "ghcr.io/ccoupel/claude-launcher-qualif"
)

func podImage(t *testing.T, o map[string][]m) string {
	t.Helper()
	c := pod(t, o)["containers"].([]any)[0].(m)
	img, _ := c["image"].(string)
	return img
}

func TestChartQualifAndProdImagesAreSeparatePackages(t *testing.T) {
	needHelm(t)
	q, p := readValues(t, "qualif"), readValues(t, "prod")
	if get(q, "image", "repository") != qualifImageRepo {
		t.Errorf("QUALIF : image.repository = %v, attendu %s", get(q, "image", "repository"), qualifImageRepo)
	}
	if get(p, "image", "repository") != prodImageRepo {
		t.Errorf("PROD : image.repository = %v, attendu %s", get(p, "image", "repository"), prodImageRepo)
	}
	issuer := []string{"--set", "ingress.tls.clusterIssuer=interne-test"}
	qout, qerr, err := helm(t, append([]string{"template", "claude-launcher-qualif", chartDir, "-n", "claude-qualif", "-f", "../deploy/helm/values-qualif.yaml", "--set", "image.tag=v6.3.0.1"}, issuer...)...)
	if err != nil {
		t.Fatalf("rendu QUALIF : %v\n%s", err, qerr)
	}
	if strings.Contains(qout, prodImageRepo+":") || strings.Contains(qout, prodImageRepo+"@") {
		t.Errorf("le rendu QUALIF référence l'image de PROD (%s) : jamais dans le paquet de production", prodImageRepo)
	}
	if !strings.Contains(qout, qualifImageRepo+":v6.3.0.1") {
		t.Errorf("le rendu QUALIF ne référence pas %s:v6.3.0.1", qualifImageRepo)
	}
	pout, perr, err := helm(t, append([]string{"template", "claude-launcher", chartDir, "-n", "claude-ng2", "-f", "../deploy/helm/values-prod.yaml"}, issuer...)...)
	if err != nil {
		t.Fatalf("rendu PROD : %v\n%s", err, perr)
	}
	for _, l := range strings.Split(pout, "\n") { // les commentaires du gabarit sont ignorés : seuls les objets comptent
		if tl := strings.TrimSpace(l); !strings.HasPrefix(tl, "#") && strings.Contains(strings.ToLower(l), "qualif") {
			t.Errorf("le rendu PROD mentionne la QUALIF (paquet, namespace ou nom) : %q", l)
		}
	}
	if !strings.Contains(pout, prodImageRepo+":") {
		t.Errorf("le rendu PROD ne référence pas %s", prodImageRepo)
	}
	// la CI ne publie la release que dans le paquet de PROD
	b, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("release.yml : %v", err)
	}
	rel := string(b)
	if strings.Contains(rel, "claude-launcher-qualif") {
		t.Errorf("release.yml cite le paquet de QUALIF : la release ne pousse que %s", prodImageRepo)
	}
	if !strings.Contains(rel, "ghcr.io/ccoupel/claude-launcher") && !strings.Contains(rel, "ghcr.io/${{ github.repository_owner }}/claude-launcher") && !strings.Contains(rel, "claude-launcher") {
		t.Errorf("release.yml ne cite pas l'image du launcher")
	}
}

func TestChartImageDigest(t *testing.T) {
	needHelm(t)
	digest := "sha256:" + strings.Repeat("ab12", 16) // 64 hex
	v := baseValues()
	img := sub(v, "image")
	img["repository"], img["tag"], img["digest"] = qualifImageRepo, "v6.3.0.9", digest
	o := must(t, v)
	if got := podImage(t, o); got != qualifImageRepo+":v6.3.0.9@"+digest {
		t.Errorf("image = %q, attendu repo:tag@digest", got)
	}
	// l'étiquette de version reste le tag seul
	ss := o["StatefulSet"][0]
	for _, lab := range []any{get(ss, "metadata", "labels", "app.kubernetes.io/version"), get(ss, "spec", "template", "metadata", "labels", "app.kubernetes.io/version")} {
		if lab != nil && lab != "v6.3.0.9" {
			t.Errorf("app.kubernetes.io/version = %v, attendu le tag seul v6.3.0.9 (sans empreinte)", lab)
		}
	}
	// sans digest : repo:tag
	v2 := baseValues()
	sub(v2, "image")["tag"] = "v6.3.0.9"
	if got := podImage(t, must(t, v2)); strings.Contains(got, "@") || !strings.HasSuffix(got, ":v6.3.0.9") {
		t.Errorf("image sans digest = %q", got)
	}
	// refus : tag contenant une empreinte, digest non sha256 ou mal formé
	for _, tc := range []struct{ name, tag, digest string }{
		{"tag@digest", "v6.3.0.9@" + digest, ""},
		{"tag avec @", "v6.3.0.9@x", ""},
		{"digest md5", "v6.3.0.9", "md5:" + strings.Repeat("a", 32)},
		{"digest trop court", "v6.3.0.9", "sha256:abc"},
		{"digest non hexadécimal", "v6.3.0.9", "sha256:" + strings.Repeat("z", 64)},
		{"digest sans préfixe", "v6.3.0.9", strings.Repeat("a", 64)},
		{"digest majuscules/long", "v6.3.0.9", "sha256:" + strings.Repeat("a", 65)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := baseValues()
			im := sub(v, "image")
			im["tag"] = tc.tag
			if tc.digest != "" {
				im["digest"] = tc.digest
			}
			if _, out, err := render(t, v); err == nil {
				t.Errorf("tag=%q digest=%q accepté\n%.200s", tc.tag, tc.digest, out)
			}
		})
	}
}

// ── LOT-4.4 : web.access (none par défaut dans le chart) → CL_WEB_ACCESS ────────────────────────────────────────

func TestChartWebAccessDefaultNoneAndToken(t *testing.T) {
	needHelm(t)
	if e := env(t, must(t, baseValues())); e["CL_WEB_ACCESS"] != "none" {
		t.Errorf("défaut du chart : CL_WEB_ACCESS = %q, attendu none", e["CL_WEB_ACCESS"])
	}
	for _, mode := range []string{"none", "token"} {
		v := baseValues()
		sub(v, "web")["access"] = mode
		if e := env(t, must(t, v)); e["CL_WEB_ACCESS"] != mode {
			t.Errorf("web.access=%s : CL_WEB_ACCESS = %q", mode, e["CL_WEB_ACCESS"])
		}
	}
	// sans Ingress (port-forward) : « none » est REFUSÉ (le launcher exige une adresse publique https pour l'accès sans
	// jeton) ; « token » est accepté
	v := baseValues()
	v["ingress"] = m{"enabled": false}
	if _, out, err := render(t, v); err == nil {
		t.Errorf("web.access: none sans Ingress accepté\n%.200s", out)
	} else if !strings.Contains(out, "web.access") {
		t.Errorf("le message ne nomme pas web.access : %s", out)
	}
	sub(v, "web")["access"] = "token"
	if e := env(t, must(t, v)); e["CL_WEB_ACCESS"] != "token" {
		t.Errorf("sans Ingress, token : CL_WEB_ACCESS = %q", e["CL_WEB_ACCESS"])
	}
}

func TestChartWebAccessInvalidValuesAndReservedEnv(t *testing.T) {
	needHelm(t)
	must(t, baseValues())
	for _, bad := range []any{"anonyme", "None", "NONE", "", "true", 1, false} {
		t.Run(fmt.Sprintf("%v", bad), func(t *testing.T) {
			v := baseValues()
			sub(v, "web")["access"] = bad
			_, out, err := render(t, v)
			if err == nil {
				t.Fatalf("web.access=%v accepté", bad)
			}
			if !strings.Contains(out, "access") {
				t.Errorf("le message ne nomme pas web.access : %s", out)
			}
		})
	}
	// CL_WEB_ACCESS posée par le chart : extraEnv ne peut pas la redéfinir
	v := baseValues()
	v["extraEnv"] = []any{m{"name": "CL_WEB_ACCESS", "value": "none"}}
	if _, out, err := render(t, v); err == nil {
		t.Errorf("extraEnv CL_WEB_ACCESS accepté\n%.200s", out)
	}
}

func TestChartEnvironmentValuesSetWebAccessNoneExplicitly(t *testing.T) {
	needHelm(t)
	for _, name := range []string{"qualif", "prod"} {
		v := readValues(t, name)
		if get(v, "web", "access") != "none" {
			t.Errorf("values-%s.yaml : web.access = %v, attendu none écrit explicitement", name, get(v, "web", "access"))
		}
	}
}

// NOTES.txt : en mode none, ni lien d'amorçage ni commande de lecture du jeton ; le rappel « sans jeton » est présent.
func TestChartNotesNoTokenInNoneBranch(t *testing.T) {
	b, err := os.ReadFile(chartDir + "/templates/NOTES.txt")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, `eq (.Values.web.access | default "none") "none"`)
	if i < 0 {
		t.Fatalf("NOTES.txt : branche web.access non trouvée")
	}
	rest := s[i:]
	j := strings.Index(rest, "{{- else }}")
	if j < 0 {
		t.Fatalf("NOTES.txt : branche token non trouvée")
	}
	none := rest[:j]
	for _, bad := range []string{"?t=", "desktop-web-token", "kubectl exec", "url-amorcage", "jeton lu"} {
		if strings.Contains(none, bad) {
			t.Errorf("NOTES.txt, mode none : « %s » présent (plus de lien d'amorçage ni de lecture du jeton)", bad)
		}
	}
	if !strings.Contains(strings.ToLower(none), "sans jeton") {
		t.Errorf("NOTES.txt, mode none : le rappel « sans jeton » manque")
	}
}

// ── coffre partagé (#201) : plus de CL_SECRETS_DIR, de PVC ni d'init ; ancien PVC seulement via lookup ──────────

func TestChartNoSecretsVolumeV650(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	ss := o["StatefulSet"][0]
	pod := sub(sub(sub(ss, "spec"), "template"), "spec")
	if ic, ok := pod["initContainers"]; ok {
		t.Errorf("initContainers présent : %v (secrets-perms supprimé)", ic)
	}
	c := pod["containers"].([]any)[0].(m)
	for _, e := range c["env"].([]any) {
		n := e.(m)["name"].(string)
		if n == "CL_SECRETS_DIR" || n == "CL_SECRETS_LEGACY_DIR" {
			t.Errorf("%s posée sans ancien PVC (lookup vide hors cluster)", n)
		}
	}
	for _, vol := range pod["volumes"].([]any) {
		if strings.HasPrefix(vol.(m)["name"].(string), "secrets") {
			t.Errorf("volume %v monté sans ancien PVC", vol.(m)["name"])
		}
	}
	// extraEnv ne peut pas poser les variables du coffre
	for _, n := range []string{"CL_SECRETS_DIR", "CL_SECRETS_LEGACY_DIR"} {
		v := clone(baseValues())
		v["extraEnv"] = []any{m{"name": n, "value": "/x"}}
		if _, _, err := render(t, v); err == nil {
			t.Errorf("extraEnv %s accepté", n)
		}
	}
	// secrets.legacy.enabled: false accepté ; clé inconnue refusée
	v := clone(baseValues())
	v["secrets"] = m{"legacy": m{"enabled": false}}
	must(t, v)
	v["secrets"] = m{"legacy": m{"bogus": 1}}
	if _, _, err := render(t, v); err == nil {
		t.Errorf("secrets.legacy.bogus accepté (schéma)")
	}
}
