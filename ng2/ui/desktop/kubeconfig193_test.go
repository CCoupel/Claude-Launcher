package desktop

// Tests de spécification #193 (v6.4.0) — dépôt du kubeconfig d'un backend k8s dans le coffre par l'interface
// (backendSecretSet {backend, kubeconfig}) : contextes listés, contenu jamais renvoyé ni journalisé, règle
// d'écriture provisoire, refus motivés. Boîte noire via Dispatch ; réutilise les aides de secrets194_test.go.
// Hypothèse d'API : la réponse liste les contextes dans « contexts » (noms seulement).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const kube193 = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: c1
  cluster: {server: "https://10.0.0.1:6443", certificate-authority-data: "Q0EtMTkzLVNFQ1JFVA=="}
users:
- name: u1
  user: {token: "tok-193-secret"}
contexts:
- name: prod
  context: {cluster: c1, user: u1}
- name: staging
  context: {cluster: c1, user: u1}
`

func vaultK8s193(t *testing.T) (a *App, dir string) {
	t.Helper()
	a, dir = vault194(t)
	cfg := os.Getenv("CL_CONFIG_DIR")
	y := "backends:\n  autre: { type: k8s, namespace: claude-qualif-extra, kubeconfig: true }\n  srv1: { type: ssh, host: 192.168.1.210 }\n"
	if err := os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

func TestKubeconfigDepotContextesListes193(t *testing.T) {
	a, dir := vaultK8s193(t)
	res, raw := mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "autre", "kubeconfig": kube193})
	ctx, _ := res["contexts"].([]any)
	if len(ctx) != 2 || ctx[0] != "prod" || ctx[1] != "staging" {
		t.Errorf("contextes = %v, attendu [prod staging]", res["contexts"])
	}
	for _, secret := range []string{"tok-193-secret", "Q0EtMTkzLVNFQ1JFVA==", "10.0.0.1"} {
		if strings.Contains(raw, secret) {
			t.Errorf("la réponse renvoie du contenu du kubeconfig (%q) : %s", secret, raw)
		}
	}
	p := filepath.Join(dir, "autre", "kubeconfig")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("kubeconfig absent du coffre : %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("droits %v, attendu 0600", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "tok-193-secret") {
		t.Error("le contenu n'est pas enregistré tel quel dans le coffre")
	}
}

func TestKubeconfigJamaisRelu193(t *testing.T) {
	a, _ := vaultK8s193(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "autre", "kubeconfig": kube193})
	_, raw := mustOK194(t, a, "backendSecretGet", map[string]any{"backend": "autre"})
	for _, secret := range []string{"tok-193-secret", "Q0EtMTkzLVNFQ1JFVA==", "https://10.0.0.1"} {
		if strings.Contains(raw, secret) {
			t.Errorf("Get renvoie du contenu du kubeconfig (%q) : %s", secret, raw)
		}
	}
}

func TestKubeconfigJournalSansContenu193(t *testing.T) {
	a, dir := vaultK8s193(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "autre", "kubeconfig": kube193})
	root := filepath.Dir(dir)
	journal := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return nil
		}
		b, _ := os.ReadFile(path)
		s := string(b)
		if strings.Contains(s, "tok-193-secret") || strings.Contains(s, "Q0EtMTkzLVNFQ1JFVA==") {
			t.Errorf("contenu du kubeconfig copié hors du coffre : %s", path)
		}
		if strings.Contains(s, "autre") && strings.Contains(strings.ToLower(s), "kubeconfig") {
			journal = true
		}
		return nil
	})
	if !journal {
		t.Error("l'écriture du kubeconfig n'est pas journalisée (backend + type), hors du coffre")
	}
}

func TestKubeconfigRefusMotives193(t *testing.T) {
	for _, c := range []struct {
		name  string
		args  map[string]any
		motif string
	}{
		{"texte quelconque", map[string]any{"backend": "autre", "kubeconfig": "pas un kubeconfig"}, "kubeconfig"},
		{"sans cluster ni contexte", map[string]any{"backend": "autre", "kubeconfig": "apiVersion: v1\nkind: Config\n"}, "kubeconfig"},
		{"backend ssh : pas de kubeconfig", map[string]any{"backend": "srv1", "kubeconfig": kube193}, "k8s"},
		{"backend inconnu", map[string]any{"backend": "inconnu", "kubeconfig": kube193}, "inconnu"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, dir := vaultK8s193(t)
			_, raw, failed := call194(t, a, "backendSecretSet", c.args)
			if !failed {
				t.Fatalf("accepté : %s", raw)
			}
			wantMotif194(t, raw, c.motif)
			for _, b := range []string{"autre", "srv1", "inconnu"} {
				if _, err := os.Stat(filepath.Join(dir, b, "kubeconfig")); err == nil {
					t.Errorf("kubeconfig écrit pour %s malgré le refus", b)
				}
			}
		})
	}
}

func TestKubeconfigSupprimer193(t *testing.T) {
	a, dir := vaultK8s193(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "autre", "kubeconfig": kube193})
	mustOK194(t, a, "backendSecretDelete", map[string]any{"backend": "autre", "what": "all"})
	if _, err := os.Stat(filepath.Join(dir, "autre", "kubeconfig")); err == nil {
		t.Error("kubeconfig non supprimé par « all »")
	}
}

// règle provisoire : même autorisation que les clés ssh, pour tous les clients (sans authentification).
func TestKubeconfigEcritureSansAuthentification193(t *testing.T) {
	a, _ := vaultK8s193(t)
	mustOK194(t, a, "backendSecretSet", map[string]any{"backend": "autre", "kubeconfig": kube193})
}
