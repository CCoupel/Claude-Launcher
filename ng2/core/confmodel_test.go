package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func settingOf(m ConfModel, key string) ConfSettingView {
	for _, s := range m.Settings {
		if s.Key == key {
			return s
		}
	}
	return ConfSettingView{}
}

func confApply(t *testing.T, src string, ch ...ConfigChange) string {
	t.Helper()
	out, err := ApplyConfConfChanges(src, ch)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func cset(k string, v any) ConfigChange { return ConfigChange{Op: "set", At: []string{k}, Value: v} }

func TestReadConfModelDefaults(t *testing.T) {
	m := ReadConfModel(defaultConfig)
	if len(m.Unrecognized) != 0 || len(m.Groups) == 0 {
		t.Fatalf("modèle par défaut : %+v", m.Unrecognized)
	}
	for k, want := range map[string]any{"GITHUB_DIR": "$HOME/GITHUB", "CLAUDE_DISABLE_MOUSE": true, "CLAUDE_EXPERIMENTAL_TEAMS": true,
		"CLAUDE_OPTIONS": "--allow-dangerously-skip-permissions", "TEAM_HIDE_IDLE": 30, "RESUME_MODE": "ask", "USAGE_REFRESH_SEC": 120} {
		if got := settingOf(m, k).Value; !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v (%T), attendu %v", k, got, got, want)
		}
	}
	if s := settingOf(m, "CLAUDE_OPTIONS"); !s.Set || !s.Editable {
		t.Errorf("CLAUDE_OPTIONS : %+v", s)
	}
	if s := settingOf(m, "TEAM_HIDE_IDLE"); s.Set { // seulement en commentaire dans le modèle
		t.Errorf("TEAM_HIDE_IDLE vu comme écrit : %+v", s)
	}
	if got := settingOf(m, "COLOR_PALETTE").Value; !reflect.DeepEqual(got, []string{"17", "52", "22", "53", "58", "23", "54", "238", "18", "59"}) {
		t.Errorf("palette par défaut : %v", got)
	}
	for _, s := range m.Settings {
		if s.Label == "" || s.Group == "" || s.Type == "" {
			t.Errorf("réglage incomplet : %+v", s)
		}
	}
}

// Le jeton n'est JAMAIS dans le modèle ni dans sa forme JSON ; seul « set ».
func TestConfTokenNeverReturned(t *testing.T) {
	src := "GITHUB_TOKEN=\"ghp_SECRETSECRET123\"\nGITHUB_DIR=x\n"
	m := ReadConfModel(src)
	s := settingOf(m, "GITHUB_TOKEN")
	if !s.Set || s.Value != nil || s.Default != nil {
		t.Fatalf("jeton : %+v", s)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "ghp_") || strings.Contains(string(b), "SECRET") {
		t.Fatalf("jeton dans le modèle : %s", b)
	}
	if ConfTokenValue(src) != "ghp_SECRETSECRET123" {
		t.Fatal("ConfTokenValue")
	}
	if s := settingOf(ReadConfModel("GITHUB_TOKEN=\"\"\n"), "GITHUB_TOKEN"); s.Set {
		t.Errorf("jeton vide vu comme écrit : %+v", s)
	}
	// remplacement : valeur contrôlée ; retrait
	out := confApply(t, src, cset("GITHUB_TOKEN", "ghp_NEW1234567"))
	if !strings.Contains(out, `GITHUB_TOKEN="ghp_NEW1234567"`) || strings.Contains(out, "SECRETSECRET") || !strings.Contains(out, "GITHUB_DIR=x") {
		t.Fatalf("remplacement :\n%s", out)
	}
	if out := confApply(t, src, ConfigChange{Op: "delete", At: []string{"GITHUB_TOKEN"}}); strings.Contains(out, "GITHUB_TOKEN") || !strings.Contains(out, "GITHUB_DIR=x") {
		t.Fatalf("retrait :\n%s", out)
	}
	for _, bad := range []any{"", "a b", "a\"b", "$(id)", 5, "x;y"} {
		if _, err := ApplyConfConfChanges(src, []ConfigChange{cset("GITHUB_TOKEN", bad)}); err == nil {
			t.Errorf("jeton %v accepté", bad)
		}
	}
}

// critical : seule la ligne de la clé change ; commentaires et autres lignes
// conservés ; relecture bash des valeurs vérifiée.
func TestApplyConfKeepsCommentsAndReadsBack(t *testing.T) {
	unixOnly(t)
	out := confApply(t, defaultConfig,
		cset("CLAUDE_DISABLE_MOUSE", false), cset("CLAUDE_OPTIONS", "--foo --bar"),
		cset("TEAM_HIDE_IDLE", 45), cset("RESUME_MODE", "never"),
		cset("EXTRA_ENVS", []any{"A=1", "B=deux mots"}), cset("COLOR_PALETTE", []any{"17", "52"}),
		cset("GITHUB_DIRS", []any{"$HOME/GITHUB", "/data/projets"}), cset("CTR_STOP_ON_EXIT", true),
		cset("USAGE_REFRESH_SEC", 90.0))
	for _, c := range []string{"# claude-launcher — configuration utilisateur", "# Plusieurs dossiers racine", "# Token GitHub (gh CLI + Claude Code MCP)",
		"# 1 = désactive la souris dans le terminal", "# 1 = active les agent teams"} {
		if !strings.Contains(out, c) {
			t.Errorf("commentaire perdu : %q", c)
		}
	}
	if !strings.Contains(out, "CLAUDE_DISABLE_MOUSE=0        # 1 = désactive la souris") {
		t.Errorf("commentaire de fin de ligne perdu ou valeur non remplacée :\n%s", out)
	}
	// nouvelle clé : sous son exemple commenté
	if i, j := strings.Index(out, "# TEAM_HIDE_IDLE=30"), strings.Index(out, "TEAM_HIDE_IDLE=45"); j < i || j-i > 60 {
		t.Errorf("TEAM_HIDE_IDLE=45 pas sous son exemple (%d, %d) :\n%s", i, j, out)
	}
	p := filepath.Join(t.TempDir(), "config.conf")
	if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if HasErrors(ValidateConfig(ConfConf, []byte(out))) {
		t.Fatalf("invalide : %v\n%s", ValidateConfig(ConfConf, []byte(out)), out)
	}
	c := loadConfigAt(p)
	if c.DisableMouse || c.ClaudeOptions != "--foo --bar" || c.TeamHideIdle != 45 || c.ResumeMode != "never" || !c.CtrStopOnExit ||
		c.UsageRefreshSec != 90 || !reflect.DeepEqual(c.ExtraEnvs, []string{"A=1", "B=deux mots"}) || !reflect.DeepEqual(c.Palette, []string{"17", "52"}) {
		t.Errorf("relecture bash : %+v", c)
	}
	m := ReadConfModel(out)
	if !reflect.DeepEqual(settingOf(m, "EXTRA_ENVS").Value, []string{"A=1", "B=deux mots"}) || settingOf(m, "CLAUDE_DISABLE_MOUSE").Value != false {
		t.Errorf("modèle relu : %+v", m.Settings)
	}
	// retour au défaut : l'affectation disparaît, le commentaire reste
	back := confApply(t, out, ConfigChange{Op: "delete", At: []string{"CLAUDE_DISABLE_MOUSE"}}, ConfigChange{Op: "delete", At: []string{"TEAM_HIDE_IDLE"}})
	if strings.Contains(back, "CLAUDE_DISABLE_MOUSE=") && !strings.Contains(back, "# ") || strings.Contains(back, "\nTEAM_HIDE_IDLE=45") {
		t.Errorf("retrait :\n%s", back)
	}
	if s := settingOf(ReadConfModel(back), "TEAM_HIDE_IDLE"); s.Set || s.Value != 30 {
		t.Errorf("défaut après retrait : %+v", s)
	}
}

func TestApplyConfFormsAndMultiStatement(t *testing.T) {
	// plusieurs affectations sur une ligne : seule la clé visée change
	src := "LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=2; LAYOUT_3ROW_DEN=9\nA=1\n"
	out := confApply(t, src, cset("LAYOUT_3ROW_MID_NUM", 4))
	if out != "LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=4; LAYOUT_3ROW_DEN=9\nA=1\n" {
		t.Errorf("ligne multiple : %q", out)
	}
	out = confApply(t, src, ConfigChange{Op: "delete", At: []string{"LAYOUT_3ROW_MID_NUM"}})
	if strings.Contains(out, "MID_NUM") || !strings.Contains(out, "LAYOUT_3ROW_LEADER_NUM=3") || !strings.Contains(out, "DEN=9") {
		t.Errorf("retrait sur ligne multiple : %q", out)
	}
	// tableau multi-lignes réécrit en multi-lignes, simple en une ligne
	out = confApply(t, "EXTRA_ENVS=(\n  \"A=1\"\n)\nGITHUB_DIRS=(\"/a\")\n", cset("EXTRA_ENVS", []any{"A=1", "B=2"}), cset("GITHUB_DIRS", []any{"/a", "/b"}))
	if out != "EXTRA_ENVS=(\n  \"A=1\"\n  \"B=2\"\n)\nGITHUB_DIRS=(\"/a\" \"/b\")\n" {
		t.Errorf("tableaux : %q", out)
	}
	// valeur scalaire pour une liste (GITHUB_DIRS=/a) : lue comme un élément
	if got := settingOf(ReadConfModel("GITHUB_DIRS=/a\n"), "GITHUB_DIRS").Value; !reflect.DeepEqual(got, []string{"/a"}) {
		t.Errorf("liste scalaire : %v", got)
	}
	// fichier sans fin de ligne ; clé absente et sans exemple : en fin de fichier
	if out := confApply(t, "A=1", cset("TEAM_HIDE_IDLE", 5)); out != "A=1\nTEAM_HIDE_IDLE=5\n" {
		t.Errorf("ajout en fin : %q", out)
	}
	if out := confApply(t, "", cset("TEAM_HIDE_IDLE", 5)); out != "TEAM_HIDE_IDLE=5\n" {
		t.Errorf("fichier vide : %q", out)
	}
	// formes non reconnues : champ en lecture seule, écriture refusée, fichier intact
	for name, src := range map[string]string{
		"boucle":   "for k in a; do TEAM_HIDE_IDLE=5; done\n",
		"eval":     "eval \"RESUME_MODE=never\"\n",
		"+=":       "EXTRA_ENVS=(\"A=1\")\nEXTRA_ENVS+=(\"B=2\")\n",
		"composée": "CLAUDE_OPTIONS=\"--x $(date)\"\n",
		"accolées": "CLAUDE_OPTIONS=\"--x\"suite\n",
		"commande": "CLAUDE_OPTIONS=$(cat /etc/x)\n",
	} {
		key := map[string]string{"boucle": "TEAM_HIDE_IDLE", "eval": "RESUME_MODE", "+=": "EXTRA_ENVS"}[name]
		if key == "" {
			key = "CLAUDE_OPTIONS"
		}
		m := ReadConfModel(src)
		if s := settingOf(m, key); s.Editable || s.Why == "" || !reflect.DeepEqual(m.Unrecognized, []string{key}) {
			t.Errorf("%s : champ modifiable : %+v %v", name, s, m.Unrecognized)
		}
		var v any = "x"
		if key == "TEAM_HIDE_IDLE" {
			v = 7
		} else if key == "RESUME_MODE" {
			v = "ask"
		} else if key == "EXTRA_ENVS" {
			v = []any{"C=3"}
		}
		if _, err := ApplyConfConfChanges(src, []ConfigChange{cset(key, v)}); err == nil || !strings.Contains(err.Error(), "Éditer en YAML") {
			t.Errorf("%s : écriture acceptée : %v", name, err)
		}
	}
}

// Une valeur ne doit jamais pouvoir ajouter une commande à ce fichier bash.
func TestApplyConfRefusesUnsafeAndInvalidValues(t *testing.T) {
	for name, ch := range map[string]ConfigChange{
		"substitution":      cset("CLAUDE_OPTIONS", "$(rm -rf ~)"),
		"guillemet inverse": cset("CLAUDE_OPTIONS", "`id`"),
		"guillemet":         cset("CLAUDE_OPTIONS", `a" ; rm x ; "`),
		"antislash":         cset("GITHUB_DIR", `C:\x`),
		"retour ligne":      cset("CLAUDE_OPTIONS", "a\nb=1"),
		"${...}":            cset("CLAUDE_OPTIONS", "${x:-$(id)}"),
		"${!x}":             cset("CLAUDE_OPTIONS", "${!x}"),
		"$ seul":            cset("CLAUDE_OPTIONS", "5$"),
		"élément de liste":  cset("GITHUB_DIRS", []any{"/ok", "$(id)"}),
		"env sans =":        cset("EXTRA_ENVS", []any{"pas de forme"}),
		"env nom invalide":  cset("EXTRA_ENVS", []any{"1A=2"}),
		"entier":            cset("TEAM_HIDE_IDLE", "abc"),
		"négatif":           cset("TEAM_HIDE_IDLE", -1),
		"borne haute":       cset("PANE_ACTIVE_BRIGHTEN_STEP", 6),
		"borne basse":       cset("USAGE_REFRESH_SEC", 10),
		"pourcentage":       cset("LAYOUT_2ROW_LEADER_PCT", 100),
		"enum":              cset("RESUME_MODE", "peut-être"),
		"booléen":           cset("CLAUDE_DISABLE_MOUSE", "oui"),
		"palette":           cset("COLOR_PALETTE", []any{"17", "300"}),
		"type liste":        cset("GITHUB_DIRS", "pas une liste"),
		"clé inconnue":      cset("PATH", "/x"),
		"op inconnue":       {Op: "boum", At: []string{"TEAM_HIDE_IDLE"}},
		"chemin":            {Op: "set", At: []string{"A", "B"}, Value: 1},
	} {
		if out, err := ApplyConfConfChanges("A=1\n", []ConfigChange{ch}); err == nil || out != "" {
			t.Errorf("%s : accepté (%q)", name, out)
		}
	}
	// permis : $NOM et ${NOM}
	if _, err := ApplyConfConfChanges("", []ConfigChange{cset("GITHUB_DIR", "$HOME/GITHUB"), cset("CLAUDE_CONFIG_DIR", "${HOME}/.claude-x")}); err != nil {
		t.Errorf("variables simples refusées : %v", err)
	}
	// mise en page : le dénominateur doit dépasser la somme des numérateurs
	if _, err := ApplyConfConfChanges("", []ConfigChange{cset("LAYOUT_3ROW_DEN", 5)}); err == nil {
		t.Error("3 + 2 ≥ 5 accepté")
	}
	if _, err := ApplyConfConfChanges("", []ConfigChange{cset("LAYOUT_3ROW_DEN", 6)}); err != nil {
		t.Errorf("3 + 2 < 6 refusé : %v", err)
	}
}

// config.conf est écrit en 0600 (jeton), copie .bak comprise, même si le fichier
// existant était plus ouvert.
func TestSaveConfigConfIs0600(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", dir)
	path := ConfigPath()
	if err := os.WriteFile(path, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if _, err := SaveConfig(ConfConf, path, []byte("A=2\nGITHUB_TOKEN=\"ghp_x\"\n"), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".bak"} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s : %v %v", p, st, err)
		}
	}
}

func TestReadHubModelAndApply(t *testing.T) {
	src := "# postes\nhubs:\n  mon-pc:\n    paths:\n      - { local: 'C:\\\\p', remote: /share/p }   # partage\n    browser_server: { listen: '127.0.0.1:8765' }\n    hub_cache: /x\n    windows:\n      hub_cache: '\\\\\\\\wsl.localhost\\\\U\\\\x'\n      remote_user: { uid: 1000, gid: 1000, name: u, home: /home/u }\n"
	m, err := ReadHubModel([]byte(src), "MON-PC.domaine", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if m.Hub != "mon-pc" || !m.Present || m.Goos != "windows" || len(m.Settings.Paths) != 1 || m.Settings.Paths[0].Remote != "/share/p" ||
		m.Settings.BrowserServer.Listen != "127.0.0.1:8765" || m.Settings.HubCache != "/x" {
		t.Fatalf("modèle : %+v", m)
	}
	ov := m.Overrides["windows"]
	if !reflect.DeepEqual(ov.Keys, []string{"remote_user", "hub_cache"}) && !reflect.DeepEqual(ov.Keys, []string{"hub_cache", "remote_user"}) {
		t.Errorf("surcharges : %+v", ov)
	}
	if m.Effective["hub_cache"] != "windows" || m.Effective["paths"] != "" || ov.Settings.RemoteUser.UID == nil || *ov.Settings.RemoteUser.UID != 1000 {
		t.Errorf("effectif : %+v %+v", m.Effective, ov.Settings.RemoteUser)
	}
	lm, _ := ReadHubModel([]byte(src), "mon-pc", "linux") // la surcharge windows ne vaut pas sous Linux
	for k, v := range lm.Effective {
		if v != "" {
			t.Errorf("effectif sous Linux : %s = %q", k, v)
		}
	}
	if len(lm.Effective) != len(hubOSKeys) || m.Effective["paths"] != "" {
		t.Errorf("toutes les clés sont renseignées : %+v", lm.Effective)
	}
	// absente : créée au premier changement, sous le nom de la machine
	em, _ := ReadHubModel([]byte("backends: {}\n"), "AUTRE", "linux")
	if em.Present || em.Hub != "AUTRE" || em.Settings.Paths == nil {
		t.Errorf("section absente : %+v", em)
	}
	// changements relatifs : commentaires conservés, clé existante retrouvée sans tenir compte de la casse
	out, err := ApplyHubChanges([]byte(src), "MON-PC.domaine", []ConfigChange{
		{Op: "set", At: []string{"hub_cache"}, Value: "/y"},
		{Op: "set", At: []string{"windows", "hub_cache"}, Value: "Z:\\\\c"},
		{Op: "set", At: []string{"paths"}, Value: []any{map[string]any{"local": "D:\\\\q", "remote": "/share/q"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# postes") || !strings.Contains(s, "hub_cache: /y") || strings.Contains(s, "mon-pc.domaine") || !strings.Contains(s, "/share/q") {
		t.Fatalf("hubs :\n%s", s)
	}
	if HasErrors(ValidateConfig(ConfBackends, out)) {
		t.Errorf("invalide : %v", ValidateConfig(ConfBackends, out))
	}
	out, err = ApplyHubChanges([]byte("backends: {}\n"), "AUTRE", []ConfigChange{{Op: "set", At: []string{"hub_cache"}, Value: "/z"}})
	if err != nil || !strings.Contains(string(out), "AUTRE:") || !strings.Contains(string(out), "hub_cache: /z") {
		t.Errorf("création : %v\n%s", err, out)
	}
	for _, bad := range []ConfigChange{{Op: "set", At: []string{"backends", "x"}, Value: 1}, {Op: "set", At: nil}} {
		if _, err := ApplyHubChanges([]byte(src), "mon-pc", []ConfigChange{bad}); err == nil {
			t.Errorf("accepté : %+v", bad)
		}
	}
}

func TestLauncherEnvModelAndApply(t *testing.T) {
	src := "# outillage\npackages: [ripgrep]   # recherche\nsetup:\n  - echo a\n"
	m, err := ReadLauncherEnvModel([]byte(src))
	if err != nil || !reflect.DeepEqual(m.Packages, []string{"ripgrep"}) || len(m.OnStart) != 0 || m.PackagesByManager == nil {
		t.Fatalf("modèle : %+v %v", m, err)
	}
	out, err := ApplyConfigChanges(ConfLauncherEnv, []byte(src), []ConfigChange{
		{Op: "set", At: []string{"packages"}, Value: []any{"ripgrep", "jq"}},
		{Op: "set", At: []string{"on_start"}, Value: []any{"echo debut"}}})
	if err != nil || !strings.Contains(string(out), "# outillage") || !strings.Contains(string(out), "# recherche") || !strings.Contains(string(out), "jq") {
		t.Fatalf("écriture : %v\n%s", err, out)
	}
	if HasErrors(ValidateConfig(ConfLauncherEnv, out)) {
		t.Errorf("invalide : %v", ValidateConfig(ConfLauncherEnv, out))
	}
	// clé réservée aux projets : refusée par la validation existante
	bad, _ := ApplyConfigChanges(ConfLauncherEnv, []byte(src), []ConfigChange{{Op: "set", At: []string{"image"}, Value: "x"}})
	if !HasErrors(ValidateConfig(ConfLauncherEnv, bad)) {
		t.Error("« image » acceptée dans l'env.yaml du launcher")
	}
}
