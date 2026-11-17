package core

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
	// en cluster, le contexte d'un backend est IGNORÉ (ServiceAccount du pod) : ni --context, ni KUBECONFIG
	e = Engine{BackendConf{Type: "k8s", Context: "k8s"}}
	if got := e.kubectl("get"); !reflect.DeepEqual(got, []string{"env", "-u", "KUBECONFIG", "kubectl", "-n", "claude-ng2", "get"}) {
		t.Errorf("kubectl avec contexte en cluster : %v", got)
	}
	if ctxArg(e.B) != "" {
		t.Errorf("conseil avec --context en cluster : %q", ctxArg(e.B))
	}
	// hors cluster : inchangé (--context, KUBECONFIG laissé)
	t.Setenv(InClusterVar, "0")
	if got := e.kubectl("get"); !reflect.DeepEqual(got, []string{"kubectl", "--context", "k8s", "-n", "default", "get"}) || ctxArg(e.B) != " --context k8s" {
		t.Errorf("kubectl avec contexte hors cluster : %v / %q", got, ctxArg(e.B))
	}
	t.Setenv(InClusterVar, "1")
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

// Revue M3 : « no » = droit manquant (conseil du chart) ; erreur de l'appel = « non vérifiable » (pas de conseil
// helm), et le contrôle s'arrête à la première erreur (pas un délai par droit).
func TestK8sInClusterRightsDistinguishNoFromError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux kubectl en shell")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"case \"$*\" in\n" +
		"  *'list pods'*) echo yes;;\n" +
		"  *'create statefulsets'*) echo no; exit 1;;\n" +
		"  *'get pods'*) echo 'Unable to connect to the server: dial tcp 10.0.0.1:443: i/o timeout' >&2; exit 1;;\n" +
		"  *) echo yes;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(InClusterVar, "1")
	steps := k8sSteps(BackendConf{Type: "k8s", Namespace: "claude-ng2"})
	var missing, unchecked *TestStep
	for i := range steps {
		switch {
		case strings.Contains(steps[i].Detail, "droit manquant : create statefulsets"):
			missing = &steps[i]
		case strings.Contains(steps[i].Detail, "droit non vérifiable : get pods"):
			unchecked = &steps[i]
		}
	}
	if missing == nil || !strings.Contains(missing.Fix, "helm upgrade") {
		t.Errorf("« no » : droit manquant avec le conseil du chart attendu : %+v", steps)
	}
	if unchecked == nil || strings.Contains(unchecked.Fix, "helm upgrade") || !strings.Contains(unchecked.Detail, "i/o timeout") {
		t.Errorf("erreur d'appel : « non vérifiable » sans conseil helm attendu : %+v", unchecked)
	}
	b, _ := os.ReadFile(calls)
	if n := strings.Count(string(b), "can-i"); n > 9 { // list pods + 5 statefulsets + scale + get pods = 8
		t.Errorf("%d appels can-i : le contrôle doit s'arrêter à la première erreur", n)
	}
	if strings.Contains(string(b), "pods/exec") || strings.Contains(string(b), "get pvc") {
		t.Errorf("les droits et volumes suivants ne doivent pas être contrôlés après l'erreur : %s", b)
	}
}

// En cluster, le diagnostic d'un backend avec « context » le dit (information) et appelle kubectl sans --context.
func TestK8sInClusterIgnoresContextInDiagnostic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux kubectl en shell")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\ncase \"$*\" in *'get pod'*) echo '{}';; *'get pvc'*) echo 'Bound [\"ReadWriteMany\"]';; *) echo yes;; esac\n"
	_ = os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECONFIG", "/x/kubeconfig")
	t.Setenv(InClusterVar, "1")
	steps := k8sSteps(BackendConf{Type: "k8s", Context: "k8s", Namespace: "claude-ng2"})
	found := false
	for _, st := range steps {
		if strings.Contains(st.Detail, "contexte « k8s » ignoré : ServiceAccount du pod") && st.OK {
			found = true
		}
	}
	if !found {
		t.Errorf("information « contexte ignoré » absente : %+v", steps)
	}
	b, _ := os.ReadFile(calls)
	if strings.Contains(string(b), "--context") || strings.Contains(string(b), "KUBECONFIG") {
		t.Errorf("kubectl appelé avec le contexte : %s", b)
	}
}

// QUALIF 6.3.0.7 : « kubectl auth can-i create pods/exec » est lu comme « le pod nommé exec » (faux négatif) et
// « patch statefulsets/scale » comme « le StatefulSet nommé scale » : les sous-ressources se vérifient par --subresource.
func TestK8sRightsUseSubresourceFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux kubectl en shell")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	// comme le vrai kubectl : la forme TYPE/NOM positionnelle est un NOM de ressource, refusé par le Role ; --subresource accordé
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"case \"$*\" in\n" +
		"  *pods/exec*|*statefulsets/scale*) echo no; exit 1;;\n" +
		"  *get\\ pod*-o\\ json*) echo '{}';;\n" +
		"  *get\\ pvc*) echo 'Bound [\"ReadWriteMany\"]';;\n" +
		"  *) echo yes;;\n" +
		"esac\n"
	_ = os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(InClusterVar, "1")
	steps := k8sSteps(BackendConf{Type: "k8s", Namespace: "claude-qualif"})
	for _, st := range steps {
		if strings.Contains(st.Detail, "droit manquant") || strings.Contains(st.Detail, "non vérifiable") {
			t.Errorf("faux droit manquant : %+v", st)
		}
	}
	b, _ := os.ReadFile(calls)
	for _, want := range []string{"--subresource=exec", "--subresource=scale"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%s absent des appels kubectl :\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "pods/exec") || strings.Contains(string(b), "statefulsets/scale") {
		t.Errorf("sous-ressource passée en forme TYPE/NOM :\n%s", b)
	}
	// l'affichage garde la forme lisible
	if rightLabel([3]string{"create", "pods", "exec"}) != "create pods/exec" {
		t.Errorf("libellé : %q", rightLabel([3]string{"create", "pods", "exec"}))
	}
}
