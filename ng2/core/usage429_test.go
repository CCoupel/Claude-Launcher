package core

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
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
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
	if NeedsFetch(120) {
		t.Error("429 : attente non respectée")
	}
	old := time.Now().Add(-usageMinBackoff - time.Second)
	_ = os.Chtimes(p+".err", old, old)
	_ = os.Remove(p)
	if !NeedsFetch(120) {
		t.Error("attente finie : appel attendu")
	}
}

func TestLinkUsageCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "shared", ".claude"))
	dir := filepath.Join(home, ".cache", "claude-launcher")
	_ = os.MkdirAll(filepath.Join(dir, "state"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "navwidth"), []byte("40"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "usage.json"), []byte(`{"limits":[]}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "usage.json.err"), []byte("trop d’appels"), 0o644)
	fut := time.Now().Add(10 * time.Minute)
	_ = os.Chtimes(filepath.Join(dir, "usage.json.err"), fut, fut)
	_ = os.WriteFile(filepath.Join(dir, "usage.json.wait"), []byte("600"), 0o644)
	LinkUsageCache()
	for _, n := range []string{"usage.json", "usage.json.err"} {
		if l, err := os.Readlink(filepath.Join(dir, n)); err != nil || l != filepath.Join(sharedUsageDir(), n) {
			t.Errorf("%s : lien %q %v", n, l, err)
		}
	}
	if b, _ := os.ReadFile(usageCachePath()); !strings.Contains(string(b), "limits") {
		t.Error("cache local non repris")
	}
	if st, _ := os.Stat(usageCachePath() + ".err"); st == nil || st.ModTime().Before(fut.Add(-time.Second)) {
		t.Error("attente (date de .err) perdue")
	}
	if b, _ := os.ReadFile(usageCachePath() + ".wait"); string(b) != "600" {
		t.Error(".wait non repris")
	}
	if _, err := os.Stat(filepath.Join(dir, "navwidth")); err != nil {
		t.Error("autres fichiers touchés")
	}
	// le hub écrit dans le commun : le panneau le lit par le lien
	_ = os.WriteFile(usageCachePath()+".tmp", []byte(`{"limits":[{"kind":"session"}]}`), 0o644)
	_ = os.Rename(usageCachePath()+".tmp", usageCachePath())
	if b, _ := os.ReadFile(filepath.Join(dir, "usage.json")); !strings.Contains(string(b), "session") {
		t.Error("lecture par le lien")
	}
	// lien remplacé par un fichier (écriture par remplacement) : rétabli
	_ = os.Remove(filepath.Join(dir, "usage.json.err"))
	_ = os.WriteFile(filepath.Join(dir, "usage.json.err"), []byte("x"), 0o644)
	LinkUsageCache()
	if _, err := os.Readlink(filepath.Join(dir, "usage.json.err")); err != nil {
		t.Error("lien non rétabli")
	}
}

// Au lancement du hub, un 429 noté (par un autre launcher, arrêté depuis) ne
// bloque plus le premier relevé.
func TestResetUsageBackoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	p := usageCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"limits":[]}`), 0o644)
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p, old, old)
	_ = os.WriteFile(p+".err", []byte("trop d’appels à l’endpoint, nouvel essai dans quelques minutes"), 0o644)
	_ = os.WriteFile(p+".wait", []byte("1800"), 0o644)
	if NeedsFetch(120) {
		t.Fatal("429 récent : pas d'appel attendu avant le reset")
	}
	ResetUsageBackoff()
	if !NeedsFetch(120) {
		t.Fatal("après le reset du lancement : appel attendu")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("usage.json ne doit pas être effacé")
	}
	if _, err := os.Stat(p + ".wait"); err == nil {
		t.Fatal(".wait non effacé")
	}
}
