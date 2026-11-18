package main

import (
	"strings"
	"testing"
)

// #200 : NetworkPolicy activée par défaut, entrée depuis le namespace du contrôleur d'Ingress seulement
func TestChartNetworkPolicyParDefaut200(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	if len(o["NetworkPolicy"]) != 1 {
		t.Fatalf("%d NetworkPolicy, attendu 1 par défaut", len(o["NetworkPolicy"]))
	}
	spec := get(o["NetworkPolicy"][0], "spec").(m)
	types, _ := spec["policyTypes"].([]any)
	if len(types) != 1 || types[0] != "Ingress" {
		t.Errorf("policyTypes = %v : la sortie ne doit pas être restreinte", types)
	}
	ing, _ := spec["ingress"].([]any)
	if len(ing) != 1 {
		t.Fatalf("ingress = %v", ing)
	}
	from, _ := get(ing[0], "from").([]any)
	if len(from) != 1 || get(from[0], "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name") != "traefik" {
		t.Errorf("source = %v, attendu namespace traefik", from)
	}
	ports, _ := get(ing[0], "ports").([]any)
	if len(ports) != 1 || get(ports[0], "port") != 8080 && get(ports[0], "port") != float64(8080) {
		t.Errorf("ports = %v", ports)
	}
}

func TestChartNetworkPolicyReglableEtDesactivable200(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	v["networkPolicy"] = m{"ingressNamespaces": []any{"ingress-nginx"}, "ingressCIDRs": []any{"192.168.1.0/24"}}
	o := must(t, v)
	from, _ := get(get(o["NetworkPolicy"][0], "spec", "ingress").([]any)[0], "from").([]any)
	if len(from) != 2 {
		t.Errorf("from = %v, attendu namespace + ipBlock", from)
	}
	v = clone(baseValues())
	v["networkPolicy"] = m{"enabled": false}
	if o := must(t, v); len(o["NetworkPolicy"]) != 0 {
		t.Error("NetworkPolicy rendue avec enabled: false")
	}
	// aucune source : plus aucune entrée → refus
	v = clone(baseValues())
	v["networkPolicy"] = m{"ingressNamespaces": []any{}}
	if _, out, err := render(t, v); err == nil {
		t.Errorf("networkPolicy sans source acceptée\n%.200s", out)
	}
}

// ingress externe préparé : refusé au rendu avant l'authentification (v7.0.0)
func TestChartIngressExterneRefuse200(t *testing.T) {
	needHelm(t)
	must(t, baseValues())
	v := clone(baseValues())
	v["externalIngress"] = m{"enabled": true, "className": "traefik", "host": "launcher.exemple.org", "tls": m{"clusterIssuer": "letsencrypt-prod"}}
	_, out, err := render(t, v)
	if err == nil {
		t.Fatal("externalIngress.enabled accepté sans authentification")
	}
	if !strings.Contains(out, "v7.0.0") || !strings.Contains(out, "ingress externe refusé") {
		t.Errorf("message sans renvoi à v7.0.0 : %.300s", out)
	}
}
