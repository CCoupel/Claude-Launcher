package core

import (
	"os"
	"strings"
)

// ── Launcher dans un cluster (#168, #169) ─────────────────────────────────
//
// Détection : CL_IN_CLUSTER=1|0 la force ; sinon KUBERNETES_SERVICE_HOST non vide ET jeton du ServiceAccount
// lisible. Les tests fixent CL_IN_CLUSTER=0 : le conteneur de développement est lui-même un pod. Contrat :
// contracts/launcher-env.md.

const (
	// InClusterVar force la détection (« 1 » ou « 0 ») ; AllowHostBackendVar autorise le backend host en cluster.
	InClusterVar         = "CL_IN_CLUSTER"
	AllowHostBackendVar  = "CL_ALLOW_HOST_BACKEND"
	saTokenPath          = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	K8sVolumesVar        = "CL_K8S_VOLUMES"
	hostBackendInCluster = "backend host refusé dans le pod du launcher (claude n'y est pas installé)"
)

// InCluster : ce launcher tourne dans un pod Kubernetes.
func InCluster() bool {
	switch strings.TrimSpace(os.Getenv(InClusterVar)) {
	case "1":
		return true
	case "0":
		return false
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return false
	}
	f, err := os.Open(saTokenPath)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// AllowHostBackend : le backend host est autorisé même en cluster (CL_ALLOW_HOST_BACKEND=1).
func AllowHostBackend() bool { return strings.TrimSpace(os.Getenv(AllowHostBackendVar)) == "1" }

// inClusterHostRefusal : le backend host est refusé dans le pod du launcher, sauf CL_ALLOW_HOST_BACKEND=1.
func inClusterHostRefusal(b BackendConf) (Problem, bool) {
	if b.Type != "host" || !InCluster() || AllowHostBackend() {
		return Problem{}, false
	}
	return Problem{hostBackendInCluster, "choisir un backend k8s (ou un autre backend distant) ; CL_ALLOW_HOST_BACKEND=1 l'autorise pour un essai (" + BackendsPath() + ")"}, true
}

// saNamespacePath : namespace du pod (variable : tests).
var saNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// PodNamespace : namespace du pod du launcher ("" hors cluster ou illisible).
func PodNamespace() string {
	if !InCluster() {
		return ""
	}
	b, err := os.ReadFile(saNamespacePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// k8sNamespace : namespace effectif d'un backend k8s — celui du backend, sinon (en cluster) celui du pod, sinon « default ».
func k8sNamespace(b BackendConf) string {
	if b.Namespace != "" {
		return b.Namespace
	}
	if ns := PodNamespace(); ns != "" {
		return ns
	}
	return "default"
}

// k8sViaServiceAccount : kubectl s'appuie sur le ServiceAccount du pod (en cluster, backend sans « context ») :
// appelé sans --context et sans KUBECONFIG.
func k8sViaServiceAccount(b BackendConf) bool { return InCluster() && b.Context == "" }

// ParseK8sVolumes : « <racine>=<pvc>[,<racine>=<pvc>…] » (CL_K8S_VOLUMES) en volumes d'un backend k8s implicite.
func ParseK8sVolumes(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if r, pvc, ok := strings.Cut(strings.TrimSpace(kv), "="); ok && strings.TrimSpace(r) != "" && strings.TrimSpace(pvc) != "" {
			out[strings.TrimSpace(r)] = strings.TrimSpace(pvc)
		}
	}
	return out
}

// NetworkErrorText : le texte d'erreur d'un outil dit que le réseau est inaccessible (résolution, connexion, délai).
func NetworkErrorText(low string) bool {
	for _, k := range []string{"no such host", "dial tcp", "connection refused", "network is unreachable", "i/o timeout",
		"could not resolve host", "connection timed out", "tls handshake timeout", "temporary failure in name resolution"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

// NetBlockedHint : dans un pod, une sortie réseau coupée est la première cause d'un service « injoignable » (#175).
func NetBlockedHint() string {
	if !InCluster() {
		return ""
	}
	return " — les sorties réseau du pod vers claude.ai et GitHub sont-elles autorisées ? (NetworkPolicy, proxy, DNS du cluster)"
}
