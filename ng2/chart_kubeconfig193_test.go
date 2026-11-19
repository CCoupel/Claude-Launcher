package main

import (
	"reflect"
	"strings"
	"testing"
)

// LOT-2.2 (#193) — rbac.extraNamespaces et backends.kubeconfigs. Écrit depuis contracts/helm-values.md (v6.4.0) et
// contracts/backends-remote.md. Aides de chart_helm_test.go. Ignoré sans helm.

func withExtraNS(ns ...string) m {
	v := clone(baseValues())
	l := make([]any, len(ns))
	for i, n := range ns {
		l[i] = n
	}
	sub(v, "rbac")["extraNamespaces"] = l
	return v
}

func byNamespace(objs []m) map[string]m {
	out := map[string]m{}
	for _, o := range objs {
		ns, _ := get(o, "metadata", "namespace").(string)
		out[ns] = o
	}
	return out
}

func TestChartExtraNamespacesRoleEtBinding193(t *testing.T) {
	needHelm(t)
	o := must(t, withExtraNS("claude-qualif-extra", "autre-ns"), "-n", "claude-qualif")
	roles, binds := byNamespace(o["Role"]), byNamespace(o["RoleBinding"])
	for _, ns := range []string{"claude-qualif", "claude-qualif-extra", "autre-ns"} {
		if roles[ns] == nil || binds[ns] == nil {
			t.Fatalf("Role/RoleBinding absent dans %s (rendus : %v / %v)", ns, keys(roles), keys(binds))
		}
	}
	if len(o["Role"]) != 3 || len(o["RoleBinding"]) != 3 {
		t.Errorf("%d Role et %d RoleBinding, attendu 3 et 3 (release + 2 namespaces)", len(o["Role"]), len(o["RoleBinding"]))
	}
	// mêmes droits partout
	ref := roles["claude-qualif"]["rules"]
	for ns, r := range roles {
		if !reflect.DeepEqual(r["rules"], ref) {
			t.Errorf("règles du Role de %s différentes de celles du namespace de la release", ns)
		}
	}
	// le sujet est toujours le ServiceAccount du pod (namespace de la release)
	for ns, b := range binds {
		subs, _ := b["subjects"].([]any)
		if len(subs) != 1 || get(subs[0], "kind") != "ServiceAccount" || get(subs[0], "namespace") != "claude-qualif" {
			t.Errorf("RoleBinding de %s : sujets %v, attendu le ServiceAccount de claude-qualif", ns, subs)
		}
	}
	// aucun droit à l'échelle du cluster
	for _, k := range []string{"ClusterRole", "ClusterRoleBinding"} {
		if len(o[k]) != 0 {
			t.Errorf("%s rendu : aucun droit cluster", k)
		}
	}
}

func TestChartExtraNamespacesVideParDefaut193(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues(), "-n", "claude-qualif")
	if len(o["Role"]) != 1 || len(o["RoleBinding"]) != 1 {
		t.Errorf("%d Role et %d RoleBinding par défaut, attendu 1 et 1", len(o["Role"]), len(o["RoleBinding"]))
	}
}

func TestChartExtraNamespacesDoublonsIgnores193(t *testing.T) {
	needHelm(t)
	o := must(t, withExtraNS("claude-qualif", "autre-ns", "autre-ns"), "-n", "claude-qualif")
	if len(o["Role"]) != 2 || len(o["RoleBinding"]) != 2 {
		t.Errorf("%d Role et %d RoleBinding, attendu 2 et 2 (release et doublons fusionnés)", len(o["Role"]), len(o["RoleBinding"]))
	}
}

func keys(mm map[string]m) []string {
	var out []string
	for k := range mm {
		out = append(out, k)
	}
	return out
}

func withKubeconfigs(entries ...m) m {
	v := clone(baseValues())
	l := make([]any, len(entries))
	for i, e := range entries {
		l[i] = e
	}
	sub(v, "backends")["kubeconfigs"] = l
	return v
}

func TestChartKubeconfigsMontesEnLectureSeule0444193(t *testing.T) {
	needHelm(t)
	o := must(t, withKubeconfigs(m{"name": "autre", "secretName": "kc-autre", "key": "config"}))
	c := launcherContainer(t, o)
	mm := mountOfPrefix(c, "/var/run/claude-launcher/kube/autre")
	if mm == nil {
		t.Fatalf("kubeconfig non monté sous /var/run/claude-launcher/kube/autre : %v", c["volumeMounts"])
	}
	if mm["readOnly"] != true {
		t.Errorf("le kubeconfig doit être monté en lecture seule : %v", mm)
	}
	var vol m
	for _, x := range pod(t, o)["volumes"].([]any) {
		if vm := x.(m); vm["name"] == mm["name"] {
			vol = vm
		}
	}
	if vol == nil || get(vol, "secret", "secretName") != "kc-autre" {
		t.Fatalf("volume Secret kc-autre absent : %v", vol)
	}
	// 0444 = 292 décimal : lisible par l'uid 1000 du pod (même motif que le Secret docker)
	if dm := get(vol, "secret", "defaultMode"); dm != 292 && dm != 0o444 && dm != float64(292) {
		t.Errorf("defaultMode = %v (%T), attendu 0444 (292)", dm, dm)
	}
	// le fichier s'appelle « config » dans le dossier du backend : /var/run/claude-launcher/kube/<name>/config
	ok := false
	if mm["subPath"] == "config" && mm["mountPath"] == "/var/run/claude-launcher/kube/autre/config" {
		ok = true
	}
	if items, _ := get(vol, "secret", "items").([]any); mm["mountPath"] == "/var/run/claude-launcher/kube/autre" && len(items) == 1 &&
		get(items[0], "key") == "config" && get(items[0], "path") == "config" {
		ok = true
	}
	if !ok {
		t.Errorf("le Secret doit aboutir à /var/run/claude-launcher/kube/autre/config (montage %v, volume %v)", mm, vol["secret"])
	}
}

func TestChartKubeconfigCleDuSecretUtilisee193(t *testing.T) {
	needHelm(t)
	o := must(t, withKubeconfigs(m{"name": "autre", "secretName": "kc-autre", "key": "kubeconfig.yaml"}))
	found := false
	for _, x := range pod(t, o)["volumes"].([]any) {
		if get(x, "secret", "secretName") != "kc-autre" {
			continue
		}
		if items, _ := get(x, "secret", "items").([]any); len(items) == 1 && get(items[0], "key") == "kubeconfig.yaml" {
			found = true
		}
	}
	for _, x := range launcherContainer(t, o)["volumeMounts"].([]any) {
		if x.(m)["subPath"] == "kubeconfig.yaml" {
			found = true
		}
	}
	if !found {
		t.Error("la clé « kubeconfig.yaml » du Secret n'est référencée ni par items ni par subPath")
	}
}

func TestChartKubeconfigsPlusieursEtAucunParDefaut193(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	if mountOfPrefix(launcherContainer(t, o), "/var/run/claude-launcher/kube") != nil {
		t.Error("montage kube sans backends.kubeconfigs")
	}
	o = must(t, withKubeconfigs(m{"name": "a", "secretName": "kc-a", "key": "config"}, m{"name": "b", "secretName": "kc-b", "key": "config"}))
	for _, n := range []string{"a", "b"} {
		if mountOfPrefix(launcherContainer(t, o), "/var/run/claude-launcher/kube/"+n) == nil {
			t.Errorf("kubeconfig %s non monté", n)
		}
	}
}

func TestChartKubeconfigsValeursInvalidesRefusees193(t *testing.T) {
	needHelm(t)
	for _, tc := range []struct {
		name string
		v    m
		want string
	}{
		{"nom en double", withKubeconfigs(m{"name": "a", "secretName": "x", "key": "config"}, m{"name": "a", "secretName": "y", "key": "config"}), "a"},
		{"nom avec chemin", withKubeconfigs(m{"name": "../evasion", "secretName": "x", "key": "config"}), "name"},
		{"sans secretName", withKubeconfigs(m{"name": "a", "key": "config"}), "secretName"},
		{"sans key", withKubeconfigs(m{"name": "a", "secretName": "x"}), "key"},
		{"chevauche le volume des projets", func() m {
			v := withKubeconfigs(m{"name": "a", "secretName": "x", "key": "config"})
			sub(v, "volume")["mountPath"] = "/var/run/claude-launcher"
			v["rootBase"] = "/var/run/claude-launcher/p"
			return v
		}(), "mountPath"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := render(t, tc.v)
			if err == nil {
				t.Fatalf("rendu accepté (%s)", tc.name)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("le message ne nomme pas %q :\n%s", tc.want, out)
			}
		})
	}
}

func mountOfPrefix(c m, prefix string) m {
	ms, _ := c["volumeMounts"].([]any)
	for _, x := range ms {
		if mm, ok := x.(m); ok {
			if p, _ := mm["mountPath"].(string); strings.HasPrefix(p, prefix) {
				return mm
			}
		}
	}
	return nil
}
