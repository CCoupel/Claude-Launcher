package main

import (
	"os"
	"strings"
	"testing"
)

// LOT-2.1 (#194) — coffre des secrets du pod, Secret TLS docker, image. Écrit depuis contracts/helm-values.md
// (section « v6.4.0 ») et contracts/backends-remote.md. Réutilise les aides de chart_helm_test.go. Ignoré sans helm.

const secretsMount = "/var/lib/claude-launcher/secrets"

func launcherContainer(t *testing.T, o map[string][]m) m {
	t.Helper()
	cs, _ := pod(t, o)["containers"].([]any)
	if len(cs) == 0 {
		t.Fatal("aucun conteneur")
	}
	return cs[0].(m)
}

func mountOf(c m, path string) m {
	ms, _ := c["volumeMounts"].([]any)
	for _, x := range ms {
		if mm, ok := x.(m); ok && mm["mountPath"] == path {
			return mm
		}
	}
	return nil
}

func TestChartSecretsPVCParDefaut194(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	var coffre m
	for _, p := range o["PersistentVolumeClaim"] {
		if n, _ := get(p, "metadata", "name").(string); strings.HasSuffix(n, "-secrets") {
			coffre = p
		}
	}
	if coffre == nil {
		t.Fatalf("PVC <release>-secrets absent : %v", o["PersistentVolumeClaim"])
	}
	if get(coffre, "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Error("le PVC du coffre doit être conservé à la désinstallation (helm.sh/resource-policy: keep)")
	}
	if am, _ := get(coffre, "spec", "accessModes").([]any); len(am) != 1 || am[0] != "ReadWriteOnce" {
		t.Errorf("accessModes = %v, attendu [ReadWriteOnce] (jamais un volume partagé)", am)
	}
	if get(coffre, "spec", "resources", "requests", "storage") != "10Mi" {
		t.Errorf("taille = %v, attendu 10Mi", get(coffre, "spec", "resources", "requests", "storage"))
	}
	if _, set := coffre["spec"].(m)["storageClassName"]; set {
		t.Error("storageClassName posée par défaut : la classe par défaut du cluster est attendue")
	}
}

func TestChartSecretsPVCValeursPrises194(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	sub(v, "secrets", "persistence")["size"] = "20Mi"
	sub(v, "secrets", "persistence")["storageClassName"] = "local-rwo"
	o := must(t, v)
	found := false
	for _, p := range o["PersistentVolumeClaim"] {
		if n, _ := get(p, "metadata", "name").(string); strings.HasSuffix(n, "-secrets") {
			found = true
			if get(p, "spec", "resources", "requests", "storage") != "20Mi" || get(p, "spec", "storageClassName") != "local-rwo" {
				t.Errorf("valeurs ignorées : %v", p["spec"])
			}
		}
	}
	if !found {
		t.Fatal("PVC du coffre absent")
	}
}

func TestChartCoffreMonteHorsRacinePartagee194(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	c := launcherContainer(t, o)
	if env(t, o)["CL_SECRETS_DIR"] != secretsMount {
		t.Errorf("CL_SECRETS_DIR = %q, attendu %s", env(t, o)["CL_SECRETS_DIR"], secretsMount)
	}
	mm := mountOf(c, secretsMount)
	if mm == nil {
		t.Fatal("coffre non monté dans le conteneur")
	}
	if mm["readOnly"] == true {
		t.Error("le coffre doit être inscriptible (la clé est déposée par l'interface)")
	}
	// la source du montage est le PVC du coffre, pas le volume partagé des projets
	vols, _ := pod(t, o)["volumes"].([]any)
	for _, x := range vols {
		vm := x.(m)
		if vm["name"] != mm["name"] {
			continue
		}
		claim, _ := get(vm, "persistentVolumeClaim", "claimName").(string)
		if !strings.HasSuffix(claim, "-secrets") || claim == "claude-projets" {
			t.Errorf("le coffre est monté depuis %q : jamais le volume partagé", claim)
		}
	}
	// hors de la racine partagée : le montage du volume projets ne le contient pas
	if strings.HasPrefix(secretsMount, "/data/p") {
		t.Error("coffre sous la racine partagée")
	}
}

func TestChartAucunDroitSecretsKubernetes194(t *testing.T) {
	needHelm(t)
	o := must(t, baseValues())
	for _, r := range append(o["Role"], o["ClusterRole"]...) {
		rules, _ := r["rules"].([]any)
		for _, x := range rules {
			rs, _ := x.(m)["resources"].([]any)
			for _, res := range rs {
				if res == "secrets" || res == "*" {
					t.Errorf("le launcher n'écrit aucun Secret Kubernetes : règle %v dans %v", x, get(r, "metadata", "name"))
				}
			}
		}
	}
	if len(o["ClusterRole"]) != 0 {
		t.Error("aucun ClusterRole")
	}
}

func TestChartDockerTLSFacultatif194(t *testing.T) {
	needHelm(t)
	// sans secretName : ni montage ni variables TLS
	o := must(t, baseValues())
	if mountOf(launcherContainer(t, o), "/var/run/claude-launcher/docker") != nil {
		t.Error("montage TLS docker sans secretName")
	}
	e := env(t, o)
	for _, k := range []string{"DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"} {
		if _, ok := e[k]; ok {
			t.Errorf("%s posée sans secretName (docker :2375 sans TLS reste le défaut)", k)
		}
	}
	// avec secretName
	v := clone(baseValues())
	sub(v, "backends", "docker")["secretName"] = "docker-certs"
	o = must(t, v)
	mm := mountOf(launcherContainer(t, o), "/var/run/claude-launcher/docker")
	if mm == nil || mm["readOnly"] != true {
		t.Errorf("le Secret TLS doit être monté en lecture seule : %v", mm)
	}
	e = env(t, o)
	if e["DOCKER_CERT_PATH"] != "/var/run/claude-launcher/docker" || e["DOCKER_TLS_VERIFY"] != "1" {
		t.Errorf("variables TLS : %v", e)
	}
	vols, _ := pod(t, o)["volumes"].([]any)
	ok := false
	for _, x := range vols {
		if get(x, "secret", "secretName") == "docker-certs" {
			ok = true
			// uid 1000 sans fsGroup : 0400 (root) serait illisible ; tmpfs du pod, lecture seule → 0444 (292)
			if dm := get(x, "secret", "defaultMode"); dm != 292 && dm != 444 && dm != float64(292) && dm != float64(444) {
				t.Errorf("defaultMode du Secret docker = %v (%T), attendu 0444 (lisible par l'uid 1000 sans fsGroup)", dm, dm)
			}
		}
	}
	if !ok {
		t.Error("volume Secret docker-certs absent")
	}
}

func TestChartCoffreNOuvrePasLeSecretTLSEnEcriture194(t *testing.T) {
	needHelm(t)
	v := clone(baseValues())
	sub(v, "backends", "docker")["secretName"] = "docker-certs"
	o := must(t, v)
	if mm := mountOf(launcherContainer(t, o), secretsMount); mm != nil && mm["name"] == "docker-tls" {
		t.Error("le coffre et le Secret TLS ne partagent pas de volume")
	}
}

// ── Image ────────────────────────────────────────────────────────────────

func TestDockerfileClientsDistants194(t *testing.T) {
	b, err := os.ReadFile("../deploy/image/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"openssh-client", "podman-remote", "download.docker.com", "sha256sum -c"} {
		if !strings.Contains(s, want) {
			t.Errorf("Dockerfile sans %q", want)
		}
	}
	// paquets interdits à l'installation : démon docker (docker.io, 134 Mo), serveur ssh
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") || !strings.Contains(l, "apt-get install") {
			continue
		}
		for _, bad := range []string{" docker.io", " openssh-server", " docker-cli"} {
			if strings.Contains(l, bad) {
				t.Errorf("paquet interdit (%s) : %s", strings.TrimSpace(bad), l)
			}
		}
	}
	if !strings.Contains(s, "podman") || !strings.Contains(s, "/usr/local/bin/podman") {
		t.Error("podman-remote doit être appelable sous le nom « podman » (lien ou adaptation)")
	}
}
