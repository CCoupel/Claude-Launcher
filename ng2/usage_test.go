package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Après un /login, l'échec « non authentifié » ne doit plus bloquer les appels.
func TestNeedsFetchApresLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	_ = os.MkdirAll(filepath.Dir(usageCachePath()), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)

	errPath := usageCachePath() + ".err"
	_ = os.WriteFile(credentialsPath(), []byte("{}"), 0o600)
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(credentialsPath(), old, old)
	_ = os.WriteFile(errPath, []byte("non authentifié"), 0o644)
	if needsFetch(60) {
		t.Fatal("échec récent sans nouveau login : l'appel devrait être suspendu")
	}

	_ = os.Chtimes(credentialsPath(), time.Now().Add(time.Second), time.Now().Add(time.Second))
	if !needsFetch(60) {
		t.Fatal("identifiants plus récents que l'échec : l'appel devrait être relancé")
	}
}

func TestLoginAlerte(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if u := readUsage(); u.Expired || !u.LoginBy.IsZero() {
		t.Fatal("sans identifiants : rien à signaler")
	}
	write := func(refresh time.Duration) {
		acc := time.Now().Add(-time.Hour).UnixMilli() // jeton d'accès périmé : sans effet
		_ = os.WriteFile(credentialsPath(), []byte(fmt.Sprintf(
			`{"claudeAiOauth":{"expiresAt":%d,"refreshTokenExpiresAt":%d}}`, acc, time.Now().Add(refresh).UnixMilli())), 0o600)
	}
	write(20 * 24 * time.Hour)
	if u := readUsage(); u.Expired || !u.LoginBy.IsZero() {
		t.Fatal("connexion valide 20 jours : pas d'alerte")
	}
	write(2 * 24 * time.Hour)
	if u := readUsage(); u.Expired || u.LoginBy.IsZero() {
		t.Fatal("expire dans 2 jours : alerte jaune attendue")
	}
	write(-time.Minute)
	if u := readUsage(); !u.Expired {
		t.Fatal("connexion expirée : alerte rouge attendue")
	}
}
