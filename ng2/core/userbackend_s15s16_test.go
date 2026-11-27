package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.1 Batch 0 (#257) — exigences de sécurité S15 / S16 (_work/reports/security-lot3.1.md, constats A1 et A2), écrites AVANT
// le code. Vérifiées par le comportement : backends chargés d'un Space non admin (Space.LoadBackends), kubeconfigFile, Engine.kcmd
// avec un FAUX kubectl qui journalise argv, environnement et contenu du fichier --kubeconfig. Aucun secret réel : canari CANARY-….
//
// S16 : le coffre d'un backend d'utilisateur est users/<uid>/vault/<backend>/ UNIQUEMENT ; jamais le coffre commun de l'admin.
// S15 : un backend k8s d'utilisateur ne tombe JAMAIS sur le ServiceAccount du launcher, ~/.kube/config ni KUBECONFIG hérité :
//       sans kubeconfig du coffre de l'utilisateur → refus (déclaration) et échec fermé (exécution : kubectl n'est pas lancé).

const (
	s15AdminCanary = "CANARY-a1d0c0ffee01"
	s15Alice       = "github-11"
	s15Bob         = "github-22"
)

func s15Kubeconfig(token string) string {
	return "apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://k8s.example.org:6443\n" +
		"users:\n- name: u\n  user:\n    token: " + token + "\ncontexts:\n- name: ctx\n  context:\n    cluster: c\n    user: u\ncurrent-context: ctx\n"
}

type s15Env struct {
	admin string // coffre commun (admin)
	log   string // journal du faux kubectl
}

// s15Setup : config et coffre commun temporaires (0700) avec un kubeconfig de l'admin portant le canari ; faux kubectl en tête du PATH ;
// environnement piégé (KUBECONFIG hérité, HOME avec ~/.kube/config, pod avec ServiceAccount) pour détecter tout repli.
func s15Setup(t *testing.T) s15Env {
	t.Helper()
	unixOnly(t)
	home := withHome(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	admin := filepath.Join(t.TempDir(), "secrets")
	if err := os.MkdirAll(filepath.Join(admin, "k8s-prod"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(admin, "k8s-prod", "kubeconfig"), []byte(s15Kubeconfig(s15AdminCanary)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SecretsDirVar, admin)
	// pièges : si l'un d'eux sert, le test le voit
	t.Setenv("KUBECONFIG", "/herite/ne/doit/jamais/servir")
	_ = os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".kube", "config"), []byte(s15Kubeconfig("CANARY-homekube")), 0o600)
	ns := filepath.Join(t.TempDir(), "namespace")
	_ = os.WriteFile(ns, []byte("ns-plateforme\n"), 0o644)
	old := saNamespacePath.Get()
	saNamespacePath.Set(ns)
	t.Cleanup(func() { saNamespacePath.Set(old) })
	t.Setenv(InClusterVar, "1")

	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := `#!/bin/sh
echo "ARGV: $*" >> "` + log + `"
echo "ENV: KUBECONFIG=[$KUBECONFIG]" >> "` + log + `"
prev=""
for a in "$@"; do
  if [ "$prev" = "--kubeconfig" ]; then echo "FILE: $a" >> "` + log + `"; cat "$a" >> "` + log + `" 2>/dev/null; fi
  prev="$a"
done
echo "{}"
`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return s15Env{admin: admin, log: log}
}

func (e s15Env) calls() string { b, _ := os.ReadFile(e.log); return string(b) }

// s15Space : espace d'utilisateur avec ce backends.yaml (écrit directement : on contrôle ce qui est CHARGÉ, pas la validation).
func s15Space(t *testing.T, uid, yaml string) Space {
	t.Helper()
	sp, err := UserSpace(uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sp.BackendsPath(), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return sp
}

func s15Vault(t *testing.T, sp Space, backend, kubeconfig string) string {
	t.Helper()
	d := filepath.Join(sp.VaultDir(), backend)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "kubeconfig")
	if err := os.WriteFile(p, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func s15Backend(t *testing.T, sp Space, name string) (BackendConf, bool) {
	t.Helper()
	bs, err := sp.LoadBackends()
	if err != nil {
		return BackendConf{}, false // chargement refusé : échec fermé, acceptable
	}
	for _, b := range bs {
		if b.Name == name {
			return b, true
		}
	}
	return BackendConf{}, false
}

const s15K8sVault = "backends:\n  %s:\n    type: k8s\n    namespace: ns\n    kubeconfig: true\n"

func s15Yaml(name string) string { return strings.Replace(s15K8sVault, "%s", name, 1) }

// ── S16 : coffre par espace ──────────────────────────────────────────────────

func TestS16DeuxUtilisateursMemeNomDeBackendDeuxCoffres(t *testing.T) {
	s15Setup(t)
	alice := s15Space(t, s15Alice, s15Yaml("mon-k8s"))
	bob := s15Space(t, s15Bob, s15Yaml("mon-k8s"))
	pa := s15Vault(t, alice, "mon-k8s", s15Kubeconfig("CANARY-alice"))
	pb := s15Vault(t, bob, "mon-k8s", s15Kubeconfig("CANARY-bob"))
	ba, ok1 := s15Backend(t, alice, "mon-k8s")
	bb, ok2 := s15Backend(t, bob, "mon-k8s")
	if !ok1 || !ok2 {
		t.Fatalf("backends non chargés (alice %v, bob %v)", ok1, ok2)
	}
	fa, err1 := ba.kubeconfigFile()
	fb, err2 := bb.kubeconfigFile()
	if err1 != nil || err2 != nil {
		t.Fatalf("kubeconfigFile : %v / %v", err1, err2)
	}
	if fa != pa || fb != pb || fa == fb {
		t.Errorf("coffres : alice %q (attendu %q), bob %q (attendu %q)", fa, pa, fb, pb)
	}
}

func TestS16NommerSonBackendCommeCeluiDeLAdminNeLitPasLeCoffreCommun(t *testing.T) {
	e := s15Setup(t)
	alice := s15Space(t, s15Alice, s15Yaml("k8s-prod")) // même nom que le backend de l'admin, sans kubeconfig dans SON coffre
	b, ok := s15Backend(t, alice, "k8s-prod")
	if !ok {
		return // refusé au chargement : échec fermé
	}
	f, err := b.kubeconfigFile()
	if err == nil && (pathWithin(f, e.admin) || !pathWithin(f, alice.VaultDir())) {
		t.Fatalf("le kubeconfig résolu (%s) sort du coffre de l'utilisateur (%s) : coffre commun lu", f, alice.VaultDir())
	}
	// exécution : kubectl factice ; le canari de l'admin n'apparaît dans AUCUNE trace
	_ = Engine{b}.kcmd("get", "pods").Run()
	if strings.Contains(e.calls(), s15AdminCanary) {
		t.Errorf("le kubeconfig de l'admin a été passé à kubectl pour un utilisateur :\n%s", e.calls())
	}
}

func TestS16NomsDeBackendInvalidesRefuses(t *testing.T) {
	e := s15Setup(t)
	for _, bad := range []string{"../k8s-prod", "a/b", "k8s-prod/..", "..", "A B", "-x", strings.Repeat("a", 64), `a\b`} {
		sp := s15Space(t, s15Alice, "backends:\n  \""+bad+"\":\n    type: k8s\n    namespace: ns\n    kubeconfig: true\n")
		b, ok := s15Backend(t, sp, bad)
		if !ok {
			continue // refusé au chargement
		}
		f, err := b.kubeconfigFile()
		if err == nil {
			t.Errorf("nom %q : kubeconfigFile = %q sans erreur", bad, f)
		}
		_ = Engine{b}.kcmd("get", "pods").Run()
		if strings.Contains(e.calls(), "ARGV") {
			t.Errorf("nom %q : kubectl lancé", bad)
		}
	}
}

// Le coffre commun reste intact et invisible : aucune écriture de l'utilisateur n'y atterrit.
func TestS16CoffreCommunIntact(t *testing.T) {
	e := s15Setup(t)
	alice := s15Space(t, s15Alice, s15Yaml("mon-k8s"))
	s15Vault(t, alice, "mon-k8s", s15Kubeconfig("CANARY-alice"))
	b, _ := s15Backend(t, alice, "mon-k8s")
	_ = Engine{b}.kcmd("get", "pods").Run()
	ents, _ := os.ReadDir(e.admin)
	if len(ents) != 1 || ents[0].Name() != "k8s-prod" {
		t.Errorf("le coffre commun a changé : %v", ents)
	}
	if strings.Contains(e.calls(), s15AdminCanary) {
		t.Error("canari de l'admin dans les traces")
	}
}

// ── S15 : pas de repli sur le ServiceAccount, ~/.kube/config ou KUBECONFIG ───────────────────────────────

func TestS15DeclarationK8sSansKubeconfigRefusee(t *testing.T) {
	s15Setup(t)
	sp, _ := UserSpace(s15Alice)
	for name, content := range map[string]string{
		"sans kubeconfig":         "backends:\n  k:\n    type: k8s\n    namespace: ns\n",
		"kubeconfig: false":       "backends:\n  k:\n    type: k8s\n    namespace: ns\n    kubeconfig: false\n",
		"kubeconfig vide":         "backends:\n  k:\n    type: k8s\n    namespace: ns\n    kubeconfig:\n",
		"chemin local (poste)":    "backends:\n  k:\n    type: k8s\n    namespace: ns\n    kubeconfig: /etc/rancher/k3s/k3s.yaml\n",
		"chemin du coffre commun": "backends:\n  k:\n    type: k8s\n    namespace: ns\n    kubeconfig: " + filepath.Join(os.Getenv(SecretsDirVar), "k8s-prod", "kubeconfig") + "\n",
	} {
		ps := sp.ValidateBackends([]byte(content))
		if !HasErrors(ps) {
			t.Errorf("%s : déclaration acceptée par la validation (%v)", name, ps)
			continue
		}
		if _, err := sp.SaveBackends([]byte(content), time.Time{}); err == nil {
			t.Errorf("%s : SaveBackends a accepté la déclaration", name)
		}
		var msg string
		for _, p := range ps {
			if p.Severity == SevError {
				msg = p.Message
				break
			}
		}
		if !strings.Contains(strings.ToLower(msg), "kubeconfig") {
			t.Errorf("%s : message peu clair « %s » (doit nommer le kubeconfig)", name, msg)
		}
	}
	if _, err := os.Stat(sp.BackendsPath()); err == nil {
		t.Error("un backends.yaml refusé a tout de même été écrit")
	}
	// avec kubeconfig: true : acceptée
	if ps, err := sp.SaveBackends([]byte(s15Yaml("k")), time.Time{}); err != nil {
		t.Errorf("kubeconfig: true refusé : %v %v", err, ps)
	}
}

func TestS15KubectlToujoursAvecLeKubeconfigDeLUtilisateur(t *testing.T) {
	e := s15Setup(t)
	alice := s15Space(t, s15Alice, s15Yaml("mon-k8s"))
	p := s15Vault(t, alice, "mon-k8s", s15Kubeconfig("CANARY-alice"))
	b, ok := s15Backend(t, alice, "mon-k8s")
	if !ok {
		t.Fatal("backend non chargé")
	}
	for _, args := range [][]string{{"get", "pods"}, {"auth", "can-i", "create", "statefulsets"}, {"scale", "statefulset", "x", "--replicas=0"}} {
		if err := (Engine{b}).kcmd(args...).Run(); err != nil {
			t.Fatalf("kubectl %v : %v", args, err)
		}
	}
	log := e.calls()
	n := strings.Count(log, "ARGV:")
	if n != 3 || strings.Count(log, "--kubeconfig "+p) != 3 || strings.Count(log, "FILE: "+p) != 3 {
		t.Errorf("chaque appel doit porter --kubeconfig %s :\n%s", p, log)
	}
	if strings.Contains(log, "KUBECONFIG=[/herite") || strings.Contains(log, ".kube/config") || strings.Contains(log, "CANARY-homekube") ||
		strings.Contains(log, s15AdminCanary) || strings.Contains(log, "ns-plateforme") {
		t.Errorf("repli sur un autre kubeconfig (hérité, ~/.kube, admin) ou sur le namespace du pod :\n%s", log)
	}
	if !strings.Contains(log, "CANARY-alice") || !strings.Contains(log, "-n ns") && !strings.Contains(log, "--namespace ns") && !strings.Contains(log, "--namespace=ns") {
		t.Errorf("kubeconfig ou namespace de l'utilisateur absents :\n%s", log)
	}
}

// Kubeconfig absent du coffre de l'utilisateur : échec fermé — kubectl n'est PAS lancé (ni SA, ni ~/.kube/config).
func TestS15KubeconfigAbsentEchecFerme(t *testing.T) {
	e := s15Setup(t)
	alice := s15Space(t, s15Alice, s15Yaml("mon-k8s")) // déclaré, mais rien dans son coffre
	b, ok := s15Backend(t, alice, "mon-k8s")
	if !ok {
		return // refusé au chargement : échec fermé
	}
	err := Engine{b}.kcmd("get", "pods").Run()
	if err == nil {
		t.Error("kubectl a réussi sans kubeconfig de l'utilisateur")
	}
	if strings.Contains(e.calls(), "ARGV") {
		t.Errorf("kubectl lancé sans kubeconfig de l'utilisateur :\n%s", e.calls())
	}
}

// Backend k8s forcé sans kubeconfig (fichier édité à la main) : jamais lancé, jamais ServiceAccount.
func TestS15BackendSansKubeconfigJamaisLance(t *testing.T) {
	e := s15Setup(t)
	alice := s15Space(t, s15Alice, "backends:\n  k:\n    type: k8s\n    namespace: ns\n")
	b, ok := s15Backend(t, alice, "k")
	if !ok {
		return // refusé au chargement : échec fermé
	}
	_ = Engine{b}.kcmd("get", "pods").Run()
	if strings.Contains(e.calls(), "ARGV") {
		t.Errorf("kubectl lancé pour un backend d'utilisateur sans kubeconfig (repli ServiceAccount / ~/.kube) :\n%s", e.calls())
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Fatal(err)
	}
}

// NR : l'admin garde son comportement v7.0.0 (coffre commun, kubeconfig: true ; sans kubeconfig en pod : ServiceAccount).
func TestS15S16AdminInchange(t *testing.T) {
	e := s15Setup(t)
	if err := os.WriteFile(BackendsPath(), []byte(s15Yaml("k8s-prod")), 0o600); err != nil {
		t.Fatal(err)
	}
	b, ok := s15Backend(t, AdminSpace(), "k8s-prod")
	if !ok {
		t.Fatal("backend de l'admin non chargé")
	}
	f, err := b.kubeconfigFile()
	if err != nil || f != filepath.Join(e.admin, "k8s-prod", "kubeconfig") {
		t.Errorf("kubeconfig de l'admin : %q, %v", f, err)
	}
	if err := (Engine{b}).kcmd("get", "pods").Run(); err != nil || !strings.Contains(e.calls(), s15AdminCanary) {
		t.Errorf("l'admin n'utilise plus son coffre commun : %v\n%s", err, e.calls())
	}
}
