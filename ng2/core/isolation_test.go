package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// La garde refuse (et consigne) toute écriture hors d'un dossier temporaire ; l'erreur est rendue ET enregistrée, de sorte
// que le TestMain échoue même si l'appelant l'ignore.
func TestGardeRefuseLEcritureHorsTemp(t *testing.T) {
	if !isolationOn() {
		t.Fatal("la garde doit être active sous test (TestMain)")
	}
	ResetIsolationViolations()
	defer ResetIsolationViolations() // ces violations VOULUES ne doivent pas faire échouer le TestMain
	// un chemin hors du dossier temporaire : le répertoire de travail du test (le paquet), jamais écrit pour de bon
	wd, _ := os.Getwd()
	for name, fn := range map[string]func(string) error{
		"WriteFileAtomic": func(p string) error { return WriteFileAtomic(p, []byte("x"), 0o600) },
		"createExclusive": func(p string) error { _, e := createExclusive(p, []byte("x"), 0o600); return e },
		"SyncSettingsEnv": func(p string) error { return SyncSettingsEnv(filepath.Dir(p), []envVar{{"GH_TOKEN", "FAKE"}}) },
	} {
		p := filepath.Join(wd, "ne-doit-pas-exister-"+name)
		if err := fn(p); err == nil || !strings.Contains(err.Error(), "hors d'un dossier temporaire") {
			t.Errorf("%s : erreur = %v", name, err)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s : fichier créé hors du dossier temporaire", name)
			os.Remove(p)
		}
	}
	if _, err := os.Stat(filepath.Join(wd, "settings.json")); err == nil {
		t.Error("settings.json créé dans le dossier du paquet")
	}
	if v := IsolationViolations(); len(v) < 3 {
		t.Errorf("violations consignées = %v", v)
	}
	// sous le dossier temporaire : permis
	ResetIsolationViolations()
	d := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(d, "a"), []byte("x"), 0o600); err != nil {
		t.Errorf("écriture sous TempDir refusée : %v", err)
	}
	if err := SyncSettingsEnv(d, []envVar{{"GH_TOKEN", "FAKE"}}); err != nil {
		t.Errorf("synchronisation sous TempDir refusée : %v", err)
	}
	if v := IsolationViolations(); len(v) != 0 {
		t.Errorf("violation sur un chemin temporaire : %v", v)
	}
}

// Le CLAUDE_CONFIG_DIR « réel » d'une session (hors TempDir) n'est jamais écrit sous test, même par l'ouverture d'un projet :
// SyncLauncherEnv rend false et consigne.
func TestSyncLauncherEnvVersLeVraiDossierRefuse(t *testing.T) {
	ResetIsolationViolations()
	defer ResetIsolationViolations()
	wd, _ := os.Getwd()
	reel := filepath.Join(wd, "faux-dossier-claude-reel")
	t.Setenv("CLAUDE_CONFIG_DIR", reel)
	if SyncLauncherEnv(Config{GithubToken: "FAKE-JETON"}) {
		t.Error("SyncLauncherEnv a réussi vers un dossier hors TempDir")
	}
	if _, err := os.Stat(filepath.Join(reel, "settings.json")); err == nil {
		t.Error("settings.json écrit hors TempDir")
	}
	os.RemoveAll(reel)
}

// Journal des écritures de secrets : empreinte et longueur, jamais la valeur ; une ligne par clé secrète modifiée.
func TestJournalDesEcrituresDeSecrets(t *testing.T) {
	d := t.TempDir()
	const tok = "ghp_FAKE_journal_test_value_1234567890"
	vars := []envVar{{"GH_TOKEN", tok}, {"GITHUB_PERSONAL_ACCESS_TOKEN", tok}, {"CLAUDE_CODE_DISABLE_MOUSE", "1"}}
	if err := SyncSettingsEnv(d, vars); err != nil {
		t.Fatal(err)
	}
	read := func() string { b, _ := os.ReadFile(settingsJournalPath(d)); return string(b) }
	j := read()
	if strings.Contains(j, tok) || strings.Contains(j, "ghp_") {
		t.Fatalf("le journal contient la valeur : %s", j)
	}
	lines := strings.Split(strings.TrimSpace(j), "\n")
	if len(lines) != 2 { // deux clés secrètes ; la clé non secrète n'est pas journalisée
		t.Fatalf("lignes = %d : %s", len(lines), j)
	}
	for _, want := range []string{`"key":"GH_TOKEN"`, `"len":38`, `"fp":"` + secretFp(tok) + `"`, `"pid":`, `"bin":`, `"version":`, `"cmd":`, `"host":`, `"at":`} {
		if !strings.Contains(j, want) {
			t.Errorf("journal sans %s : %s", want, j)
		}
	}
	// identique : aucune nouvelle ligne
	_ = SyncSettingsEnv(d, vars)
	if read() != j {
		t.Error("écriture journalisée sans changement")
	}
	// valeur modifiée : nouvelle ligne avec l'empreinte précédente (prev)
	const tok2 = "ghp_FAKE_journal_test_value_AUTRE"
	_ = SyncSettingsEnv(d, []envVar{{"GH_TOKEN", tok2}, {"GITHUB_PERSONAL_ACCESS_TOKEN", tok}, {"CLAUDE_CODE_DISABLE_MOUSE", "1"}})
	j2 := read()
	if strings.Contains(j2, tok2) || !strings.Contains(j2, `"prev":"`+secretFp(tok)+`"`) || !strings.Contains(j2, secretFp(tok2)) {
		t.Errorf("journal après changement : %s", j2)
	}
	// changement d'une clé NON secrète : pas de ligne
	_ = SyncSettingsEnv(d, []envVar{{"GH_TOKEN", tok2}, {"GITHUB_PERSONAL_ACCESS_TOKEN", tok}, {"CLAUDE_CODE_DISABLE_MOUSE", "0"}})
	if read() != j2 {
		t.Error("clé non secrète journalisée")
	}
	if st, err := os.Stat(settingsJournalPath(d)); err != nil || st.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Errorf("droits du journal : %v %v", st, err)
	}
}
