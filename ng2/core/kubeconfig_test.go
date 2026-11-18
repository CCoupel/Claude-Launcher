package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const kubeOK = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: c1
  cluster: {server: "https://10.0.0.1:6443", certificate-authority-data: QUJD}
contexts:
- name: prod
  context: {cluster: c1, user: u1}
- name: test
  context: {cluster: c1, user: u1}
users:
- name: u1
  user: {token: SECRET-TOKEN-193}
`

func TestKubeconfigYAML(t *testing.T) {
	var f backendsFile
	src := "backends:\n  a: {type: k8s, kubeconfig: true}\n  b: {type: k8s, kubeconfig: /etc/kc/config}\n  c: {type: k8s}\n"
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if !f.Backends["a"].Kubeconfig.Vault || f.Backends["b"].Kubeconfig.Path != "/etc/kc/config" || f.Backends["c"].Kubeconfig.set() {
		t.Errorf("décodage : %+v", f.Backends)
	}
	// non k8s : refusé ; chemin relatif refusé
	for _, bad := range []string{"backends:\n  x: {type: ssh, host: h, kubeconfig: true}\n", "backends:\n  x: {type: k8s, kubeconfig: rel/path}\n"} {
		var g backendsFile
		_ = yaml.Unmarshal([]byte(bad), &g)
		if _, err := backendsOf(g); err == nil {
			t.Errorf("accepté : %s", bad)
		}
	}
	// modèle de l'éditeur : true / chemin
	m, err := ReadBackendsModel([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]BackendEntry{} // sous Windows, un backend « host » implicite est ajouté en tête
	for _, b := range m.Backends {
		byName[b.Name] = b
	}
	if byName["a"].Kubeconfig != true || byName["b"].Kubeconfig != "/etc/kc/config" || byName["c"].Kubeconfig != nil {
		t.Errorf("modèle : %+v", m.Backends)
	}
}

func TestKubectlAvecKubeconfig(t *testing.T) {
	d := secretsEnv(t)
	t.Setenv("CL_IN_CLUSTER", "1")
	t.Setenv("KUBECONFIG", "/env/kubeconfig")
	// sans kubeconfig du backend : ServiceAccount, contexte ignoré, KUBECONFIG retiré
	b := BackendConf{Name: "k", Type: "k8s", Context: "prod", Namespace: "ns"}
	if a := strings.Join(Engine{b}.kubectl("get", "pods"), " "); strings.Contains(a, "--kubeconfig") || strings.Contains(a, "--context") || !strings.HasPrefix(a, "env -u KUBECONFIG") {
		t.Errorf("ServiceAccount : %s", a)
	}
	// coffre : --kubeconfig et contexte honoré même en cluster
	b.Kubeconfig = KubeconfigRef{Vault: true}
	a := strings.Join(Engine{b}.kubectl("get", "pods"), " ")
	if !strings.Contains(a, "--kubeconfig "+filepath.Join(d, "k", "kubeconfig")) || !strings.Contains(a, "--context prod") {
		t.Errorf("kubeconfig du coffre : %s", a)
	}
	// chemin (absolu sous le système courant : /… n'est pas absolu sous Windows)
	kc := filepath.Join(t.TempDir(), "kc", "config")
	b.Kubeconfig = KubeconfigRef{Path: kc}
	if a := strings.Join(Engine{b}.kubectl("get"), " "); !strings.Contains(a, "--kubeconfig "+kc) {
		t.Errorf("kubeconfig chemin : %s", a)
	}
	// chemin sous une zone partagée : refusé, mais --kubeconfig reste (aucun repli)
	shared := t.TempDir()
	t.Setenv("CL_ROOT_BASE", shared)
	b.Kubeconfig = KubeconfigRef{Path: filepath.Join(shared, "kc")}
	if a := (Engine{b}).kubectl("get"); len(a) != 1 || !strings.HasPrefix(a[0], "kubeconfig-refuse") || !strings.Contains(a[0], "zone partagée") {
		t.Errorf("chemin partagé : kubectl ne doit pas être lancé : %v", a)
	}
}

func TestKubeconfigDepotEtRefus(t *testing.T) {
	d := secretsEnv(t)
	ctxs, fp, _, err := SecretSetKubeconfig("k8", kubeOK, SecretWrite{Source: "t"})
	if err != nil || len(ctxs) != 2 || ctxs[0] != "prod" || !strings.HasPrefix(fp, "sha256:") {
		t.Fatalf("dépôt : %v %v %q", ctxs, err, fp)
	}
	info, _ := SecretGet("k8")
	b, _ := json.Marshal(info)
	if !info.HasKubeconfig || len(info.KubeContexts) != 2 || strings.Contains(string(b), "SECRET-TOKEN-193") {
		t.Errorf("Get : %s", b)
	}
	if st, err := os.Stat(filepath.Join(d, "k8", "kubeconfig")); err != nil || (st.Mode().Perm() != 0o600 && os.PathSeparator == '/') {
		t.Errorf("fichier : %v %v", st, err)
	}
	j, _ := os.ReadFile(filepath.Join(CacheDir(), secretJournal))
	if strings.Contains(string(j), "SECRET-TOKEN-193") || !strings.Contains(string(j), "kubeconfig") {
		t.Errorf("journal : %s", j)
	}
	for name, bad := range map[string]string{
		"exec":      strings.Replace(kubeOK, "{token: SECRET-TOKEN-193}", "{exec: {command: /bin/evil}}", 1),
		"provider":  strings.Replace(kubeOK, "{token: SECRET-TOKEN-193}", "{auth-provider: {name: gcp}}", 1),
		"tokenFile": strings.Replace(kubeOK, "{token: SECRET-TOKEN-193}", "{tokenFile: /var/run/secrets/token}", 1),
		"ca":        strings.Replace(kubeOK, "certificate-authority-data: QUJD", "certificate-authority: /etc/ca", 1),
		"casse":     strings.Replace(kubeOK, "{token: SECRET-TOKEN-193}", "{Exec: {command: /bin/evil}}", 1),
		"casse2":    strings.Replace(strings.Replace(kubeOK, "users:", "Users:", 1), "{token: SECRET-TOKEN-193}", "{ExEc: {command: x}}", 1),
		"vide":      "n'importe quoi",
	} {
		if _, _, _, err := SecretSetKubeconfig("k8", bad, SecretWrite{}); err == nil {
			t.Errorf("%s : kubeconfig accepté", name)
		}
	}
	if err := SecretDelete("k8", "kubeconfig", SecretWrite{}); err != nil {
		t.Fatal(err)
	}
	if info, _ := SecretGet("k8"); info.HasKubeconfig {
		t.Error("kubeconfig encore présent")
	}
}

func TestKubeconfigEtapesTest(t *testing.T) {
	d := secretsEnv(t)
	b := BackendConf{Name: "k8", Type: "k8s", Kubeconfig: KubeconfigRef{Vault: true}}
	// absent : illisible, sans repli
	if st, ok := kubeconfigSteps(b); ok || st[0].OK || !strings.Contains(st[0].Detail, "illisible") {
		t.Errorf("absent : %+v", st)
	}
	_ = os.MkdirAll(filepath.Join(d, "k8"), 0o700)
	_ = os.WriteFile(filepath.Join(d, "k8", "kubeconfig"), []byte(kubeOK), 0o600)
	if st, ok := kubeconfigSteps(b); !ok || !st[0].OK || st[0].Label != "kubeconfig lisible" {
		t.Errorf("lisible : %+v", st)
	}
	b.Context = "inconnu"
	if st, ok := kubeconfigSteps(b); ok || !strings.Contains(st[0].Detail, "absent du kubeconfig") {
		t.Errorf("contexte absent : %+v", st)
	}
}

func TestKubeconfigAvertissements(t *testing.T) {
	secretsEnv(t)
	kc := strings.Replace(kubeOK, "certificate-authority-data: QUJD", "insecure-skip-tls-verify: true, proxy-url: 'http://px:3128'", 1)
	_, _, w, err := SecretSetKubeconfig("k8", kc, SecretWrite{})
	if err != nil || len(w) != 2 || !strings.Contains(strings.Join(w, "|"), "insecure-skip-tls-verify") || !strings.Contains(strings.Join(w, "|"), "proxy-url") {
		t.Errorf("avertissements : %v %v", w, err)
	}
	if _, _, w, _ := SecretSetKubeconfig("k8", kubeOK, SecretWrite{}); len(w) != 0 {
		t.Errorf("kubeconfig sain : avertissements %v", w)
	}
}

// Un fichier posé hors du launcher (chemin) est contrôlé à chaque appel de kubectl, pas seulement au dépôt.
func TestKubectlRefuseKubeconfigCheminDangereux(t *testing.T) {
	secretsEnv(t)
	f := filepath.Join(t.TempDir(), "kc")
	_ = os.WriteFile(f, []byte(strings.Replace(kubeOK, "{token: SECRET-TOKEN-193}", "{exec: {command: /bin/evil}}", 1)), 0o600)
	b := BackendConf{Name: "k", Type: "k8s", Kubeconfig: KubeconfigRef{Path: f}}
	a := (Engine{b}).kubectl("get", "pods")
	if len(a) != 1 || !strings.HasPrefix(a[0], "kubeconfig-refuse") || !strings.Contains(a[0], "exec") {
		t.Fatalf("kubectl lancé avec un kubeconfig dangereux : %v", a)
	}
	// la commande de remplacement échoue avec la raison, sans rien exécuter
	if err := (Engine{b}).kcmd("get").Run(); err == nil || !strings.Contains(err.Error(), "kubeconfig-refuse") {
		t.Errorf("erreur d'exécution : %v", err)
	}
	// et le test du backend le dit
	if st, ok := kubeconfigSteps(b); ok || !strings.Contains(st[0].Detail, "exec") {
		t.Errorf("étape : %+v", st)
	}
}

// pathWithin compare les formes réelles : un chemin à créer sous une racine existante reste « dedans » même si l'un des
// deux passe par un lien (sous Windows : nom court 8.3 contre nom long), et le refus d'un coffre sous une zone partagée en dépend.
func TestPathWithinFormesReelles(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "lien")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("liens symboliques indisponibles : ", err)
	}
	if !pathWithin(filepath.Join(link, "coffre", "sous"), real) {
		t.Error("chemin à créer sous un lien vers la racine : doit être dedans")
	}
	if !pathWithin(filepath.Join(real, "coffre"), link) {
		t.Error("racine donnée par un lien : doit contenir le chemin réel")
	}
	if pathWithin(filepath.Join(t.TempDir(), "coffre"), real) {
		t.Error("chemin hors racine jugé dedans")
	}
	if pathWithin(filepath.Join(filepath.Dir(real), "voisin"), real) {
		t.Error("dossier voisin jugé dedans")
	}
}
