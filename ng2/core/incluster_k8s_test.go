package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// #168 : namespace du pod, volumes implicites, kubectl via le ServiceAccount, montages du pod.
func TestInClusterK8sHelpers(t *testing.T) {
	ns := filepath.Join(t.TempDir(), "namespace")
	_ = os.WriteFile(ns, []byte("claude-ng2\n"), 0o644)
	old := saNamespacePath
	saNamespacePath = ns
	defer func() { saNamespacePath = old }()

	t.Setenv(InClusterVar, "0")
	if PodNamespace() != "" || k8sNamespace(BackendConf{}) != "default" {
		t.Errorf("hors cluster : %q / %q", PodNamespace(), k8sNamespace(BackendConf{}))
	}
	t.Setenv(InClusterVar, "1")
	if PodNamespace() != "claude-ng2" || k8sNamespace(BackendConf{}) != "claude-ng2" || k8sNamespace(BackendConf{Namespace: "autre"}) != "autre" {
		t.Errorf("en cluster : %q / %q", PodNamespace(), k8sNamespace(BackendConf{}))
	}
	// kubectl : sans --context ; KUBECONFIG de l'environnement écarté (env -u) ; avec context : inchangé
	t.Setenv("KUBECONFIG", "/x/kubeconfig")
	e := Engine{BackendConf{Type: "k8s"}}
	if got := e.kubectl("get", "pods"); !reflect.DeepEqual(got, []string{"env", "-u", "KUBECONFIG", "kubectl", "-n", "claude-ng2", "get", "pods"}) {
		t.Errorf("kubectl en cluster : %v", got)
	}
	e = Engine{BackendConf{Type: "k8s", Context: "lab"}}
	if got := e.kubectl("get"); got[0] != "kubectl" || got[1] != "--context" {
		t.Errorf("kubectl avec contexte : %v", got)
	}
	// CL_K8S_VOLUMES
	if got := ParseK8sVolumes("/data/p=claude-projets, /autre=pvc2,mal,=x,y="); !reflect.DeepEqual(got, map[string]string{"/data/p": "claude-projets", "/autre": "pvc2"}) {
		t.Errorf("CL_K8S_VOLUMES : %v", got)
	}
	// backend implicite sans backends.yaml
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv(K8sVolumesVar, "/data/p=claude-projets")
	bs, err := LoadBackends()
	if err != nil {
		t.Fatal(err)
	}
	var k *BackendConf
	for i := range bs {
		if bs[i].Name == "k8s" {
			k = &bs[i]
		}
	}
	if k == nil || k.Namespace != "claude-ng2" || k.Volumes["/data/p"] != "claude-projets" {
		t.Errorf("backend k8s implicite : %+v", k)
	}
}

func TestPodMounts(t *testing.T) {
	j := `{"spec":{"volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"claude-projets"}},{"name":"t","emptyDir":{}}],
	"containers":[{"volumeMounts":[{"name":"d","mountPath":"/data/p/"},{"name":"t","mountPath":"/tmp"}]}]}}`
	if got := podMounts(j); !reflect.DeepEqual(got, map[string]string{"/data/p": "claude-projets"}) {
		t.Errorf("montages : %v", got)
	}
	if len(podMounts("pas du json")) != 0 {
		t.Error("JSON illisible : aucun montage")
	}
}

func TestBackendsModelExposesCluster(t *testing.T) {
	ns := filepath.Join(t.TempDir(), "namespace")
	_ = os.WriteFile(ns, []byte("claude-ng2\n"), 0o644)
	old := saNamespacePath
	saNamespacePath = ns
	defer func() { saNamespacePath = old }()
	t.Setenv(InClusterVar, "1")
	if m, _ := ReadBackendsModel(nil); !m.InCluster || m.PodNamespace != "claude-ng2" {
		t.Errorf("en cluster : %+v", m)
	}
	t.Setenv(InClusterVar, "0")
	if m, _ := ReadBackendsModel(nil); m.InCluster || m.PodNamespace != "" {
		t.Errorf("hors cluster : %+v", m)
	}
}
