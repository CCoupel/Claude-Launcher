package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #201 (dev) : lien pendant, migration, kubeconfig par chemin, /files. Dossiers temporaires, aucun vrai projet.

func dev201Env(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("CL_ROOT_BASE", root)
	t.Setenv(SecretsDirVar, "")
	t.Setenv(SecretsLegacyDirVar, "")
	t.Setenv("CL_CACHE_DIR", filepath.Join(root, ".cache"))
	_ = os.MkdirAll(filepath.Join(root, ".cache"), 0o700)
	return root
}

func TestDev201RealPathDanglingAndLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	d := t.TempDir()
	if err := os.Symlink(filepath.Join(d, "cible", "vide"), filepath.Join(d, "pendant")); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(d)
	if got := realPath(filepath.Join(d, "pendant", "x")); got != filepath.Join(want, "cible", "vide", "x") {
		t.Errorf("lien pendant non suivi : %s", got)
	}
	_ = os.Symlink(filepath.Join(d, "b"), filepath.Join(d, "a"))
	_ = os.Symlink(filepath.Join(d, "a"), filepath.Join(d, "b"))
	if _, err := realPathChecked(filepath.Join(d, "a")); err == nil {
		t.Error("boucle de liens non signalée")
	}
	// coffre = lien pendant vers l'extérieur : refusé, rien n'est créé
	out := filepath.Join(t.TempDir(), "dehors")
	_ = os.Symlink(out, filepath.Join(d, "coffre"))
	if err := checkSecretsDir(filepath.Join(d, "coffre"), nil); err == nil {
		t.Error("coffre en lien pendant accepté")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("rien ne doit être créé")
	}
}

func TestDev201MigrationKeepsLegacyAndReportsConflict(t *testing.T) {
	root := dev201Env(t)
	legacy := filepath.Join(t.TempDir(), "old")
	t.Setenv(SecretsLegacyDirVar, legacy)
	for b, c := range map[string]string{"srv1": "A", "srv2": "B"} {
		_ = os.MkdirAll(filepath.Join(legacy, b), 0o700)
		_ = os.WriteFile(filepath.Join(legacy, b, "id"), []byte(c), 0o600)
		_ = os.WriteFile(filepath.Join(legacy, b, "known_hosts"), []byte("k"+c), 0o600)
	}
	_ = os.WriteFile(filepath.Join(legacy, "fichier-libre"), []byte("x"), 0o600) // pas un backend : ignoré
	vault := filepath.Join(root, ".config", "claude-launcher-ng2", "secrets")
	// srv2 existe déjà avec un autre contenu : coffre commun conservé
	_ = os.MkdirAll(filepath.Join(vault, "srv2"), 0o700)
	_ = os.WriteFile(filepath.Join(vault, "srv2", "id"), []byte("COMMUN"), 0o600)
	n, err := MigrateSecrets()
	if err != nil || n != 1 {
		t.Fatalf("migration : %d %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(vault, "srv1", "id")); string(b) != "A" {
		t.Errorf("srv1 non migré : %q", b)
	}
	if st, _ := os.Stat(filepath.Join(vault, "srv1", "id")); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("droits %o", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(filepath.Join(vault, "srv2", "id")); string(b) != "COMMUN" {
		t.Errorf("srv2 écrasé : %q", b)
	}
	if c := SecretsMigrationConflicts(); len(c) != 1 || c[0] != "srv2" {
		t.Errorf("conflits : %v", c)
	}
	if _, err := os.Stat(filepath.Join(legacy, "srv1", "id")); err != nil {
		t.Error("ancien coffre non conservé")
	}
	if n, _ := MigrateSecrets(); n != 0 {
		t.Errorf("migration rejouée : %d", n)
	}
	j, _ := os.ReadFile(filepath.Join(CacheDir(), secretJournal))
	if !strings.Contains(string(j), `"action":"migrate"`) || !strings.Contains(string(j), `"instance"`) || strings.Contains(string(j), "COMMUN") {
		t.Errorf("journal : %s", j)
	}
}

func TestDev201KubeconfigPathAllowedInVaultOrPrivate(t *testing.T) {
	root := dev201Env(t)
	vault := filepath.Join(root, ".config", "claude-launcher-ng2", "secrets")
	priv := filepath.Join(root, "PROJETS", "p", "PRIVATE", "PROD", "kubeconfig")
	autre := filepath.Join(root, "PROJETS", "p", "kubeconfig")
	cases := map[string]bool{filepath.Join(vault, "b", "kubeconfig"): true, priv: true, autre: false}
	for p, ok := range cases {
		_, err := BackendConf{Name: "b", Kubeconfig: KubeconfigRef{Path: p}}.kubeconfigFile()
		if (err == nil) != ok {
			t.Errorf("%s : admis=%v, erreur %v", p, ok, err)
		}
	}
}
