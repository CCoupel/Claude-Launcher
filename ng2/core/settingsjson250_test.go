package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #250 (LOT-1) — teamleader : teamleader.md ET .claude/settings.json (model, effortLevel : valeurs résolues), P4.
// contracts/desktop-dispatch.md « v6.9.0 » (teamAgentSet › Teamleader). Helpers : companion250_test.go.

func st250Path(proj string) string { return filepath.Join(proj, ".claude", "settings.json") }

func st250Parse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("settings.json invalide après écriture : %v\n%s", err, s)
	}
	return m
}

// Fichier absent : créé exactement comme dans le contrat, valeurs résolues (compagnon > template).
func TestSettingsAbsentCree250(t *testing.T) {
	proj := c250Proj(t) // template sonnet / medium
	res := c250MustSet(t, proj, "teamleader", s250("opus"), nil)
	if got := c250Read(t, st250Path(proj)); got != "{\n  \"model\": \"opus\",\n  \"effortLevel\": \"medium\"\n}\n" {
		t.Errorf("settings.json créé : %q", got)
	}
	if got := c250Read(t, c250Companion(proj, "teamleader")); got != "---\nmodel: opus\n---\n" {
		t.Errorf("teamleader.md : %q", got)
	}
	st, _ := res["settings"].(map[string]any)
	if st["file"] != ".claude/settings.json" || st["model"] != "opus" || st["effortLevel"] != "medium" {
		t.Errorf("settings du résultat : %v, attendu {file .claude/settings.json, model opus, effortLevel medium}", st)
	}
	note, _ := res["note"].(string)
	if !strings.Contains(note, "settings.json") || !strings.Contains(note, "/model") || !strings.Contains(note, "/effort") {
		t.Errorf("note : %q, attendu le rappel de /model et /effort et settings.json", note)
	}
}

// critical — clés présentes : seules leurs valeurs changent, tout le reste est identique octet pour octet.
func TestSettingsClesPresentesResteIdentique250(t *testing.T) {
	proj := c250Proj(t)
	orig := "{\n" +
		"    \"permissions\"  :  { \"allow\": [\"Bash(ls:*)\"] },\n" +
		"    \"env\": { \"model\": \"sonnet\", \"effortLevel\": \"low\" },\n" +
		"    \"model\"   :   \"sonnet\"  ,\n" +
		"\t\"hooks\": {\"SessionStart\": []},\n" +
		"    \"effortLevel\":\"low\"\n" +
		"}"
	c250Write(t, st250Path(proj), orig)
	c250MustSet(t, proj, "teamleader", s250("opus"), s250("high"))
	want := "{\n" +
		"    \"permissions\"  :  { \"allow\": [\"Bash(ls:*)\"] },\n" +
		"    \"env\": { \"model\": \"sonnet\", \"effortLevel\": \"low\" },\n" + // imbriqué : ignoré
		"    \"model\"   :   \"opus\"  ,\n" +
		"\t\"hooks\": {\"SessionStart\": []},\n" +
		"    \"effortLevel\":\"high\"\n" +
		"}" // pas de fin de ligne finale : conservée absente
	if got := c250Read(t, st250Path(proj)); got != want {
		t.Errorf("réécriture textuelle minimale :\n%q\nattendu :\n%q", got, want)
	}
}

// Clés absentes : insérées en tête de l'objet racine avec l'indentation détectée ; le reste intact.
func TestSettingsClesAbsentesInsereesIndentationDetectee250(t *testing.T) {
	for _, tc := range []struct{ name, indent string }{{"2 espaces", "  "}, {"4 espaces", "    "}, {"tabulation", "\t"}} {
		proj := c250Proj(t)
		rest := []string{
			tc.indent + "\"permissions\": {\"allow\": []},",
			tc.indent + "\"env\": {\"model\": \"x\"}",
		}
		orig := "{\n" + strings.Join(rest, "\n") + "\n}\n"
		c250Write(t, st250Path(proj), orig)
		c250MustSet(t, proj, "teamleader", s250("opus"), s250("max"))
		got := c250Read(t, st250Path(proj))
		m := st250Parse(t, got)
		if m["model"] != "opus" || m["effortLevel"] != "max" {
			t.Errorf("%s : model=%v effortLevel=%v", tc.name, m["model"], m["effortLevel"])
		}
		if env, _ := m["env"].(map[string]any); env["model"] != "x" {
			t.Errorf("%s : clé imbriquée env.model modifiée : %v", tc.name, env)
		}
		// lignes insérées = celles des deux clés, avec l'indentation détectée ; retirées, on retrouve l'original
		var kept, inserted []string
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "\"model\": \"opus\"") || strings.HasPrefix(strings.TrimSpace(l), "\"effortLevel\": \"max\"") {
				inserted = append(inserted, l)
				continue
			}
			kept = append(kept, l)
		}
		if len(inserted) != 2 {
			t.Errorf("%s : %d lignes insérées, attendu 2 :\n%s", tc.name, len(inserted), got)
		}
		for _, l := range inserted {
			if l[:len(l)-len(strings.TrimLeft(l, " \t"))] != tc.indent {
				t.Errorf("%s : indentation de %q, attendu %q", tc.name, l, tc.indent)
			}
		}
		if strings.Join(kept, "\n") != orig {
			t.Errorf("%s : le reste n'est pas identique :\n%q\nattendu :\n%q", tc.name, strings.Join(kept, "\n"), orig)
		}
		if !strings.HasPrefix(got, "{\n"+inserted[0]) {
			t.Errorf("%s : clés non insérées en tête de l'objet racine :\n%s", tc.name, got)
		}
	}
}

// Clés imbriquées seulement : ignorées, les clés de premier niveau sont insérées.
func TestSettingsClesImbriqueesIgnorees250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, st250Path(proj), "{\n  \"env\": {\"model\": \"haiku\", \"effortLevel\": \"low\"}\n}\n")
	c250MustSet(t, proj, "teamleader", s250("opus"), nil)
	m := st250Parse(t, c250Read(t, st250Path(proj)))
	env, _ := m["env"].(map[string]any)
	if env["model"] != "haiku" || env["effortLevel"] != "low" {
		t.Errorf("clés imbriquées modifiées : %v", env)
	}
	if m["model"] != "opus" || m["effortLevel"] != "medium" {
		t.Errorf("premier niveau : model=%v effortLevel=%v, attendu opus/medium", m["model"], m["effortLevel"])
	}
}

// JSON invalide, racine non objet, clé en double : rien n'est écrit (ni compagnon, ni settings.json).
func TestSettingsInvalideRienEcrit250(t *testing.T) {
	cases := []struct{ name, content string }{
		{"JSON illisible", "{ \"model\": \"sonnet\", "},
		{"racine tableau", "[1, 2]"},
		{"racine chaîne", "\"model\""},
		{"clé model en double", "{\"model\": \"a\", \"model\": \"b\"}"},
		{"clé effortLevel en double", "{\"effortLevel\": \"low\", \"x\": 1, \"effortLevel\": \"max\"}"},
	}
	for _, c := range cases {
		proj := c250Proj(t)
		c250Write(t, st250Path(proj), c.content)
		before := snapshot250(t, proj)
		_, err := c250Set(t, proj, "teamleader", s250("opus"), s250("high"))
		if err == nil {
			t.Errorf("%s : refus attendu", c.name)
		} else if !strings.Contains(err.Error(), "settings.json illisible") {
			t.Errorf("%s : message %q, attendu « settings.json illisible : <raison> »", c.name, err)
		}
		if d := diff250(before, snapshot250(t, proj)); d != "" {
			t.Errorf("%s : fichiers modifiés malgré le refus : %s", c.name, d)
		}
		if c250Exists(c250Companion(proj, "teamleader")) {
			t.Errorf("%s : teamleader.md créé malgré settings.json invalide", c.name)
		}
	}
}

// « default » / retour au template : settings.json reçoit les valeurs du template (clé non retirée) ; teamleader.md perd la clé.
func TestSettingsDefautEcritLesValeursDuTemplate250(t *testing.T) {
	proj := c250Proj(t) // sonnet / medium
	c250Write(t, c250Companion(proj, "teamleader"), "---\nmodel: opus\neffort: max\n---\ncorps\n")
	c250Write(t, st250Path(proj), "{\n  \"model\": \"opus\",\n  \"effortLevel\": \"max\",\n  \"x\": 1\n}\n")
	c250MustSet(t, proj, "teamleader", s250("default"), s250("default"))
	if got := c250Read(t, st250Path(proj)); got != "{\n  \"model\": \"sonnet\",\n  \"effortLevel\": \"medium\",\n  \"x\": 1\n}\n" {
		t.Errorf("settings.json : %q, attendu les valeurs du template (clés conservées)", got)
	}
	if got := c250Read(t, c250Companion(proj, "teamleader")); got != "corps\n" {
		t.Errorf("teamleader.md : %q, attendu clés retirées", got)
	}
	// valeur égale au template : même résultat
	c250MustSet(t, proj, "teamleader", s250("opus"), s250("max"))
	c250MustSet(t, proj, "teamleader", s250("sonnet"), s250("medium"))
	m := st250Parse(t, c250Read(t, st250Path(proj)))
	if m["model"] != "sonnet" || m["effortLevel"] != "medium" {
		t.Errorf("retour par valeur du template : %v", m)
	}
}

// Valeurs résolues : seule une clé change côté compagnon, settings.json reçoit les DEUX valeurs résolues.
func TestSettingsValeursResoluesDesDeuxCles250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, c250Companion(proj, "teamleader"), "---\neffort: high\n---\n")
	c250MustSet(t, proj, "teamleader", s250("haiku"), nil)
	m := st250Parse(t, c250Read(t, st250Path(proj)))
	if m["model"] != "haiku" || m["effortLevel"] != "high" {
		t.Errorf("settings.json : %v, attendu model haiku (changé) et effortLevel high (résolu du compagnon)", m)
	}
}

// Autres agents : settings.json n'est jamais touché.
func TestSettingsPasEcritPourUnAutreAgent250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, st250Path(proj), "{\"model\": \"sonnet\"}\n")
	before := c250Read(t, st250Path(proj))
	c250MustSet(t, proj, "dev-backend", s250("opus"), s250("high"))
	c250MustSet(t, proj, "redacteur", s250("opus"), nil)
	if c250Read(t, st250Path(proj)) != before {
		t.Error("settings.json modifié par le réglage d'un teammate (seul le teamleader l'écrit)")
	}
}

// settings.local.json et le settings.json de CLAUDE_CONFIG_DIR : jamais touchés.
func TestSettingsLocalEtUtilisateurIntacts250(t *testing.T) {
	proj := c250Proj(t)
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	local := filepath.Join(proj, ".claude", "settings.local.json")
	c250Write(t, local, "{\"model\": \"haiku\", \"effortLevel\": \"low\"}\n")
	user := filepath.Join(cfgDir, "settings.json")
	c250Write(t, user, "{\"model\": \"haiku\"}\n")
	bl, bu := c250Read(t, local), c250Read(t, user)
	c250MustSet(t, proj, "teamleader", s250("opus"), s250("max"))
	c250MustSet(t, proj, "teamleader", s250("default"), s250("default"))
	if c250Read(t, local) != bl {
		t.Error("settings.local.json modifié")
	}
	if c250Read(t, user) != bu {
		t.Error("settings.json de CLAUDE_CONFIG_DIR modifié")
	}
}

// Échec du second fichier (settings.json non écrivable) : le compagnon, source durable, est écrit ; partial = « companion ».
func TestSettingsEchecDuSecondFichierPartial250(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("répertoire en lecture seule non effectif (Windows ou root)")
	}
	proj := c250Proj(t)
	orig := "{\n  \"model\": \"sonnet\",\n  \"effortLevel\": \"medium\"\n}\n"
	c250Write(t, st250Path(proj), orig)
	dir := filepath.Join(proj, ".claude")
	// .claude en lecture seule : l'écriture atomique de settings.json (fichier temporaire à côté) échoue ;
	// .claude/agents reste écrivable : le compagnon est écrit en premier.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip(err.Error())
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	res, err := c250Set(t, proj, "teamleader", s250("opus"), nil)
	_ = os.Chmod(dir, 0o755)
	if err == nil && res["partial"] != "companion" {
		t.Errorf("échec de settings.json non signalé : err=%v partial=%v", err, res["partial"])
	}
	if res["partial"] != nil && res["partial"] != "companion" {
		t.Errorf("partial : %v, attendu « companion »", res["partial"])
	}
	if got := c250Read(t, c250Companion(proj, "teamleader")); got != "---\nmodel: opus\n---\n" {
		t.Errorf("le compagnon doit être écrit malgré l'échec du second fichier : %q", got)
	}
	if got := c250Read(t, st250Path(proj)); got != orig {
		t.Errorf("settings.json modifié malgré l'échec : %q", got)
	}
}

// Mode de settings.json conservé ; aucun temporaire laissé dans .claude/.
func TestSettingsModeEtTemporaires250(t *testing.T) {
	proj := c250Proj(t)
	p := st250Path(proj)
	c250Write(t, p, "{\n  \"x\": 1\n}\n")
	if runtime.GOOS != "windows" {
		_ = os.Chmod(p, 0o600)
	}
	c250MustSet(t, proj, "teamleader", s250("opus"), nil)
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, attendu 0600 conservé", st.Mode().Perm())
		}
	}
	ents, _ := os.ReadDir(filepath.Join(proj, ".claude"))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".lock") {
			t.Errorf("temporaire ou verrou laissé : %s", e.Name())
		}
	}
}
