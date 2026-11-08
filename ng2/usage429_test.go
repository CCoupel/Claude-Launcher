package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUsage429Backoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	p := usageCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	// attente doublée à chaque 429, plafonnée ; Retry-After l'emporte
	for _, want := range []time.Duration{5, 10, 20, 30, 30} {
		if w := tooMany(p, ""); w != want*time.Minute {
			t.Errorf("attente %v, attendu %v min", w, want)
		}
	}
	if w := tooMany(p, "900"); w != 15*time.Minute {
		t.Errorf("Retry-After 900 : %v", w)
	}
	// .err daté dans le futur : pas d'appel avant la fin de l'attente, même si
	// claude a renouvelé son jeton entre-temps
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.WriteFile(p+".err", []byte("trop d’appels à l’endpoint, nouvel essai vers 10:52"), 0o644)
	at := time.Now().Add(20*time.Minute - usageMinBackoff)
	_ = os.Chtimes(p+".err", at, at)
	_ = os.WriteFile(credentialsPath(), []byte("{}"), 0o600)
	_ = os.Chtimes(credentialsPath(), time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	if needsFetch(120) {
		t.Error("429 : attente non respectée")
	}
	old := time.Now().Add(-usageMinBackoff - time.Second)
	_ = os.Chtimes(p+".err", old, old)
	_ = os.Remove(p)
	if !needsFetch(120) {
		t.Error("attente finie : appel attendu")
	}
}

func TestLinkUsageCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "shared", ".claude"))
	old := filepath.Join(home, ".cache", "claude-launcher")
	_ = os.MkdirAll(old, 0o755)
	_ = os.WriteFile(filepath.Join(old, "usage.json"), []byte(`{"limits":[]}`), 0o644)
	_ = os.WriteFile(filepath.Join(old, "usage.json.err"), []byte("x"), 0o644)
	linkUsageCache()
	if t2, err := os.Readlink(old); err != nil || t2 != sharedUsageDir() {
		t.Fatalf("lien : %q %v", t2, err)
	}
	if b, _ := os.ReadFile(filepath.Join(sharedUsageDir(), "usage.json")); !strings.Contains(string(b), "limits") {
		t.Error("cache existant non repris")
	}
	linkUsageCache() // idempotent
	if t2, _ := os.Readlink(old); t2 != sharedUsageDir() {
		t.Error("lien perdu")
	}
	// dossier avec autre chose : intact
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	other := filepath.Join(home2, ".cache", "claude-launcher")
	_ = os.MkdirAll(other, 0o755)
	_ = os.WriteFile(filepath.Join(other, "autre"), nil, 0o644)
	linkUsageCache()
	if _, err := os.Readlink(other); err == nil {
		t.Error("dossier avec d'autres fichiers remplacé")
	}
}
