package core

// #225 : settings.json ne reçoit JAMAIS une valeur plus ancienne que config.conf. SyncLauncherEnv reçoit une copie en mémoire
// (Config) qui peut être périmée (config.conf édité à la main depuis sa lecture) : il compare la date de la config utilisée à
// celle du fichier et relit avant d'écrire. Une config.conf invalide garde la dernière copie valide (rien n'est effacé).
// Cas utilisateur : CLAUDE_DISABLE_MOUSE 0 → 1 à la main, puis ouverture d'un projet. CLAUDE_CONFIG_DIR temporaire.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const mouseVar225 = "CLAUDE_CODE_DISABLE_MOUSE"

func conf225(t *testing.T, mouse string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(), []byte("CLAUDE_DISABLE_MOUSE="+mouse+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ConfigPath(), at, at); err != nil { // dates distinctes : jamais deux écritures « dans la même seconde »
		t.Fatal(err)
	}
}

func env225(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ClaudeDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings.json : %v", err)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("settings.json illisible : %v\n%s", err, b)
	}
	if doc.Env == nil {
		doc.Env = map[string]string{}
	}
	return doc.Env
}

func home225(t *testing.T) time.Time {
	t.Helper()
	unixOnly(t)
	withHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // jamais le vrai settings.json
	return time.Now().Add(-time.Hour)
}

// Souris 0 → 1 à la main : une copie lue AVANT l'édition (souris active = ne pas poser la variable) ne doit pas effacer la
// nouvelle valeur ; et dans l'autre sens (1 → 0) elle ne doit pas la ré-écrire.
func TestSyncLauncherEnvJamaisPlusAncien225(t *testing.T) {
	for name, c := range map[string]struct{ avant, apres string }{"0 vers 1": {"0", "1"}, "1 vers 0": {"1", "0"}} {
		t.Run(name, func(t *testing.T) {
			t0 := home225(t)
			conf225(t, c.avant, t0)
			stale := LoadConfig() // copie mémoire du launcher, lue avant l'édition
			if stale.DisableMouse != (c.avant == "1") {
				t.Fatalf("copie de départ : DisableMouse=%v pour %s", stale.DisableMouse, c.avant)
			}
			if !SyncLauncherEnv(stale) {
				t.Fatal("recopie initiale en échec")
			}
			conf225(t, c.apres, t0.Add(10*time.Second)) // édition extérieure, SANS relancer ni relire la copie
			if !SyncLauncherEnv(stale) {
				t.Fatal("recopie avec copie périmée en échec")
			}
			_, has := env225(t)[mouseVar225]
			if want := c.apres == "1"; has != want {
				t.Errorf("souris %s : settings.json %s %s, la valeur du FICHIER (%s) devait l'emporter sur la copie périmée", name, map[bool]string{true: "porte", false: "ne porte pas"}[has], mouseVar225, c.apres)
			}
		})
	}
}

// Copie à jour : rien n'est relu ni modifié (la valeur de la copie est celle du fichier).
func TestSyncLauncherEnvCopieAJourInchangee225(t *testing.T) {
	t0 := home225(t)
	conf225(t, "1", t0)
	cfg := LoadConfig()
	for i := 0; i < 3; i++ {
		if !SyncLauncherEnv(cfg) {
			t.Fatal("recopie en échec")
		}
	}
	if env225(t)[mouseVar225] != "1" {
		t.Errorf("settings.json : %v", env225(t))
	}
}

// Config construite à la main (aucun config.conf sur le poste) : ses valeurs sont recopiées telles quelles (non-régression
// des appels existants).
func TestSyncLauncherEnvSansFichierConfig225(t *testing.T) {
	home225(t)
	if _, err := os.Stat(ConfigPath()); err == nil {
		t.Fatal("config.conf présent : banc non isolé")
	}
	if !SyncLauncherEnv(Config{DisableMouse: true, ExpTeams: true}) {
		t.Fatal("recopie en échec")
	}
	if e := env225(t); e[mouseVar225] != "1" || e["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] != "1" {
		t.Errorf("settings.json : %v", e)
	}
}

// config.conf devenu INVALIDE (erreur de syntaxe) : la dernière copie valide est gardée — settings.json n'est ni effacé ni
// remplacé par des valeurs par défaut ; une fois corrigé, la nouvelle valeur est prise.
func TestSyncLauncherEnvConfigInvalideGardeLAncienneCopie225(t *testing.T) {
	t0 := home225(t)
	conf225(t, "1", t0)
	stale := LoadConfig()
	if !SyncLauncherEnv(stale) || env225(t)[mouseVar225] != "1" {
		t.Fatalf("départ : %v", env225(t))
	}
	bad := "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n"
	if p := ValidateConfig(ConfConf, []byte(bad)); len(p) == 0 {
		t.Skip("contenu jugé valide par ValidateConfig : choisir une autre erreur de syntaxe")
	}
	if err := os.WriteFile(ConfigPath(), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(ConfigPath(), t0.Add(10*time.Second), t0.Add(10*time.Second))
	_ = SyncLauncherEnv(stale)
	if env225(t)[mouseVar225] != "1" {
		t.Errorf("config invalide : settings.json = %v, attendu la dernière copie valide (souris à 1) conservée", env225(t))
	}
	conf225(t, "0", t0.Add(20*time.Second)) // corrigé
	_ = SyncLauncherEnv(stale)
	if _, has := env225(t)[mouseVar225]; has {
		t.Errorf("config corrigée (souris 0) : settings.json porte encore %s : %v", mouseVar225, env225(t))
	}
}
