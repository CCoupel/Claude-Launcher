package core

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
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

// Sécurité (#149) : EXTRA_ENVS refuse les variables qui changent ce qui
// s'exécute ; une valeur ne recopie jamais un secret ($GITHUB_TOKEN).
func TestSecurityEnvAndVariables(t *testing.T) {
	for _, name := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "ld_preload", "DYLD_INSERT_LIBRARIES", "BASH_ENV", "ENV", "PATH", "PROMPT_COMMAND",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_EXEC_PATH", "IFS", "PS4", "SHELLOPTS", "BASHOPTS", "NODE_OPTIONS",
		// R3 : configuration de git, askpass, proxys, éditeur, shell, répertoire personnel, fonctions exportées, clés Anthropic
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_ASKPASS", "SSH_ASKPASS",
		"GIT_PROXY_COMMAND", "GIT_EXTERNAL_DIFF", "GIT_PAGER", "PAGER", "MANPAGER", "EDITOR", "VISUAL", "GIT_EDITOR", "BROWSER", "SHELL", "HOME",
		"PYTHONHOME", "PERL5LIB", "RUBYLIB", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "LUA_INIT", "BASH_FUNC_foo", "NODE_EXTRA_CA_CERTS",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "anthropic_auth_token", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "NO_PROXY"} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{cset("EXTRA_ENVS", []any{name + "=x"})}); err == nil || !strings.Contains(err.Error(), "interdite") {
			t.Errorf("%s accepté : %v", name, err)
		}
	}
	if _, err := ApplyConfConfChanges("", []ConfigChange{cset("EXTRA_ENVS", []any{"MY_API_URL=https://x", "MON_SERVICE_URL=https://y", "TZ=Europe/Paris", "LANG=fr_FR.UTF-8"})}); err != nil {
		t.Errorf("variables ordinaires refusées : %v", err)
	}
	for name, ch := range map[string]ConfigChange{
		"jeton dans une option":    cset("CLAUDE_OPTIONS", "$GITHUB_TOKEN"),
		"jeton dans une variable":  cset("EXTRA_ENVS", []any{"X=${GITHUB_TOKEN}"}),
		"GH_TOKEN":                 cset("GITHUB_DIR", "$GH_TOKEN"),
		"variable quelconque":      cset("GITHUB_DIR", "$SECRET_THING/x"),
		"jeton recopié dans liste": cset("GITHUB_DIRS", []any{"/a", "$GITHUB_TOKEN"}),
	} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{ch}); err == nil || !strings.Contains(err.Error(), "non autorisée") {
			t.Errorf("%s : accepté : %v", name, err)
		}
	}
	for _, v := range []string{"$HOME/GITHUB", "${HOME}/x", "$XDG_CONFIG_HOME/c", "$GITHUB_DIR/p", "$USER"} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{cset("GITHUB_DIR", v)}); err != nil {
			t.Errorf("%s refusé : %v", v, err)
		}
	}
}

// Éditeur de texte : le jeton est remplacé par un marqueur ; le laisser tel
// quel conserve le jeton actuel, le remplacer ou le vider fonctionne.
func TestConfTokenKeepMarker(t *testing.T) {
	cur := "# c\nGITHUB_TOKEN=\"ghp_REALTOKEN1234\"   # mon jeton\nTEAM_HIDE_IDLE=45\n"
	masked := MaskConfToken(cur)
	if strings.Contains(masked, "REALTOKEN") || !strings.Contains(masked, `GITHUB_TOKEN="__GARDER__"   # mon jeton`) || !strings.Contains(masked, "TEAM_HIDE_IDLE=45") {
		t.Fatalf("masquage : %q", masked)
	}
	// non touché : restauré à l'identique
	rest := func(edited, current string) string {
		t.Helper()
		got, err := RestoreConfToken(edited, current)
		if err != nil {
			t.Fatalf("RestoreConfToken : %v", err)
		}
		return got
	}
	if got := rest(masked, cur); got != cur {
		t.Errorf("restauration : %q", got)
	}
	// modifié ailleurs : le jeton reste
	if got := rest(strings.Replace(masked, "=45", "=60", 1), cur); !strings.Contains(got, "ghp_REALTOKEN1234") || !strings.Contains(got, "TEAM_HIDE_IDLE=60") {
		t.Errorf("autre réglage : %q", got)
	}
	// remplacé, vidé : respecté
	if got := rest(strings.Replace(masked, "__GARDER__", "ghp_NEWNEWNEW99", 1), cur); !strings.Contains(got, "ghp_NEWNEWNEW99") || strings.Contains(got, "REALTOKEN") {
		t.Errorf("remplacement : %q", got)
	}
	if got := rest(strings.Replace(masked, `"__GARDER__"`, `""`, 1), cur); strings.Contains(got, "REALTOKEN") || strings.Contains(got, "__GARDER__") {
		t.Errorf("vidage : %q", got)
	}
	// marqueur sans jeton actuel : jamais enregistré comme jeton
	if got := rest("GITHUB_TOKEN=\"__GARDER__\"\n", "A=1\n"); strings.Contains(got, "__GARDER__") || !strings.Contains(got, `GITHUB_TOKEN=""`) {
		t.Errorf("sans jeton actuel : %q", got)
	}
	// sans jeton, forme composée ou déjà masqué : inchangé
	for _, s := range []string{"A=1\n", "GITHUB_TOKEN=\"\"\n", "GITHUB_TOKEN=\"$(pass gh)\"\n", "GITHUB_TOKEN=\"__GARDER__\"\n"} {
		if MaskConfToken(s) != s {
			t.Errorf("%q modifié", s)
		}
	}
	// jeton nu
	if got := MaskConfToken("GITHUB_TOKEN=ghp_bare12345\n"); strings.Contains(got, "bare") || !strings.Contains(got, "__GARDER__") {
		t.Errorf("jeton nu : %q", got)
	}
}

func TestMaskSecret(t *testing.T) {
	tok := "ghp_0123456789"
	if got := MaskSecret("ligne 3 : "+tok+" inattendu", tok); strings.Contains(got, tok) || !strings.Contains(got, "***") {
		t.Errorf("long : %q", got)
	}
	// jeton court : pas de remplacement (il dégraderait n'importe quel message) — message générique
	if got := MaskSecret("erreur à la ligne 1 près de a", "a"); strings.Contains(got, "ligne") || got == "" {
		t.Errorf("court : %q", got)
	}
	if got := MaskSecret("rien", "abc"); got != "rien" { // il n'y figure pas : intact
		t.Errorf("absent : %q", got)
	}
	if got := MaskSecret("x", ""); got != "x" {
		t.Errorf("sans jeton : %q", got)
	}
}

// .bak préexistant (plus ouvert, ou lien) : resserré en 0600, jamais écrit à travers un lien.
func TestSaveConfigBakMode(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", dir)
	path := ConfigPath()
	_ = os.WriteFile(path, []byte("A=1\n"), 0o600)
	_ = os.WriteFile(path+".bak", []byte("vieux\n"), 0o644)
	_ = os.Chmod(path+".bak", 0o644)
	st, _ := os.Stat(path)
	keep := filepath.Join(dir, "autre-nom-du-bak") // lien dur vers l'ancienne copie : écrite sur place, il changerait
	if err := os.Link(path+".bak", keep); err != nil {
		t.Skip("liens durs indisponibles")
	}
	if _, err := SaveConfig(ConfConf, path, []byte("A=2\n"), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if bst, _ := os.Stat(path + ".bak"); bst.Mode().Perm() != 0o600 {
		t.Errorf(".bak préexistant : %v", bst.Mode().Perm())
	}
	if b, _ := os.ReadFile(keep); string(b) != "vieux\n" {
		t.Errorf(".bak réécrit sur place (large) puis resserré : il doit être recréé en 0600 d'emblée (%q)", b)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != "A=1\n" {
		t.Errorf(".bak : %q", b)
	}
	// .bak qui est un lien : remplacé, la cible n'est pas écrite
	target := filepath.Join(dir, "cible")
	_ = os.WriteFile(target, []byte("intact"), 0o644)
	_ = os.Remove(path + ".bak")
	if err := os.Symlink(target, path+".bak"); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	st, _ = os.Stat(path)
	if _, err := SaveConfig(ConfConf, path, []byte("A=3\n"), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "intact" {
		t.Errorf("cible du lien écrasée : %q", b)
	}
	if lst, _ := os.Lstat(path + ".bak"); lst.Mode()&os.ModeSymlink != 0 {
		t.Error(".bak toujours un lien")
	}
}

// Backends k8s : noms invalides (injection d'option ou syntaxe hors DNS-1123) refusés
// avant tout appel ; « ssh://-oProxyCommand=… » aussi.
func TestBackendNamesDNS1123AndSSHAuthority(t *testing.T) {
	for _, y := range []string{
		"backends:\n  k: { type: k8s, namespace: 'Mauvais_NS' }\n",
		"backends:\n  k: { type: k8s, namespace: '-x' }\n",
		"backends:\n  k: { type: k8s, namespace: ok, volumes: { /a: 'PVC_Maj' } }\n",
		"backends:\n  k: { type: k8s, context: 'a b' }\n",
		"backends:\n  k: { type: k8s, context: 'x;y' }\n",
		"backends:\n  d: { type: docker, host: 'ssh://-oProxyCommand=x' }\n",
		"backends:\n  d: { type: docker, host: 'ssh://u@-oProxyCommand=x' }\n",
		"backends:\n  d: { type: ssh, host: srv1, engine_host: 'ssh://-x' }\n",
		"backends:\n  d: { type: docker, hosts: ['ssh://-x'] }\n",
	} {
		if !HasErrors(ValidateConfig(ConfBackends, []byte(y))) {
			t.Errorf("accepté : %s", y)
		}
		var f backendsFile
		_ = yaml.Unmarshal([]byte(y), &f)
		if _, err := backendsOf(f); err == nil {
			t.Errorf("backendsOf accepte : %s", y)
		}
	}
	for _, y := range []string{
		"backends:\n  k: { type: k8s, context: 'k8s', namespace: claude-ng2, volumes: { /share/MFS/ROOT: claude-shared } }\n",
		"backends:\n  k: { type: k8s, context: 'admin@cluster.local', namespace: a, volumes: { /x: pvc.v1-b } }\n",
		"backends:\n  d: { type: docker, host: 'ssh://user@srv1' }\n",
	} {
		if HasErrors(ValidateConfig(ConfBackends, []byte(y))) {
			t.Errorf("refusé : %s → %v", y, ValidateConfig(ConfBackends, []byte(y)))
		}
	}
}

// R1 : le jeton est masqué partout où il figure dans l'éditeur de texte (commentaire,
// autre clé, tableau) ; le marqueur ne se restaure que dans GITHUB_TOKEN. R2 : un
// jeton actuel de forme dangereuse n'est pas réécrit.
func TestConfTokenMaskEverywhereAndRestoreOnlyInKey(t *testing.T) {
	const tok = "ghp_ABCDEFGH12345678"
	cur := "# GITHUB_TOKEN=\"" + tok + "\"   # ancien\nGITHUB_TOKEN=\"" + tok + "\"\nCLAUDE_OPTIONS=\"--t " + tok + "\"\nEXTRA_ENVS=(\"GH_TOKEN=" + tok + "\")\nexport GH_TOKEN=" + tok + "\n"
	masked := MaskConfToken(cur)
	if strings.Contains(masked, tok) || strings.Count(masked, ConfTokenKeep) != 5 {
		t.Fatalf("jeton encore visible (%d marqueurs) :\n%s", strings.Count(masked, ConfTokenKeep), masked)
	}
	// un jeton court n'est masqué que dans GITHUB_TOKEN (sinon n'importe quel texte le serait)
	if got := MaskConfToken("GITHUB_TOKEN=\"abc\"\nA=abcd\n"); !strings.Contains(got, "A=abcd") || strings.Contains(got, `GITHUB_TOKEN="abc"`) {
		t.Errorf("jeton court : %q", got)
	}
	// restauration : le jeton revient dans GITHUB_TOKEN seulement ; les autres occurrences refusées
	if _, err := RestoreConfToken(masked, cur); err == nil || !strings.Contains(err.Error(), "ne convient que pour GITHUB_TOKEN") {
		t.Errorf("marqueur dans une autre clé accepté : %v", err)
	}
	ok := "# GITHUB_TOKEN=\"" + ConfTokenKeep + "\"   # ancien\nGITHUB_TOKEN=\"" + ConfTokenKeep + "\"\nTEAM_HIDE_IDLE=5\n"
	got, err := RestoreConfToken(ok, cur)
	if err != nil || strings.Count(got, tok) != 1 || !strings.Contains(got, "GITHUB_TOKEN=\""+tok+"\"\nTEAM_HIDE_IDLE=5") || !strings.Contains(got, `# GITHUB_TOKEN="__GARDER__"`) {
		t.Errorf("restauration : %v\n%s", err, got)
	}
	for name, edited := range map[string]string{
		"tableau":   "GITHUB_TOKEN=\"__GARDER__\"\nEXTRA_ENVS=(\"X=__GARDER__\")\n",
		"commande":  "GITHUB_TOKEN=\"__GARDER__\"\necho __GARDER__\n",
		"autre clé": "GITHUB_TOKEN=\"__GARDER__\"\nGH_TOKEN=\"__GARDER__\"\n",
		"composée":  "GITHUB_TOKEN=\"__GARDER__\"\nA=\"x__GARDER__y\"\n",
	} {
		if _, err := RestoreConfToken(edited, "GITHUB_TOKEN=\"ghp_X12345678\"\n"); err == nil {
			t.Errorf("%s : accepté", name)
		}
	}
	// R2 : jeton actuel écrit à la main entre apostrophes avec « $ », guillemet ou « ` »
	for _, cur := range []string{"GITHUB_TOKEN='a$(id)bbbb'\n", "GITHUB_TOKEN='a\"b;touch x'\n", "GITHUB_TOKEN='a`id`bbbbb'\n"} {
		out, err := RestoreConfToken("GITHUB_TOKEN=\"__GARDER__\"\n", cur)
		if err == nil || out != "" || strings.Contains(err.Error(), "id") {
			t.Errorf("%q : %q %v", cur, out, err)
		}
	}
	// sans marqueur : rien à restaurer, aucune erreur
	if out, err := RestoreConfToken("A=1\n", "GITHUB_TOKEN='a$(id)'\n"); err != nil || out != "A=1\n" {
		t.Errorf("sans marqueur : %q %v", out, err)
	}
}

// M2 : le marqueur de l'éditeur de texte n'est jamais enregistré comme jeton par le
// chemin structuré (il écraserait le jeton actuel).
func TestSetTokenRefusesKeepMarker(t *testing.T) {
	src := "GITHUB_TOKEN=\"ghp_REALTOKEN1234\"\n"
	for _, v := range []string{ConfTokenKeep, "x" + ConfTokenKeep, ConfTokenKeep + "y"} {
		if out, err := ApplyConfConfChanges(src, []ConfigChange{cset("GITHUB_TOKEN", v)}); err == nil || out != "" || strings.Contains(err.Error(), "REALTOKEN") {
			t.Errorf("%q accepté : %q %v", v, out, err)
		}
	}
	if out := confApply(t, src, cset("GITHUB_TOKEN", "ghp_AUTRE12345678")); !strings.Contains(out, "ghp_AUTRE12345678") {
		t.Errorf("jeton valide refusé : %q", out)
	}
}

// M3 : une mise en page déjà incohérente dans le fichier ne bloque pas les autres
// réglages ; elle n'est contrôlée que si l'une des trois clés change.
func TestLayoutCheckOnlyWhenTouched(t *testing.T) {
	src := "LAYOUT_3ROW_LEADER_NUM=5\nLAYOUT_3ROW_MID_NUM=4\nLAYOUT_3ROW_DEN=7\n"
	if _, err := ApplyConfConfChanges(src, []ConfigChange{cset("TEAM_HIDE_IDLE", 10)}); err != nil {
		t.Errorf("autre réglage bloqué par une mise en page invalide : %v", err)
	}
	if _, err := ApplyConfConfChanges(src, []ConfigChange{cset("LAYOUT_3ROW_MID_NUM", 3)}); err == nil {
		t.Error("5 + 3 ≥ 7 accepté")
	}
	if out, err := ApplyConfConfChanges(src, []ConfigChange{cset("LAYOUT_3ROW_DEN", 12)}); err != nil || !strings.Contains(out, "DEN=12") {
		t.Errorf("correction de la mise en page refusée : %v", err)
	}
}

// M1 : le jeton recopié dans une autre valeur ne ressort pas du modèle structuré
// (champ masqué en lecture seule) ; un ancien jeton en commentaire est masqué dans
// l'éditeur de texte par sa forme.
func TestTokenMaskedInModelAndShapedTokensInComments(t *testing.T) {
	const tok = "ghp_ABCDEFGH12345678ABCD"
	src := "GITHUB_TOKEN=\"" + tok + "\"\nEXTRA_ENVS=(\"GH_TOKEN=" + tok + "\" \"A=1\")\nCLAUDE_OPTIONS=\"--t " + tok + "\"\n"
	m := ReadConfModel(src)
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), tok) {
		t.Fatalf("jeton dans le modèle : %s", b)
	}
	for _, k := range []string{"EXTRA_ENVS", "CLAUDE_OPTIONS"} {
		s := settingOf(m, k)
		if s.Editable || !strings.Contains(s.Why, "jeton") {
			t.Errorf("%s : %+v", k, s)
		}
	}
	if got := settingOf(m, "EXTRA_ENVS").Value; !reflect.DeepEqual(got, []string{"GH_TOKEN=***", "A=1"}) {
		t.Errorf("valeur masquée : %v", got)
	}
	if !settingOf(m, "GITHUB_TOKEN").Set || settingOf(m, "TEAM_HIDE_IDLE").Editable != true {
		t.Error("jeton ou autres réglages abîmés")
	}
	// ancien jeton (d'une autre valeur) laissé en commentaire : masqué par sa forme
	old := "# ancien GITHUB_TOKEN=\"ghp_ZZZZZZZZ99999999ZZZZ\"\n# pat github_pat_11ABCDEFG0123456789abcdef_xyz\nGITHUB_TOKEN=\"" + tok + "\"\n"
	masked := MaskConfToken(old)
	if strings.Contains(masked, "ZZZZZZZZ") || strings.Contains(masked, "github_pat_") || strings.Contains(masked, tok) || strings.Count(masked, ConfTokenKeep) != 3 {
		t.Errorf("masquage par forme :\n%s", masked)
	}
	// le marqueur laissé dans les commentaires est accepté à l'enregistrement
	if got, err := RestoreConfToken(masked, old); err != nil || !strings.Contains(got, `GITHUB_TOKEN="`+tok+`"`) || strings.Contains(got, "ZZZZZZZZ") {
		t.Errorf("restauration : %v\n%s", err, got)
	}
}
