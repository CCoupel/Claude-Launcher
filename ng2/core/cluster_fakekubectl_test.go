package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/platform"
)

// LOT-4.1 (#168, #167, #175) — diagnostic d'un backend k8s depuis le pod du launcher avec un FAUX kubectl (script
// placé en tête du PATH) : sans contexte ni KUBECONFIG, namespace du pod, chaque droit manquant nommé, volumes,
// montages dans le pod du launcher. Contrat : contracts/launcher-env.md (« Launcher dans un cluster »). HOME factice.

// twFakeKubectl : installe un faux kubectl. Il journalise (arguments + valeur de KUBECONFIG) dans le fichier rendu et
// répond d'après les fichiers du dossier : deny (« verbe ressource » par ligne : réponse « no »), down (existe : API
// injoignable), pod.json (get pod), pvc (get pvc : phase et modes d'accès).
func twFakeKubectl(t *testing.T) (dir, log string) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	log = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
D="` + dir + `"
echo "KUBECONFIG=[$KUBECONFIG] $*" >> "$D/calls.log"
if [ -e "$D/down" ]; then echo "Unable to connect to the server: dial tcp 10.0.0.1:443: i/o timeout" >&2; exit 1; fi
while [ "$1" = "-n" ] || [ "$1" = "--context" ] || [ "$1" = "--kubeconfig" ]; do shift 2; done
case "$1 $2" in
  "auth can-i")
    K="$3 $4"
    case "$5" in --subresource=*) K="$3 $4/${5#--subresource=}";; esac
    if grep -qx "$K" "$D/deny" 2>/dev/null; then echo no; exit 1; fi
    echo yes ;;
  "get pod") cat "$D/pod.json" 2>/dev/null || exit 1 ;;
  "get pvc") cat "$D/pvc" 2>/dev/null || exit 1 ;;
  *) echo "{}" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, log
}

func twClusterEnv(t *testing.T) {
	t.Helper()
	withHome(t)
	ns := filepath.Join(t.TempDir(), "namespace")
	_ = os.WriteFile(ns, []byte("claude-ng2\n"), 0o644)
	old := saNamespacePath.Get()
	saNamespacePath.Set(ns)
	t.Cleanup(func() { saNamespacePath.Set(old) })
	t.Setenv(InClusterVar, "1")
	t.Setenv("KUBECONFIG", "/ne/doit/jamais/servir")
}

func twSteps(b BackendConf) []TestStep {
	return k8sStepsInCluster(b, Engine{b}, k8sNamespace(b), nil)
}

func twDetails(ss []TestStep) string {
	var s []string
	for _, x := range ss {
		s = append(s, x.Label+" | "+x.Detail+" | "+x.Fix)
	}
	return strings.Join(s, "\n")
}

// ServiceAccount du pod : kubectl sans --context, sans KUBECONFIG, dans le namespace du pod ; tout accordé = tout vert.
func TestClusterDiagUsesPodServiceAccountAndNamespace(t *testing.T) {
	twClusterEnv(t)
	dir, log := twFakeKubectl(t)
	_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteMany]"), 0o644)
	self := platform.RealHostName()
	_ = os.WriteFile(filepath.Join(dir, "pod.json"), []byte(`{"spec":{"volumes":[{"name":"d","persistentVolumeClaim":{"claimName":"claude-projets"}}],"containers":[{"volumeMounts":[{"name":"d","mountPath":"/data/p"}]}]}}`), 0o644)
	b := BackendConf{Name: "k8s", Type: "k8s", Volumes: map[string]string{"/data/p": "claude-projets"}}
	steps := twSteps(b)
	for _, s := range steps {
		if !s.OK {
			t.Errorf("étape en échec alors que tout est accordé : %s | %s", s.Label, s.Detail)
		}
	}
	calls, _ := os.ReadFile(log)
	if len(calls) == 0 {
		t.Fatal("le faux kubectl n'a pas été appelé")
	}
	for _, l := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		if strings.Contains(l, "--context") {
			t.Errorf("appel avec --context en cluster : %s", l)
		}
		if !strings.Contains(l, "KUBECONFIG=[]") {
			t.Errorf("KUBECONFIG de l'environnement transmis à kubectl : %s", l)
		}
		if !strings.Contains(l, "-n claude-ng2") {
			t.Errorf("appel hors du namespace du pod : %s", l)
		}
	}
	if !strings.Contains(string(calls), "get pod "+self) {
		t.Errorf("le pod du launcher (%s) n'est pas lu pour contrôler les montages :\n%s", self, calls)
	}
}

// Chaque droit manquant est nommé (« droit manquant : <verbe> <ressource> ») avec le conseil « helm upgrade ».
func TestClusterDiagNamesEveryMissingRight(t *testing.T) {
	twClusterEnv(t)
	dir, _ := twFakeKubectl(t)
	_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteMany]"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "deny"), []byte("create pods/exec\ndelete statefulsets\nlist events\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "pod.json"), []byte(`{"spec":{"volumes":[],"containers":[]}}`), 0o644)
	steps := twSteps(BackendConf{Name: "k8s", Type: "k8s"})
	got := twDetails(steps)
	for _, want := range []string{"droit manquant : create pods/exec", "droit manquant : delete statefulsets", "droit manquant : list events"} {
		if !strings.Contains(got, want) {
			t.Errorf("« %s » absent :\n%s", want, got)
		}
	}
	if !strings.Contains(got, "helm upgrade") {
		t.Errorf("conseil « mettre à jour le Role du chart (helm upgrade) » absent :\n%s", got)
	}
	// un droit accordé n'est pas nommé manquant
	for _, ok := range []string{"get statefulsets", "list pods", "create statefulsets"} {
		if strings.Contains(got, "droit manquant : "+ok) {
			t.Errorf("droit accordé (%s) déclaré manquant", ok)
		}
	}
}

// API injoignable : « cluster injoignable (ServiceAccount du pod) », jamais « contexte « » ».
func TestClusterDiagUnreachableMessage(t *testing.T) {
	twClusterEnv(t)
	dir, _ := twFakeKubectl(t)
	_ = os.WriteFile(filepath.Join(dir, "down"), []byte("x"), 0o644)
	got := twDetails(twSteps(BackendConf{Name: "k8s", Type: "k8s"}))
	if !strings.Contains(got, "cluster injoignable (ServiceAccount du pod)") {
		t.Errorf("message attendu absent :\n%s", got)
	}
	if strings.Contains(got, "contexte") {
		t.Errorf("message de contexte (inadapté en cluster) :\n%s", got)
	}
}

// PVC non monté dans le pod du launcher : message du contrat.
func TestClusterDiagPVCNotMountedInLauncherPod(t *testing.T) {
	twClusterEnv(t)
	dir, _ := twFakeKubectl(t)
	_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteMany]"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "pod.json"), []byte(`{"spec":{"volumes":[],"containers":[{"volumeMounts":[]}]}}`), 0o644)
	got := twDetails(twSteps(BackendConf{Name: "k8s", Type: "k8s", Volumes: map[string]string{"/data/p": "claude-projets"}}))
	if !strings.Contains(got, "volume claude-projets non monté dans le pod du launcher à /data/p") {
		t.Errorf("message de montage absent :\n%s", got)
	}
	// PVC non ReadWriteMany : refusé
	_ = os.WriteFile(filepath.Join(dir, "pvc"), []byte("Bound [ReadWriteOnce]"), 0o644)
	if got := twDetails(twSteps(BackendConf{Name: "k8s", Type: "k8s", Volumes: map[string]string{"/data/p": "claude-projets"}})); !strings.Contains(got, "ReadWriteMany") {
		t.Errorf("PVC ReadWriteOnce non signalé :\n%s", got)
	}
}

// Backend implicite en cluster, sans backends.yaml : k8s, namespace du pod, volumes de CL_K8S_VOLUMES ; hors cluster : absent.
func TestImplicitK8sBackendOnlyInCluster(t *testing.T) {
	twClusterEnv(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv(K8sVolumesVar, "/data/p=claude-projets,/data/q=pvc-q")
	find := func() *BackendConf {
		bs, err := LoadBackends()
		if err != nil {
			t.Fatal(err)
		}
		for i := range bs {
			if bs[i].Type == "k8s" {
				return &bs[i]
			}
		}
		return nil
	}
	k := find()
	if k == nil || k.Namespace != "claude-ng2" || k.Volumes["/data/p"] != "claude-projets" || k.Volumes["/data/q"] != "pvc-q" {
		t.Fatalf("backend k8s implicite : %+v", k)
	}
	t.Setenv(InClusterVar, "0")
	if k := find(); k != nil && k.Namespace == "claude-ng2" {
		t.Errorf("backend implicite du pod proposé hors cluster : %+v", k)
	}
}

// ── #167 / #175 ──────────────────────────────────────────────────────────────

func TestUpdateModeNotifyOnlyValues(t *testing.T) {
	for v, want := range map[string]bool{"notify": true, " notify ": true, "auto": false, "": false, "NOTIFY": false, "off": false} {
		t.Setenv("CL_UPDATE_MODE", v)
		if got := UpdateNotifyOnly(); got != want {
			t.Errorf("CL_UPDATE_MODE=%q : %v, attendu %v", v, got, want)
		}
	}
}

// #175 : indice « sorties réseau » seulement en cluster ; reconnaissance des erreurs réseau usuelles.
func TestNetBlockedHintOnlyInCluster(t *testing.T) {
	withHome(t)
	t.Setenv(InClusterVar, "0")
	if h := NetBlockedHint(); h != "" {
		t.Errorf("hors cluster : %q, attendu vide", h)
	}
	t.Setenv(InClusterVar, "1")
	h := NetBlockedHint()
	for _, w := range []string{"claude.ai", "GitHub", "NetworkPolicy"} {
		if !strings.Contains(h, w) {
			t.Errorf("indice en cluster sans « %s » : %q", w, h)
		}
	}
	for _, e := range []string{"dial tcp 1.2.3.4:443: i/o timeout", "no such host", "connection refused", "temporary failure in name resolution"} {
		if !NetworkErrorText(strings.ToLower(e)) {
			t.Errorf("erreur réseau non reconnue : %q", e)
		}
	}
	if NetworkErrorText("permission denied") || NetworkErrorText("") {
		t.Errorf("faux positif d'erreur réseau")
	}
}
