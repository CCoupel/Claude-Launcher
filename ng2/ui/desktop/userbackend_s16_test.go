package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// LOT-3.1 Batch 0 (#257) — S16 (coffre par espace) et S24 (isolement) par l'API web, écrits AVANT le code. Un utilisateur dépose le
// kubeconfig de SON backend par backendSecretSet : il atterrit dans users/<uid>/vault/<backend>/kubeconfig, jamais dans le coffre
// commun de l'admin ; deux utilisateurs de même nom de backend ont deux coffres ; un nom de backend traversant ou invalide est
// refusé. Canaris CANARY-… dans les fausses identités : ils ne sortent dans aucune réponse ni journal.

const (
	ub16Admin = "CANARY-a1d0c0ffee01"
	ub16Alice = "CANARY-alice00001"
	ub16Bob   = "CANARY-bob0000002"
)

func ub16Kube(token string) string {
	return "apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://k8s.example.org:6443\n" +
		"users:\n- name: u\n  user:\n    token: " + token + "\ncontexts:\n- name: ctx\n  context:\n    cluster: c\n    user: u\ncurrent-context: ctx\n"
}

// ub16Start : serveur RunWeb en mode proxy, coffre commun de l'admin avec un kubeconfig « k8s-prod » portant le canari.
func ub16Start(t *testing.T) (s *px172, adminVault string) {
	t.Helper()
	adminVault = filepath.Join(t.TempDir(), "secrets")
	if err := os.MkdirAll(filepath.Join(adminVault, "k8s-prod"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminVault, "k8s-prod", "kubeconfig"), []byte(ub16Kube(ub16Admin)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(core.SecretsDirVar, adminVault)
	s = ed255Start(t) // alice = github-1111, bob = github-2222 (API GitHub factice de sp255Gh)
	return s, adminVault
}

// ub16Declare : écrit directement le backends.yaml d'un utilisateur (la validation de LOT-3.1 est testée ailleurs).
func ub16Declare(t *testing.T, uid, name string) {
	t.Helper()
	sp, err := core.UserSpace(uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Ensure(); err != nil {
		t.Fatal(err)
	}
	y := "backends:\n  " + name + ":\n    type: k8s\n    namespace: ns\n    kubeconfig: true\n"
	if err := os.WriteFile(sp.BackendsPath(), []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ub16Tree(t *testing.T, root string) (files map[string]string) {
	t.Helper()
	files = map[string]string{}
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			files[p] = string(b)
		}
		return nil
	})
	return files
}

func TestS16KubeconfigAtterritDansLeCoffreDeLUtilisateur(t *testing.T) {
	s, admin := ub16Start(t)
	ub16Declare(t, "github-1111", "k8s-prod") // alice nomme son backend comme celui de l'admin
	raw, e, r := ed255Do(t, s, "alice", "backendSecretSet", map[string]any{"backend": "k8s-prod", "kubeconfig": ub16Kube(ub16Alice)})
	if e != "" || r["ok"] != true {
		t.Fatalf("backendSecretSet d'un utilisateur : %s", raw)
	}
	for _, c := range []string{ub16Alice, ub16Admin} {
		if strings.Contains(raw, c) {
			t.Errorf("canari %s dans la réponse : %s", c, raw)
		}
	}
	mine := filepath.Join(core.UsersDir(), "github-1111", "vault", "k8s-prod", "kubeconfig")
	if b, err := os.ReadFile(mine); err != nil || !strings.Contains(string(b), ub16Alice) {
		t.Errorf("kubeconfig absent du coffre de l'utilisateur (%s) : %v", mine, err)
	}
	// coffre commun intact : le kubeconfig de l'admin n'a pas été écrasé
	if b, _ := os.ReadFile(filepath.Join(admin, "k8s-prod", "kubeconfig")); !strings.Contains(string(b), ub16Admin) || strings.Contains(string(b), ub16Alice) {
		t.Errorf("le coffre commun de l'admin a été modifié : %q", b)
	}
	// ni le canari de l'utilisateur ailleurs que dans son coffre
	for p, c := range ub16Tree(t, filepath.Dir(core.UsersDir())) {
		if strings.Contains(c, ub16Alice) && !strings.HasPrefix(p, filepath.Join(core.UsersDir(), "github-1111", "vault")) {
			t.Errorf("secret d'alice hors de son coffre : %s", p)
		}
	}
	if strings.Contains(fmtJournal(t), ub16Alice) || strings.Contains(fmtJournal(t), ub16Admin) {
		t.Error("un secret figure dans le journal des écritures")
	}
}

func TestS16DeuxUtilisateursMemeNomDeBackendDeuxCoffresWeb(t *testing.T) {
	s, _ := ub16Start(t)
	ub16Declare(t, "github-1111", "mon-k8s")
	ub16Declare(t, "github-2222", "mon-k8s")
	for user, canary := range map[string]string{"alice": ub16Alice, "bob": ub16Bob} {
		if raw, e, r := ed255Do(t, s, user, "backendSecretSet", map[string]any{"backend": "mon-k8s", "kubeconfig": ub16Kube(canary)}); e != "" || r["ok"] != true {
			t.Fatalf("%s : %s", user, raw)
		}
	}
	a, _ := os.ReadFile(filepath.Join(core.UsersDir(), "github-1111", "vault", "mon-k8s", "kubeconfig"))
	b, _ := os.ReadFile(filepath.Join(core.UsersDir(), "github-2222", "vault", "mon-k8s", "kubeconfig"))
	if !strings.Contains(string(a), ub16Alice) || strings.Contains(string(a), ub16Bob) || !strings.Contains(string(b), ub16Bob) || strings.Contains(string(b), ub16Alice) {
		t.Errorf("coffres mélangés ou écrasés :\nalice=%q\nbob=%q", a, b)
	}
	// l'état lu par chacun ne montre que SON kubeconfig (jamais celui d'un autre, ni de l'admin)
	for _, u := range []string{"alice", "bob"} {
		raw, _, _ := ed255Do(t, s, u, "backendSecretGet", map[string]any{"backend": "mon-k8s"})
		for _, c := range []string{ub16Alice, ub16Bob, ub16Admin} {
			if strings.Contains(raw, c) {
				t.Errorf("%s : canari %s dans backendSecretGet : %s", u, c, raw)
			}
		}
	}
}

// Lire l'état du coffre d'un backend de même nom que celui de l'admin : jamais l'état du coffre commun.
func TestS16BackendSecretGetNeMontrePasLeCoffreCommun(t *testing.T) {
	s, _ := ub16Start(t)
	ub16Declare(t, "github-1111", "k8s-prod") // alice : rien dans son coffre
	raw, e, r := ed255Do(t, s, "alice", "backendSecretGet", map[string]any{"backend": "k8s-prod"})
	if e != "" && r == nil {
		return // refus : acceptable
	}
	if strings.Contains(raw, ub16Admin) || strings.Contains(strings.ToLower(raw), `"haskubeconfig":true`) {
		t.Errorf("alice voit le kubeconfig du coffre commun de l'admin : %s", raw)
	}
}

func TestS16S24NomsDeBackendTraversantsOuInvalidesRefuses(t *testing.T) {
	s, admin := ub16Start(t)
	ub16Declare(t, "github-1111", "mon-k8s")
	before := ub16Tree(t, filepath.Dir(core.UsersDir()))
	for _, bad := range []string{"../k8s-prod", "../../secrets/k8s-prod", "a/b", "github-2222/vault/mon-k8s", "K8S-PROD", "..", ".", "",
		strings.Repeat("a", 64), "a\x00b", "a b"} {
		raw, e, r := ed255Do(t, s, "alice", "backendSecretSet", map[string]any{"backend": bad, "kubeconfig": ub16Kube(ub16Alice)})
		if r != nil && r["ok"] == true {
			t.Errorf("nom %q accepté : %s", bad, raw)
		}
		if e == "" && (r == nil || r["ok"] != false || r["msg"] == "") {
			t.Errorf("nom %q : pas de refus clair : %s", bad, raw)
		}
	}
	after := ub16Tree(t, filepath.Dir(core.UsersDir()))
	for p, c := range after {
		if _, had := before[p]; !had && strings.Contains(c, ub16Alice) {
			t.Errorf("un nom refusé a tout de même écrit un secret : %s", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(admin, "k8s-prod", "kubeconfig")); !strings.Contains(string(b), ub16Admin) {
		t.Error("coffre commun modifié par un nom traversant")
	}
	// un utilisateur ne peut pas cibler l'espace d'un autre
	if _, err := os.Stat(filepath.Join(core.UsersDir(), "github-2222")); err == nil {
		t.Error("l'espace de bob a été créé par un appel d'alice")
	}
}

// backendSecretSet pour un backend non déclaré dans SON espace (déclaré chez l'admin seulement) : refusé.
func TestS16BackendDeLAdminNonUtilisableParUnUtilisateur(t *testing.T) {
	s, admin := ub16Start(t)
	raw, e, r := ed255Do(t, s, "alice", "backendSecretSet", map[string]any{"backend": "k8s-prod", "kubeconfig": ub16Kube(ub16Alice)})
	if r != nil && r["ok"] == true {
		t.Errorf("backendSecretSet accepté pour un backend absent de son espace : %s", raw)
	}
	if e == "" && (r == nil || r["ok"] != false) {
		t.Errorf("pas de refus : %s", raw)
	}
	if b, _ := os.ReadFile(filepath.Join(admin, "k8s-prod", "kubeconfig")); !strings.Contains(string(b), ub16Admin) || strings.Contains(string(b), ub16Alice) {
		t.Errorf("coffre commun modifié : %q", b)
	}
}

func fmtJournal(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(core.CacheDir(), "secrets-journal.jsonl"))
	return string(b)
}
