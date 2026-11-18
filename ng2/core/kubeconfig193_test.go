package core

// Tests de spécification #193 (v6.4.0) — contracts/backends-remote.md « kubeconfig d'un backend k8s » : priorité
// kubeconfig du backend > ServiceAccount du pod > KUBECONFIG, --kubeconfig / --context vérifiés par un FAUX kubectl,
// fichier illisible = erreur sans repli, « Tester » (kubeconfig lisible puis auth can-i). Boîte noire : backends.yaml
// → LoadBackends → Engine.kubectl / TestBackend. Écrits avant l'implémentation (TDD) ; HOME et coffre factices.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kubeYAML193 = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: c1
  cluster: {server: "https://10.0.0.1:6443"}
users:
- name: u1
  user: {token: "tok-193-secret"}
contexts:
- name: prod
  context: {cluster: c1, user: u1, namespace: claude-qualif-extra}
- name: staging
  context: {cluster: c1, user: u1}
`

// k8sBackend193 : poste factice, faux kubectl, coffre 0700 ; charge le backend « autre » décrit par yamlBackend.
func k8sBackend193(t *testing.T, yamlBackend string) (b BackendConf, log, vault string) {
	t.Helper()
	h := withHome(t)
	_, log = twFakeKubectl(t)
	vault = filepath.Join(t.TempDir(), "coffre")
	if err := os.MkdirAll(vault, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SecretsDirVar, vault)
	t.Setenv("KUBECONFIG", "/ne/doit/jamais/servir")
	cfg := filepath.Join(h, ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(cfg, 0o755)
	t.Setenv("CL_CONFIG_DIR", cfg)
	if err := os.WriteFile(BackendsPath(), []byte("backends:\n  autre:\n"+yamlBackend), 0o644); err != nil {
		t.Fatal(err)
	}
	bs, err := LoadBackends()
	if err != nil {
		t.Fatalf("backends.yaml refusé : %v", err)
	}
	for _, x := range bs {
		if x.Name == "autre" {
			return x, log, vault
		}
	}
	t.Fatal("backend « autre » absent")
	return
}

func writeKube193(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(kubeYAML193), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// run193 : exécute la commande kubectl du moteur et rend la dernière ligne du journal du faux kubectl.
func run193(t *testing.T, b BackendConf, log string) string {
	t.Helper()
	_ = os.Remove(log)
	if err := (Engine{b}).kcmd("get", "pods").Run(); err != nil {
		t.Fatalf("kubectl : %v", err)
	}
	raw, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return lines[len(lines)-1]
}

func TestKubeconfigCheminPrioriteSurKUBECONFIG193(t *testing.T) {
	kc := writeKube193(t, filepath.Join(t.TempDir(), "kube", "config"))
	b, log, _ := k8sBackend193(t, "    type: k8s\n    namespace: claude-qualif-extra\n    kubeconfig: "+kc+"\n    context: prod\n")
	line := run193(t, b, log)
	for _, want := range []string{"--kubeconfig " + kc, "--context prod", "-n claude-qualif-extra", "KUBECONFIG=[]"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q absent de l'appel kubectl : %s", want, line)
		}
	}
}

func TestKubeconfigPrioriteSurServiceAccountEnCluster193(t *testing.T) {
	kc := writeKube193(t, filepath.Join(t.TempDir(), "kube", "config"))
	b, log, _ := k8sBackend193(t, "    type: k8s\n    namespace: claude-qualif-extra\n    context: prod\n    kubeconfig: "+kc+"\n")
	twClusterEnv(t) // pod : ServiceAccount, KUBECONFIG factice
	t.Setenv(SecretsDirVar, os.Getenv(SecretsDirVar))
	line := run193(t, b, log)
	for _, want := range []string{"--kubeconfig " + kc, "--context prod", "KUBECONFIG=[]"} {
		if !strings.Contains(line, want) {
			t.Errorf("en cluster, le kubeconfig du backend prime sur le ServiceAccount et son contexte est honoré : %q absent de %s", want, line)
		}
	}
}

func TestSansKubeconfigComportementV630193(t *testing.T) {
	b, log, _ := k8sBackend193(t, "    type: k8s\n    namespace: claude-qualif-extra\n    context: prod\n")
	line := run193(t, b, log) // hors cluster : contexte + KUBECONFIG de l'environnement, comme en v6.3.0
	if strings.Contains(line, "--kubeconfig") || !strings.Contains(line, "--context prod") || !strings.Contains(line, "KUBECONFIG=[/ne/doit/jamais/servir]") {
		t.Errorf("hors cluster sans kubeconfig : %s", line)
	}
	twClusterEnv(t)
	line = run193(t, b, log) // en cluster : ServiceAccount, ni --kubeconfig ni --context ni KUBECONFIG
	if strings.Contains(line, "--kubeconfig") || strings.Contains(line, "--context") || !strings.Contains(line, "KUBECONFIG=[]") {
		t.Errorf("en cluster sans kubeconfig : %s", line)
	}
}

func TestKubeconfigDuCoffre193(t *testing.T) {
	b, log, vault := k8sBackend193(t, "    type: k8s\n    namespace: claude-qualif-extra\n    kubeconfig: true\n")
	want := filepath.Join(vault, "autre", "kubeconfig")
	writeKube193(t, want)
	if line := run193(t, b, log); !strings.Contains(line, "--kubeconfig "+want) {
		t.Errorf("« kubeconfig: true » doit viser le fichier du coffre %s : %s", want, line)
	}
}

func hasStep193(steps []TestStep, label string, ok bool) bool {
	for _, s := range steps {
		if strings.Contains(strings.ToLower(s.Label+" "+s.Detail), strings.ToLower(label)) && s.OK == ok { // libellé ou détail
			return true
		}
	}
	return false
}

func TestTesterKubeconfigLisibleEtAuthCanI193(t *testing.T) {
	kc := writeKube193(t, filepath.Join(t.TempDir(), "kube", "config"))
	b, log, _ := k8sBackend193(t, "    type: k8s\n    namespace: claude-qualif-extra\n    kubeconfig: "+kc+"\n")
	twClusterEnv(t)
	steps := TestBackend(b)
	if !hasStep193(steps, "kubeconfig lisible", true) {
		t.Errorf("étape « kubeconfig lisible » OK absente : %+v", steps)
	}
	raw, _ := os.ReadFile(log)
	if !strings.Contains(string(raw), "auth can-i") || !strings.Contains(string(raw), "--kubeconfig "+kc) {
		t.Errorf("auth can-i doit être joué avec ce kubeconfig : %s", raw)
	}
}

func TestKubeconfigIllisibleSansRepli193(t *testing.T) {
	for _, c := range []struct{ name, yaml string }{
		{"chemin inexistant", "    type: k8s\n    namespace: x\n    kubeconfig: /ne/pas/ici/config\n"},
		{"coffre vide", "    type: k8s\n    namespace: x\n    kubeconfig: true\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, log, _ := k8sBackend193(t, c.yaml)
			twClusterEnv(t) // le ServiceAccount existerait : il ne doit PAS servir de repli
			_ = os.Remove(log)
			steps := TestBackend(b)
			if !hasStep193(steps, "kubeconfig illisible", false) {
				t.Errorf("erreur « kubeconfig illisible sur ce poste » attendue : %+v", steps)
			}
			if raw, err := os.ReadFile(log); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
				t.Errorf("kubectl appelé malgré le kubeconfig illisible (repli interdit) : %s", raw)
			}
		})
	}
}

func TestKubeconfigNeFuitPasDansLesEtapes193(t *testing.T) {
	kc := writeKube193(t, filepath.Join(t.TempDir(), "kube", "config"))
	b, _, _ := k8sBackend193(t, "    type: k8s\n    namespace: x\n    kubeconfig: "+kc+"\n")
	twClusterEnv(t)
	for _, s := range TestBackend(b) {
		if strings.Contains(s.Label+s.Detail+s.Fix, "tok-193-secret") {
			t.Errorf("le jeton du kubeconfig figure dans une étape : %+v", s)
		}
	}
}
