package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LOT-3.2 jalon 4 (test-writer) — couche core de l'ouverture d'un projet d'utilisateur : cas non couverts par les tests du dev (faux docker à
// état : uctrFake). Approbation copiée d'un autre projet, image ≠ défaut, run_as du backend sur chaque exec.

func t4Open(t *testing.T, e Engine, key []byte) (CtrOpen, error) {
	t.Helper()
	return e.OpenUserProject(key, "cat", "p", CtrOpenIO{Start: func() []byte { return []byte(UserStartCommand("cat", "p")) }, Size: func() (int, int) { return 100, 30 }})
}

func TestT4ApprobationCopieeDUnAutreProjetRefusee(t *testing.T) {
	dir, e := uctrFake(t)
	sp, _ := UserSpace("github-9")
	key, _ := sp.ApprovalKey()
	files := map[string][]byte{"env.yaml": []byte("image: " + UserDefaultImage() + "\n")}
	_ = os.WriteFile(filepath.Join(dir, "files", "env.yaml"), files["env.yaml"], 0o644)
	// trust.json VALIDE mais scellé pour un AUTRE projet (cat/q), copié tel quel sous cat/p
	tr, err := BuildTrust(key, "github-9", e.B.Name, UserProjectDir("cat", "q"), files)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "files", "trust.json"), tr, 0o644)
	var na ErrUserEnvNotApproved
	if _, err := t4Open(t, e, key); !errors.As(err, &na) {
		t.Fatalf("approbation copiée d'un autre projet acceptée : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session")); err == nil {
		t.Error("session créée sur une approbation copiée")
	}
	// scellé pour un autre backend / un autre utilisateur : refusé aussi
	for name, mk := range map[string]func() ([]byte, error){
		"autre backend": func() ([]byte, error) { return BuildTrust(key, "github-9", "autre", UserProjectDir("cat", "p"), files) },
		"autre utilisateur": func() ([]byte, error) {
			return BuildTrust(key, "github-10", e.B.Name, UserProjectDir("cat", "p"), files)
		},
	} {
		b, _ := mk()
		_ = os.WriteFile(filepath.Join(dir, "files", "trust.json"), b, 0o644)
		if _, err := t4Open(t, e, key); !errors.As(err, &na) {
			t.Errorf("%s : approbation acceptée : %v", name, err)
		}
	}
	// témoin : scellé pour le bon projet → s'ouvre
	good, _ := BuildTrust(key, "github-9", e.B.Name, UserProjectDir("cat", "p"), files)
	_ = os.WriteFile(filepath.Join(dir, "files", "trust.json"), good, 0o644)
	if _, err := t4Open(t, e, key); err != nil {
		t.Errorf("approbation valide refusée : %v", err)
	}
}

// D4 : image d'env.yaml ≠ image par défaut → refus nommé, MÊME approuvé ; jamais de session.
func TestT4ImageDifferenteDeLImageParDefaut(t *testing.T) {
	dir, e := uctrFake(t)
	sp, _ := UserSpace("github-9")
	key, _ := sp.ApprovalKey()
	_ = os.WriteFile(filepath.Join(dir, "files", "env.yaml"), []byte("image: debian:12\n"), 0o644)
	fs, _ := e.UserEnvFiles("cat", "p")
	if err := e.ApproveUserEnv(key, "cat", "p", fs); err != nil {
		t.Fatal(err)
	}
	_, err := t4Open(t, e, key)
	if err == nil || !strings.Contains(err.Error(), "image non autorisée : seule l'image par défaut est admise") {
		t.Fatalf("image ≠ défaut : %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "session")); serr == nil {
		t.Error("session créée avec une image non autorisée")
	}
}

// run_as du backend : CHAQUE exec (sauf le bootstrap du conteneur) sous l'uid:gid du backend, jamais 2000 ni root.
func TestT4RunAsDuBackendSurChaqueExec(t *testing.T) {
	dir, e := uctrFake(t)
	e.B.RunAs = &RunAsConf{UID: 3000, GID: 4000}
	sp, _ := UserSpace("github-9")
	key, _ := sp.ApprovalKey()
	if _, err := t4Open(t, e, key); err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	n := 0
	for _, c := range strings.Split(uctrCalls(dir), "\n") {
		if !strings.HasPrefix(c, "exec ") {
			continue
		}
		n++
		if !strings.Contains(c, "--user=3000:4000") || strings.Contains(c, "2000:2000") {
			t.Errorf("exec hors run_as 3000:4000 : %s", c)
		}
	}
	if n == 0 {
		t.Fatal("aucun exec journalisé")
	}
}

// Canari : ni le config.conf, ni les variables, ni l'env.yaml du launcher de l'admin ne passent dans une commande ou dans l'entrée standard d'un exec.
func TestT4AucuneDonneeDeLAdmin(t *testing.T) {
	dir, e := uctrFake(t)
	sp, _ := UserSpace("github-9")
	key, _ := sp.ApprovalKey()
	cfg := os.Getenv("CL_CONFIG_DIR")
	_ = os.WriteFile(filepath.Join(cfg, "config.conf"), []byte("GITHUB_TOKEN=CANARY-conf-1\nCLAUDE_OPTIONS=--x CANARY-conf-2\nSETUP=echo CANARY-conf-3\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cfg, "env.yaml"), []byte("setup:\n  - echo CANARY-conf-4\npackages: [CANARY-conf-5]\n"), 0o644)
	for k, v := range map[string]string{"GH_TOKEN": "CANARY-env-6", "ANTHROPIC_API_KEY": "CANARY-env-7", "CLAUDE_CODE_OAUTH_TOKEN": "CANARY-env-8", "CL_DESKTOP_TOKEN": "CANARY-env-9"} {
		t.Setenv(k, v)
	}
	if _, err := t4Open(t, e, key); err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	if c := uctrCalls(dir); strings.Contains(c, "CANARY") {
		t.Errorf("donnée du launcher / de l'admin dans les commandes d'un utilisateur :\n%s", c)
	}
	files, _ := os.ReadDir(filepath.Join(dir, "files")) // contenu écrit dans le volume (tee) : aucun canari non plus
	for _, f := range files {
		if b, _ := os.ReadFile(filepath.Join(dir, "files", f.Name())); strings.Contains(string(b), "CANARY") {
			t.Errorf("canari écrit dans le volume : %s", f.Name())
		}
	}
}
