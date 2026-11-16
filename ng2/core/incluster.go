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
