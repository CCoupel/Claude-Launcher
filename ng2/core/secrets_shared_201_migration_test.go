//go:build !windows && secretsmigration

package core

// Migration v6.4.0 → coffre commun (#201). Build tag `secretsmigration` : le point d'entrée n'existait pas à l'écriture
// (dev-backend codait en parallèle). Retirer le tag une fois l'API posée ; signature ATTENDUE (à ajuster au code final) :
//
//	func MigrateLegacySecrets() (conflicts []string, err error)
//
// legacy = CL_SECRETS_LEGACY_DIR (ou emplacement v6.4.0) ; global = coffre commun ; journal = <CacheDir>/secrets-journal.jsonl.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func legacy201(t *testing.T, files map[string]string) (legacy string) {
	t.Helper()
	legacy = filepath.Join(t.TempDir(), "legacy")
	for rel, c := range files {
		p := filepath.Join(legacy, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CL_SECRETS_LEGACY_DIR", legacy)
	return legacy
}

func journal201(t *testing.T) string {
	b, _ := os.ReadFile(filepath.Join(os.Getenv(CacheDirVar), secretJournal))
	return string(b)
}

func TestMigrationCopieBackendAbsent201(t *testing.T) {
	sharedEnv201(t)
	legacy := legacy201(t, map[string]string{"srv/id": "PRIV", "srv/id.pub": "PUB", "srv/known_hosts": "KH"})
	if _, err := MigrateLegacySecrets(); err != nil {
		t.Fatal(err)
	}
	vault, _ := SecretsDir()
	for f, want := range map[string]string{"id": "PRIV", "id.pub": "PUB", "known_hosts": "KH"} {
		p := filepath.Join(vault, "srv", f)
		b, err := os.ReadFile(p)
		if err != nil || string(b) != want {
			t.Errorf("%s : %q %v", p, b, err)
		}
		if st, _ := os.Stat(p); st == nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s : 0600 attendu", p)
		}
	}
	if _, err := os.Stat(filepath.Join(legacy, "srv", "id")); err != nil {
		t.Error("l'ancien coffre doit être conservé")
	}
	j := journal201(t)
	if !strings.Contains(j, "migrate") || strings.Contains(j, "PRIV") {
		t.Errorf("journal : action migrate attendue, contenu jamais journalisé : %s", j)
	}
}

func TestMigrationConflitGlobalGagne201(t *testing.T) {
	sharedEnv201(t)
	vault, _ := SecretsDir()
	_ = os.MkdirAll(filepath.Join(vault, "srv"), 0o700)
	_ = os.WriteFile(filepath.Join(vault, "srv", "id"), []byte("GLOBAL"), 0o600)
	legacy201(t, map[string]string{"srv/id": "ANCIEN"})
	c1, err := MigrateLegacySecrets()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(vault, "srv", "id")); string(b) != "GLOBAL" {
		t.Errorf("le coffre global doit gagner : %q", b)
	}
	if len(c1) != 1 || c1[0] != "srv" {
		t.Errorf("écart à signaler pour srv : %v", c1)
	}
	if n := strings.Count(journal201(t), "srv"); n == 0 {
		t.Error("écart non journalisé")
	}
	// signalement UNIQUE : un second passage (autre démarrage) ne le renouvelle pas
	if c2, _ := MigrateLegacySecrets(); len(c2) != 0 {
		t.Errorf("écart déjà signalé : %v", c2)
	}
}

func TestMigrationInstancesConcurrentes201(t *testing.T) {
	sharedEnv201(t)
	legacy201(t, map[string]string{"srv/id": "PRIV", "srv/id.pub": "PUB", "b2/id": "P2"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = MigrateLegacySecrets() }()
	}
	wg.Wait()
	vault, _ := SecretsDir()
	for f, want := range map[string]string{"srv/id": "PRIV", "srv/id.pub": "PUB", "b2/id": "P2"} {
		if b, err := os.ReadFile(filepath.Join(vault, f)); err != nil || string(b) != want {
			t.Errorf("%s : %q %v (fichier partiel ou absent)", f, b, err)
		}
	}
	if n := strings.Count(journal201(t), "migrate"); n != 3 && n != 2 { // une entrée par backend (2) ou par fichier (3), jamais de doublon
		t.Errorf("doublons au journal : %d entrées migrate", n)
	}
	ents, _ := os.ReadDir(filepath.Join(vault, "srv"))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("fichier temporaire résiduel : %s", e.Name())
		}
	}
}
