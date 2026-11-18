package main

import (
	"fmt"
	"strings"
	"testing"
)

// LOT-4 (#200) — NetworkPolicy par défaut et ingress externe préparé mais refusé. Écrit depuis
// contracts/helm-values.md (« v6.4.0 ») et contracts/web-server.md. Aides de chart_helm_test.go. Ignoré sans helm.

func netpol(t *testing.T, o map[string][]m) m {
	t.Helper()
	if len(o["NetworkPolicy"]) != 1 {
		t.Fatalf("%d NetworkPolicy, attendu 1", len(o["NetworkPolicy"]))
	}
	return o["NetworkPolicy"][0]
}

func TestChartNetworkPolicyParDefaut200(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	np := netpol(t, o)
	// le pod du launcher, et lui seul, est visé (mêmes labels que le StatefulSet)
	sel, _ := get(np, "spec", "podSelector", "matchLabels").(m)
	pl, _ := get(o["StatefulSet"][0], "spec", "selector", "matchLabels").(m)
	if len(sel) == 0 || fmt.Sprint(sel) != fmt.Sprint(pl) {
		t.Errorf("podSelector %v ≠ sélecteur du StatefulSet %v", sel, pl)
	}
	// entrée seulement : la sortie n'est pas restreinte (backends distants, GitHub, ACME)
	types, _ := get(np, "spec", "policyTypes").([]any)
	if len(types) != 1 || types[0] != "Ingress" {
		t.Errorf("policyTypes = %v, attendu [Ingress] (sortie non restreinte)", types)
	}
	if _, has := get(np, "spec").(m)["egress"]; has {
		t.Error("règle egress présente : la sortie ne doit pas être restreinte")
	}
	// entrée depuis le namespace du contrôleur d'Ingress (traefik), sur le port du Service seulement
	rules, _ := get(np, "spec", "ingress").([]any)
	if len(rules) == 0 {
		t.Fatal("aucune règle d'entrée : le pod serait injoignable")
	}
	found := false
	for _, r := range rules {
		from, _ := get(r, "from").([]any)
		for _, f := range from {
			if get(f, "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name") == "traefik" {
				found = true
			}
		}
		ports, _ := get(r, "ports").([]any)
		if len(ports) == 0 {
			t.Errorf("règle d'entrée sans port : tous les ports seraient ouverts : %v", r)
		}
		for _, p := range ports {
			if get(p, "protocol") != "TCP" {
				t.Errorf("protocole %v, attendu TCP", get(p, "protocol"))
			}
		}
	}
	if !found {
		t.Errorf("entrée depuis le namespace traefik absente : %v", rules)
	}
}

func TestChartNetworkPolicyPortDuService200(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	sub(v, "service")["port"] = 9443
	np := netpol(t, must(t, v))
	for _, r := range get(np, "spec", "ingress").([]any) {
		for _, p := range get(r, "ports").([]any) {
			if get(p, "port") != 9443 && get(p, "port") != float64(9443) {
				t.Errorf("port %v, attendu service.port (9443)", get(p, "port"))
			}
		}
	}
}

func TestChartNetworkPolicyDesactivable200(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	sub(v, "networkPolicy")["enabled"] = false
	o := must(t, v)
	if len(o["NetworkPolicy"]) != 0 {
		t.Errorf("%d NetworkPolicy avec networkPolicy.enabled=false", len(o["NetworkPolicy"]))
	}
}

func TestChartNetworkPolicyNamespacesReglables200(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	sub(v, "networkPolicy")["ingressNamespaces"] = []any{"traefik", "ingress-nginx"}
	np := netpol(t, must(t, v))
	got := map[string]bool{}
	for _, r := range get(np, "spec", "ingress").([]any) {
		for _, f := range get(r, "from").([]any) {
			if n, ok := get(f, "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name").(string); ok {
				got[n] = true
			}
		}
	}
	if !got["traefik"] || !got["ingress-nginx"] {
		t.Errorf("namespaces admis = %v, attendu traefik et ingress-nginx", got)
	}
}

func TestChartNetworkPolicyJamaisOuverteATous200(t *testing.T) {
	needHelm(t)
	// aucune configuration ne doit produire une règle « from » vide (= tout le monde) ou un ipBlock 0.0.0.0/0
	v := clone(baseValues())
	sub(v, "networkPolicy")["ingressNamespaces"] = []any{"traefik"}
	np := netpol(t, must(t, v))
	for _, r := range get(np, "spec", "ingress").([]any) {
		from, _ := get(r, "from").([]any)
		if len(from) == 0 {
			t.Errorf("règle sans « from » (ouverte à tous) : %v", r)
		}
		for _, f := range from {
			if c, _ := get(f, "ipBlock", "cidr").(string); c == "0.0.0.0/0" || c == "::/0" {
				t.Errorf("ipBlock ouvert à tous : %s", c)
			}
		}
	}
}

func TestChartIngressExterneRefuse200(t *testing.T) {
	needHelm(t)
	// désactivé (défaut) : aucun second Ingress
	o := must(t, baseValues())
	if n := len(o["Ingress"]); n != 1 {
		t.Errorf("%d Ingress par défaut, attendu 1 (l'interne seul)", n)
	}
	// activé : échec explicite du rendu, avec le motif
	v := clone(baseValues())
	e := sub(v, "externalIngress")
	e["enabled"] = true
	e["className"] = "traefik"
	e["host"] = "launcher.exemple.net"
	sub(e, "tls")["clusterIssuer"] = "letsencrypt-prod"
	_, out, err := render(t, v)
	if err == nil {
		t.Fatal("externalIngress.enabled=true accepté : aucune authentification avant v7.0.0")
	}
	for _, want := range []string{"ingress externe refusé", "v7.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("le message ne contient pas %q :\n%s", want, out)
		}
	}
}

func TestChartIngressExterneValeursDeclarees200(t *testing.T) {
	needHelm(t)
	// les valeurs sont dans le schéma : un hôte et un ClusterIssuer sans enabled ne cassent pas le rendu et ne créent rien
	v := clone(baseValues())
	e := sub(v, "externalIngress")
	e["host"] = "launcher.exemple.net"
	e["className"] = "traefik"
	sub(e, "tls")["clusterIssuer"] = "letsencrypt-prod"
	o := must(t, v)
	if len(o["Ingress"]) != 1 {
		t.Errorf("%d Ingress, attendu 1 tant que externalIngress.enabled est faux", len(o["Ingress"]))
	}
	for _, c := range o["Certificate"] {
		if strings.Contains(fmt.Sprint(get(c, "spec", "issuerRef")), "letsencrypt-prod") {
			t.Error("Certificate Let's Encrypt créé alors que l'ingress externe est désactivé")
		}
	}
}

func TestChartDelaiDArretAuMoins30s200(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	g := get(o["StatefulSet"][0], "spec", "template", "spec", "terminationGracePeriodSeconds")
	n, _ := g.(int)
	if f, ok := g.(float64); ok {
		n = int(f)
	}
	if n < 30 {
		t.Errorf("terminationGracePeriodSeconds = %v, attendu ≥ 30 (arrêt propre des terminaux avant SIGKILL)", g)
	}
}

// l'entrée est limitée aux pods du contrôleur (ET namespace + pod), pas à tout le namespace
func TestChartNetworkPolicyPodSelector200(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	from, _ := get(get(o["NetworkPolicy"][0], "spec", "ingress").([]any)[0], "from").([]any)
	if get(from[0], "podSelector", "matchLabels", "app.kubernetes.io/name") != "traefik" {
		t.Errorf("podSelector du contrôleur absent par défaut : %v", from[0])
	}
	v := clone(baseValues())
	v["networkPolicy"] = m{"ingressPodSelector": m{"app.kubernetes.io/name": "ingress-nginx"}}
	o = must(t, v)
	from, _ = get(get(o["NetworkPolicy"][0], "spec", "ingress").([]any)[0], "from").([]any)
	if get(from[0], "podSelector", "matchLabels", "app.kubernetes.io/name") != "ingress-nginx" {
		t.Errorf("podSelector réglé non repris : %v", from[0])
	}
}
