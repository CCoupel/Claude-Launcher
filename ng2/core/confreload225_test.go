package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bacs : CL_CONFIG_DIR et CLAUDE_CONFIG_DIR temporaires — jamais le vrai config.conf ni le vrai settings.json.
func confBench225(t *testing.T) (conf, settings string) {
	t.Helper()
	unixOnly(t)
	root := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	_ = os.MkdirAll(Ng2ConfigDir(), 0o755)
	return ConfigPath(), filepath.Join(ClaudeDir(), "settings.json")
}

// editConf : réécrit config.conf avec une date distincte (la résolution du système de fichiers ne doit pas masquer l'édition).
func editConf(t *testing.T, path, content string, plus time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(plus)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// FreshConfig : inchangé → même copie ; modifié → relu (racines gardées) ; invalide → dernière copie valide + message, une fois.
func TestFreshConfig225(t *testing.T) {
	conf, _ := confBench225(t)
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\nUSAGE_REFRESH_SEC=100\n", 0)
	cur := LoadConfig()
	cur.Roots = []string{"/racine/figée"}
	if c, w := FreshConfig(cur); w != "" || c.UsageRefreshSec != 100 || !c.Stamp.Same(cur.Stamp) {
		t.Fatalf("inchangé : %+v %q", c.UsageRefreshSec, w)
	}
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=1\nUSAGE_REFRESH_SEC=200\n", 5*time.Second)
	c, w := FreshConfig(cur)
	if w != "" || c.UsageRefreshSec != 200 || !c.DisableMouse || len(c.Roots) != 1 || c.Roots[0] != "/racine/figée" {
		t.Fatalf("modifié : usage=%d mouse=%v roots=%v warn=%q", c.UsageRefreshSec, c.DisableMouse, c.Roots, w)
	}
	// invalide : ancienne copie gardée, dite une seule fois
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n", 10*time.Second)
	if ps := ValidateConfig(ConfConf, readFileBytes(t, conf)); !HasErrors(ps) {
		t.Skip("contenu jugé valide par la validation actuelle")
	}
	bad, w := FreshConfig(c)
	if bad.UsageRefreshSec != 200 || !bad.DisableMouse {
		t.Errorf("config invalide : copie valide perdue (%d, souris %v)", bad.UsageRefreshSec, bad.DisableMouse)
	}
	if w == "" {
		t.Fatal("aucun message pour un config.conf invalide")
	}
	if _, w2 := FreshConfig(bad); w2 != "" {
		t.Errorf("message répété : %q", w2)
	}
}

func readFileBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Édition de MÊME TAILLE dans la même seconde (date grossière : 9P, MooseFS) : la date et la taille ne changent pas, le contenu
// si — vue dans la fenêtre « racy » (cas souris 0→1). Hors fenêtre (copie lue longtemps après la date du fichier), la date
// fait foi et le fichier n'est pas relu.
func TestConfigChangedMemeTailleMemeDate225(t *testing.T) {
	conf, _ := confBench225(t)
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\n", 0)
	fixed := time.Now().Add(-time.Second) // date grossière : arrondie, identique avant et après l'édition
	_ = os.Chtimes(conf, fixed, fixed)
	cur := LoadConfig()
	if cur.DisableMouse || ConfigChanged(cur.Stamp) {
		t.Fatal("précondition")
	}
	if err := os.WriteFile(conf, []byte("CLAUDE_DISABLE_MOUSE=1\n"), 0o600); err != nil { // même taille
		t.Fatal(err)
	}
	_ = os.Chtimes(conf, fixed, fixed) // même date : date ET taille identiques
	st := ConfigStamp()
	if st.MTime != cur.Stamp.MTime && !st.MTime.Equal(cur.Stamp.MTime) || st.Size != cur.Stamp.Size {
		t.Fatal("la précondition « même date, même taille » n'est pas réalisée")
	}
	if !ConfigChanged(cur.Stamp) {
		t.Fatal("édition de même taille dans la fenêtre racy non vue")
	}
	if c, _ := FreshConfig(cur); !c.DisableMouse {
		t.Error("FreshConfig n'a pas pris la nouvelle valeur (souris 0→1)")
	}
	// hors fenêtre : copie lue 10 s après la date du fichier → la date fait foi, aucune relecture du contenu
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(conf, old, old)
	cur = LoadConfig()
	cur.Stamp.ReadAt = old.Add(10 * time.Second)
	_ = os.WriteFile(conf+".x", nil, 0o600)
	if ConfigChanged(cur.Stamp) {
		t.Error("fichier inchangé hors fenêtre racy : aucun changement attendu")
	}
}

// Erreur transitoire (config.conf illisible) : la copie en mémoire est gardée, PAS les valeurs par défaut ; disparu : défauts.
func TestFreshConfigIllisibleGardeLaCopie225(t *testing.T) {
	conf, _ := confBench225(t)
	if os.Geteuid() == 0 {
		t.Skip("root lit tout")
	}
	editConf(t, conf, "USAGE_REFRESH_SEC=333\nCLAUDE_DISABLE_MOUSE=1\n", 0)
	cur := LoadConfig()
	editConf(t, conf, "USAGE_REFRESH_SEC=444\nCLAUDE_DISABLE_MOUSE=1\n", 5*time.Second)
	if err := os.Chmod(conf, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(conf, 0o600)
	c, w := FreshConfig(cur)
	if w != "" || c.UsageRefreshSec != 333 || !c.DisableMouse {
		t.Fatalf("illisible : usage=%d souris=%v warn=%q (copie gardée attendue)", c.UsageRefreshSec, c.DisableMouse, w)
	}
	_ = os.Chmod(conf, 0o600)
	if c, _ := FreshConfig(cur); c.UsageRefreshSec != 444 {
		t.Errorf("lisible de nouveau : %d", c.UsageRefreshSec)
	}
}

// Config construite à la main (sans Stamp) avec un config.conf existant : tenue pour périmée (invariant documenté).
func TestFreshConfigSansStampEstPeriméee225(t *testing.T) {
	conf, _ := confBench225(t)
	editConf(t, conf, "USAGE_REFRESH_SEC=555\n", 0)
	c, _ := FreshConfig(Config{UsageRefreshSec: 1})
	if c.UsageRefreshSec != 555 {
		t.Errorf("Config{} sans Stamp devait être relue : %d", c.UsageRefreshSec)
	}
}

// config.conf ABSENT (éditeur : supprimer puis recréer) : copie gardée pendant le délai de grâce — jamais un jeton vide recopié ;
// absent durablement : valeurs par défaut ; revenu : relu.
func TestFreshConfigAbsentDelaiDeGrace225(t *testing.T) {
	conf, settings := confBench225(t)
	old := confAbsentGrace
	confAbsentGrace = 300 * time.Millisecond
	defer func() { confAbsentGrace = old }()
	noteConfPresent()
	editConf(t, conf, "GITHUB_TOKEN=ghp_FAKE_TEST_grace\nUSAGE_REFRESH_SEC=77\n", 0)
	cur := LoadConfig()
	if cur.GithubToken != "ghp_FAKE_TEST_grace" {
		t.Fatalf("précondition : %q", cur.GithubToken)
	}
	_ = os.Remove(conf)
	c, _ := FreshConfig(cur)
	if c.GithubToken != "ghp_FAKE_TEST_grace" || c.UsageRefreshSec != 77 {
		t.Fatalf("absence récente : copie perdue (%q, %d)", c.GithubToken, c.UsageRefreshSec)
	}
	if !SyncLauncherEnv(c) {
		t.Fatal("recopie")
	}
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), "ghp_FAKE_TEST_grace") {
		t.Errorf("jeton retiré de settings.json pendant l'absence :\n%s", b)
	}
	// recréé dans le délai : relu normalement
	editConf(t, conf, "GITHUB_TOKEN=ghp_FAKE_TEST_grace\nUSAGE_REFRESH_SEC=88\n", 5*time.Second)
	if c, _ := FreshConfig(cur); c.UsageRefreshSec != 88 {
		t.Errorf("recréé : %d", c.UsageRefreshSec)
	}
	// absent durablement : défauts après le délai
	_ = os.Remove(conf)
	FreshConfig(cur)
	time.Sleep(400 * time.Millisecond)
	if c, _ := FreshConfig(cur); c.GithubToken != "" {
		t.Errorf("absent durablement : défauts attendus, jeton %q", c.GithubToken)
	}
}

// Dérive d'horloge : date du fichier dans le FUTUR du client (serveur en avance) = « racy » en permanence, le contenu tranche
// (même taille, même date) ; date nettement ancienne : la date fait foi.
func TestConfigChangedDeriveHorloge225(t *testing.T) {
	conf, _ := confBench225(t)
	future := time.Now().Add(time.Hour)
	editConf(t, conf, "CLAUDE_DISABLE_MOUSE=0\n", time.Hour)
	cur := LoadConfig()
	if ConfigChanged(cur.Stamp) {
		t.Fatal("précondition")
	}
	if err := os.WriteFile(conf, []byte("CLAUDE_DISABLE_MOUSE=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(conf, future, future)
	if st := ConfigStamp(); !st.MTime.Equal(cur.Stamp.MTime) || st.Size != cur.Stamp.Size {
		t.Skip("même date non réalisable ici")
	}
	if !ConfigChanged(cur.Stamp) {
		t.Error("date dans le futur : édition de même taille non vue")
	}
}
