//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #201 (LOT-2) : coffre COMMUN sous la racine partagée, contrôles de chemin (contracts/backends-remote.md
// « Coffre des secrets »). Écrits depuis le contrat : le coffre vit sous <CL_ROOT_BASE>/.config/claude-launcher-ng2/secrets.

// sharedEnv : racine partagée + cache, sans CL_SECRETS_DIR (emplacement par défaut du coffre commun).
func sharedEnv201(t *testing.T) (root string) {
	t.Helper()
	resetSecretsMigration(t) // état global du signalement : propre à chaque test (-count=N)
	root = t.TempDir()
	home := t.TempDir() // ancien coffre local (v6.4.0) : jamais le vrai ~/.config/claude-launcher/secrets
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppLocal"))
	t.Setenv("CL_ROOT_BASE", root)
	t.Setenv(SecretsDirVar, "")
	t.Setenv("CL_SECRETS_LEGACY_DIR", filepath.Join(home, "aucun-ancien-coffre"))
	t.Setenv(CacheDirVar, filepath.Join(t.TempDir(), "cache"))
	t.Setenv("CL_K8S_VOLUMES", "")
	return root
}

func mustNotExist201(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); err == nil {
		t.Errorf("%s ne doit pas exister (rien n'est écrit en cas de refus)", p)
	}
}

func TestCoffreCommunSousLaRacine201(t *testing.T) {
	root := sharedEnv201(t)
	want := filepath.Join(root, ".config", "claude-launcher-ng2", "secrets")
	got, err := SecretsDir()
	if err != nil {
		t.Fatalf("coffre commun sous la racine partagée refusé : %v", err)
	}
	if got != want {
		t.Errorf("emplacement : %s, attendu %s", got, want)
	}
	if st, _ := os.Stat(got); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("coffre créé en 0700 attendu : %v", st)
	}
	priv, pub, _ := generateKeyPair("a")
	if _, _, err := SecretSetKey("srv", string(priv), string(pub), false, SecretWrite{}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(want, "srv", "id")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("clé privée en 0600 attendue : %v %v", st, err)
	}
}

func TestCoffreLienPendant201(t *testing.T) {
	for _, mode := range []string{"relatif", "absolu"} {
		t.Run(mode, func(t *testing.T) {
			root := sharedEnv201(t)
			cfg := filepath.Join(root, ".config")
			_ = os.MkdirAll(cfg, 0o700)
			target := filepath.Join(root, "PROJETS", "p", "cible-absente") // n'existe pas : lien pendant
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			link := filepath.Join(cfg, "claude-launcher-ng2")
			dst := target
			if mode == "relatif" {
				dst = filepath.Join("..", "PROJETS", "p", "cible-absente")
			}
			if err := os.Symlink(dst, link); err != nil {
				t.Fatal(err)
			}
			if _, err := SecretsDir(); err == nil {
				t.Error("coffre dont le chemin passe par un lien pendant vers un projet accepté")
			}
			mustNotExist201(t, target)
			priv, pub, _ := generateKeyPair("a")
			if _, _, err := SecretSetKey("srv", string(priv), string(pub), false, SecretWrite{}); err == nil {
				t.Error("écriture acceptée à travers un lien pendant")
			}
			mustNotExist201(t, target)
			mustNotExist201(t, filepath.Join(root, "PROJETS", "p", "secrets"))
		})
	}
}

func TestCoffreBoucleDeLiens201(t *testing.T) {
	root := sharedEnv201(t)
	cfg := filepath.Join(root, ".config")
	_ = os.MkdirAll(cfg, 0o700)
	a, b := filepath.Join(cfg, "claude-launcher-ng2"), filepath.Join(cfg, "b")
	_ = os.Symlink(b, a)
	_ = os.Symlink(a, b)
	if _, err := SecretsDir(); err == nil {
		t.Error("boucle de liens : coffre accepté (refus attendu, sans blocage)")
	}
}

func TestCoffreLienVersAutreDossierExistant201(t *testing.T) {
	root := sharedEnv201(t)
	other := filepath.Join(root, "PROJETS", "p", "dossier")
	_ = os.MkdirAll(other, 0o700)
	cfg := filepath.Join(root, ".config")
	_ = os.MkdirAll(cfg, 0o700)
	_ = os.Symlink(other, filepath.Join(cfg, "claude-launcher-ng2"))
	if _, err := SecretsDir(); err == nil {
		t.Error("chemin résolu hors de l'emplacement attendu : accepté")
	}
	entries, _ := os.ReadDir(other)
	if len(entries) != 0 {
		t.Errorf("rien ne doit être créé dans le dossier visé : %v", entries)
	}
}

func TestCoffreSousUnDossierDeProjet201(t *testing.T) {
	root := sharedEnv201(t)
	d := filepath.Join(root, "PROJETS", "p", "secrets")
	t.Setenv(SecretsDirVar, d)
	if _, err := SecretsDir(); err == nil {
		t.Error("coffre sous un dossier de projet accepté")
	}
	mustNotExist201(t, d)
	// aussi sous PRIVATE/ d'un projet : jamais le coffre du launcher
	t.Setenv(SecretsDirVar, filepath.Join(root, "PROJETS", "p", "PRIVATE", "GLOBAL", "secrets"))
	if _, err := SecretsDir(); err == nil {
		t.Error("coffre dans PRIVATE/ d'un projet accepté (le launcher n'y écrit pas)")
	}
}

func TestCoffreNon0700Refuse201(t *testing.T) {
	root := sharedEnv201(t)
	d := filepath.Join(root, ".config", "claude-launcher-ng2", "secrets")
	_ = os.MkdirAll(d, 0o755)
	_ = os.Chmod(d, 0o755)
	if _, err := SecretsDir(); err == nil || !strings.Contains(err.Error(), "droits") {
		t.Errorf("coffre en 0755 : refus « droits » attendu, obtenu %v", err)
	}
	priv, pub, _ := generateKeyPair("a")
	if _, _, err := SecretSetKey("srv", string(priv), string(pub), false, SecretWrite{}); err == nil {
		t.Error("écriture dans un coffre en 0755 acceptée")
	}
	mustNotExist201(t, filepath.Join(d, "srv"))
}

func TestCoffreCheminRelatifRefuse201(t *testing.T) {
	sharedEnv201(t)
	t.Setenv(SecretsDirVar, "relatif/secrets")
	if _, err := SecretsDir(); err == nil {
		t.Error("chemin non absolu accepté")
	}
}

func TestCoffreRefusSansJournalNiFichier201(t *testing.T) {
	root := sharedEnv201(t)
	t.Setenv(SecretsDirVar, filepath.Join(root, "PROJETS", "p", "secrets"))
	priv, pub, _ := generateKeyPair("a")
	_, _, _ = SecretSetKey("srv", string(priv), string(pub), false, SecretWrite{})
	mustNotExist201(t, filepath.Join(root, "PROJETS", "p", "secrets"))
	mustNotExist201(t, filepath.Join(os.Getenv(CacheDirVar), secretJournal))
}

// kubeconfig.path : admis dans le coffre global ou un PRIVATE/ de projet, refusé ailleurs sous la racine partagée.
func TestKubeconfigPathSousRacine201(t *testing.T) {
	root := sharedEnv201(t)
	vault, err := SecretsDir()
	if err != nil {
		t.Fatal(err)
	}
	ok := []string{
		filepath.Join(vault, "autre", "kubeconfig"),
		filepath.Join(root, "PROJETS", "p", "PRIVATE", "QUALIF", "kubeconfig"),
		filepath.Join(root, "PROJETS", "p", "PRIVATE", "GLOBAL", "kubeconfig"),
	}
	ko := []string{
		filepath.Join(root, "PROJETS", "p", "kubeconfig"),
		filepath.Join(root, "PROJETS", "p", "deploy", "kubeconfig"),
		filepath.Join(root, "autre", "kubeconfig"),
	}
	for _, p := range ok {
		b := BackendConf{Name: "k", Kubeconfig: KubeconfigRef{Path: p}}
		if got, err := b.kubeconfigFile(); err != nil || got != p {
			t.Errorf("kubeconfig %s : admis attendu, obtenu %q %v", p, got, err)
		}
	}
	for _, p := range ko {
		b := BackendConf{Name: "k", Kubeconfig: KubeconfigRef{Path: p}}
		if _, err := b.kubeconfigFile(); err == nil {
			t.Errorf("kubeconfig %s : refus attendu sous la racine partagée", p)
		}
	}
	// hors racine partagée : inchangé (Secret monté)
	if _, err := (BackendConf{Name: "k", Kubeconfig: KubeconfigRef{Path: filepath.Join(t.TempDir(), "kubeconfig")}}).kubeconfigFile(); err != nil {
		t.Errorf("kubeconfig hors racine partagée refusé : %v", err)
	}
	// un lien dans PRIVATE/ vers un fichier hors zone admise ne contourne pas le refus
	link := filepath.Join(root, "PROJETS", "p", "PRIVATE", "QUALIF", "lien")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	_ = os.Symlink(filepath.Join(root, "PROJETS", "p", "kubeconfig"), link)
	if _, err := (BackendConf{Name: "k", Kubeconfig: KubeconfigRef{Path: link}}).kubeconfigFile(); err == nil {
		t.Error("lien PRIVATE/ vers un fichier de projet hors zone admise accepté")
	}
}

// /files ne sert jamais le coffre ni PRIVATE/ : test obligatoire du contrat. Le coffre (id, id.pub, known_hosts, kubeconfig)
// ne se reconnaît pas à son nom de fichier : c'est son dossier qui doit être exclu.
func TestFilesRefuseCoffreEtPrivate201(t *testing.T) {
	for _, rel := range []string{
		".config/claude-launcher-ng2/secrets/srv/id",
		".config/claude-launcher-ng2/secrets/srv/id.pub",
		".config/claude-launcher-ng2/secrets/srv/known_hosts",
		".config/claude-launcher-ng2/secrets/srv/kubeconfig",
		".config/claude-launcher-ng2/secrets/srv",
		".config/claude-launcher-ng2/secrets",
		"PROJETS/p/PRIVATE/GLOBAL/acces.md",
		"PROJETS/p/PRIVATE/QUALIF/deploy_key",
		"PROJETS/p/private/QUALIF/x", // casse
	} {
		if !sensitiveRel(rel) {
			t.Errorf("%s : jamais servi attendu", rel)
		}
	}
	if sensitiveRel("PROJETS/p/README.md") {
		t.Error("un fichier ordinaire doit rester servi")
	}
}
