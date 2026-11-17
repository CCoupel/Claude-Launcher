package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"claude-launcher-ng2/platform"
)

// #169 : CL_HUB_NAME remplace le nom de la machine (clé hubs.<nom>, identité de propriétaire) ; le nom réel reste
// celui des espaces de PID.
func TestHubNameOverridesHostname(t *testing.T) {
	real := platform.RealHostName()
	t.Setenv(platform.HubNameVar, "")
	if hubName() != real {
		t.Errorf("sans CL_HUB_NAME : %q, attendu le nom de la machine %q", hubName(), real)
	}
	idBefore := LauncherID()
	t.Setenv(platform.HubNameVar, "cluster-prod")
	if hubName() != "cluster-prod" || platform.HostName() != "cluster-prod" {
		t.Errorf("hubName() = %q", hubName())
	}
	if platform.RealHostName() != real {
		t.Error("le nom réel de la machine a changé")
	}
	if LauncherID() == idBefore || LauncherID() != "cluster-prod:"+LauncherID()[len("cluster-prod:"):] {
		t.Errorf("LauncherID %q ne porte pas CL_HUB_NAME", LauncherID())
	}
	// valeur invalide : ignorée par HostName, signalée par HubNameError
	for _, bad := range []string{"-x", "a b", "é", "a/b", strings.Repeat("a", 64)} {
		t.Setenv(platform.HubNameVar, bad)
		if platform.HubNameError() == nil || platform.HostName() != real {
			t.Errorf("%q : invalide attendu (erreur %v, HostName %q)", bad, platform.HubNameError(), platform.HostName())
		}
	}
	t.Setenv(platform.HubNameVar, "Pod-1.cluster_a")
	if platform.HubNameError() != nil {
		t.Errorf("nom valide refusé : %v", platform.HubNameError())
	}
}

// Identité stable : un pod recréé (nom de machine différent) garde le même LauncherID tant que CL_HUB_NAME ne change pas.
func TestLauncherIDStableAcrossHostnames(t *testing.T) {
	t.Setenv(platform.HubNameVar, "cluster-qualif")
	a := LauncherID()
	b := LauncherID()
	if a != b {
		t.Fatalf("identité instable : %q puis %q", a, b)
	}
}

// #169 : CL_CACHE_DIR utilisé tel quel, créé s'il manque, cache des milestones compris.
func TestCacheDirEnvVarOverride(t *testing.T) {
	d := filepath.Join(t.TempDir(), "etat", "hub-a")
	t.Setenv(CacheDirVar, d)
	if got := CacheDir(); got != d {
		t.Errorf("CacheDir() = %q, attendu %q (tel quel)", got, d)
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		t.Errorf("CL_CACHE_DIR non créé : %v", err)
	}
	if got := MsCachePath(); got != filepath.Join(d, "milestones-v2.json") {
		t.Errorf("MsCachePath() = %q", got)
	}
	t.Setenv(CacheDirVar, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if CacheDir() == d || filepath.Base(CacheDir()) != "claude-launcher-ng2" {
		t.Errorf("sans CL_CACHE_DIR : %q (v6.2.0 : <cache>/claude-launcher-ng2)", CacheDir())
	}
}

// #169 : détection en cluster et refus du backend host.
func TestInClusterAndHostBackendRefusal(t *testing.T) {
	host := BackendConf{Name: "host", Type: "host"}
	// refus POUR LA RAISON « en cluster » : sous Windows, host est de toute façon refusé (claude ne tourne pas sur le
	// poste) — c'est le motif du refus qui prouve la règle du cluster, pas le simple fait d'être refusé
	clusterRefused := func(b BackendConf) bool {
		p, refused := LocalRefusal(b)
		return refused && p.What == hostBackendInCluster
	}
	t.Setenv(InClusterVar, "0")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if InCluster() {
		t.Error("CL_IN_CLUSTER=0 doit forcer « hors cluster »")
	}
	if clusterRefused(host) {
		t.Error("host refusé « dans le pod du launcher » hors cluster")
	}
	t.Setenv(InClusterVar, "1")
	if !InCluster() {
		t.Error("CL_IN_CLUSTER=1 doit forcer « en cluster »")
	}
	p, refused := LocalRefusal(host)
	if !refused || p.What != "backend host refusé dans le pod du launcher (claude n'y est pas installé)" {
		t.Errorf("host en cluster : %+v refusé=%v", p, refused)
	}
	if _, refused := LocalRefusal(BackendConf{Name: "lab", Type: "k8s"}); refused {
		t.Error("un backend k8s ne doit pas être refusé en cluster")
	}
	t.Setenv(AllowHostBackendVar, "1")
	if clusterRefused(host) {
		t.Error("CL_ALLOW_HOST_BACKEND=1 doit lever le refus « dans le pod du launcher »")
	}
	if runtime.GOOS != "windows" {
		if _, refused := LocalRefusal(host); refused {
			t.Error("CL_ALLOW_HOST_BACKEND=1 doit autoriser host (hors Windows)")
		}
	}
	// détection automatique : variable d'environnement seule ne suffit pas sans jeton lisible
	t.Setenv(InClusterVar, "")
	if _, err := os.Stat(saTokenPath); err != nil && InCluster() {
		t.Error("KUBERNETES_SERVICE_HOST sans jeton de ServiceAccount lisible : pas en cluster")
	}
}
