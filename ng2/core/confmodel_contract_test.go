package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #149 (LOT-3.3) — compléments à confmodel_test.go : ce que bash relit vraiment
// (valeurs à caractères spéciaux sans effet de bord), clés citées en commentaire,
// formes `export` / conditionnelle / sur une ligne, tableau multiligne, CRLF,
// et hubs.<machine> / env.yaml du launcher. Fichiers temporaires, aucun réseau.

// bashValue : ce que bash lit pour name après avoir sourcé src (sans effet de bord).
func bashValue(t *testing.T, src, name string) string {
	t.Helper()
	unixOnly(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "config.conf")
	if err := os.WriteFile(f, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `cd "$1" && source "$2" && printf %s "${!3}"`, "bash", dir, f, name)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash a refusé le fichier : %v\n%s", err, src)
	}
	return string(out)
}

// Une valeur « difficile mais permise » est relue à l'identique par bash et
// n'exécute rien (aucun fichier créé), pour une chaîne et pour une variable ajoutée.
func TestConfValueRoundTripsThroughBash(t *testing.T) {
	unixOnly(t)
	sentinel := filepath.Join(t.TempDir(), "PWNED")
	hard := []string{
		"--a --b=c",
		"a; touch " + sentinel,
		"x && touch " + sentinel + " || y",
		"p|q > " + sentinel,
		"glob * ? [a-z] ~ ! # pas un commentaire",
		"é à ü — 日本",
		"  espaces autour  ",
		"$HOME/dir et ${HOME}x",
	}
	for _, v := range hard {
		out, err := ApplyConfConfChanges("# c\nX=1\n", []ConfigChange{cset("CLAUDE_OPTIONS", v)})
		if err != nil {
			t.Errorf("%q refusé : %v", v, err)
			continue
		}
		want := v
		if strings.Contains(v, "$") { // $NOM permis : développé par bash comme dans le fichier d'origine
			want = os.ExpandEnv(v)
		}
		if got := bashValue(t, out, "CLAUDE_OPTIONS"); got != want {
			t.Errorf("%q relu %q par bash\n%s", v, got, out)
		}
		if _, err := os.Stat(sentinel); err == nil {
			t.Fatalf("valeur %q : une commande a été exécutée en sourçant le fichier", v)
		}
		if HasErrors(ValidateConfig(ConfConf, []byte(out))) {
			t.Errorf("%q : fichier invalide : %v", v, ValidateConfig(ConfConf, []byte(out)))
		}
		if m := ReadConfModel(out); settingOf(m, "CLAUDE_OPTIONS").Value != v {
			t.Errorf("%q : modèle relu %v", v, settingOf(m, "CLAUDE_OPTIONS").Value)
		}
	}
	// liste : chaque élément reste un élément (espaces, signes =)
	items := []any{"A=une valeur", "B=x;y", "C=$HOME"}
	out, err := ApplyConfConfChanges("", []ConfigChange{cset("EXTRA_ENVS", items)})
	if err != nil {
		t.Fatal(err)
	}
	got := ReadConfModel(out)
	if v, _ := settingOf(got, "EXTRA_ENVS").Value.([]string); len(v) != 3 || v[0] != "A=une valeur" || v[1] != "B=x;y" {
		t.Errorf("EXTRA_ENVS relu : %v\n%s", settingOf(got, "EXTRA_ENVS").Value, out)
	}
	cnt := exec.Command("bash", "-c", `source "$1"; printf %s "${#EXTRA_ENVS[@]}"`, "bash", writeTemp(t, out))
	if b, err := cnt.Output(); err != nil || string(b) != "3" {
		t.Errorf("bash lit %q éléments (%v)\n%s", b, err, out)
	}
}

func writeTemp(t *testing.T, s string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// Une clé citée seulement en commentaire n'est ni lue ni modifiée ; l'ajout se
// fait sans toucher au commentaire ; clé voisine (préfixe) non confondue.
func TestConfCommentedAndNeighbourKeys(t *testing.T) {
	src := "# TEAM_HIDE_IDLE=99   (exemple)\n# RESUME_MODE=always\nTEAM_HIDE_IDLE_X=7\nMY_RESUME_MODE=always\n"
	m := ReadConfModel(src)
	if s := settingOf(m, "TEAM_HIDE_IDLE"); s.Set || s.Value != 30 || !s.Editable {
		t.Errorf("TEAM_HIDE_IDLE lu depuis un commentaire ou une clé voisine : %+v", s)
	}
	if s := settingOf(m, "RESUME_MODE"); s.Set || s.Value != "ask" {
		t.Errorf("RESUME_MODE : %+v", s)
	}
	out := confApply(t, src, cset("TEAM_HIDE_IDLE", 12), cset("RESUME_MODE", "never"))
	for _, keep := range []string{"# TEAM_HIDE_IDLE=99   (exemple)", "# RESUME_MODE=always", "TEAM_HIDE_IDLE_X=7", "MY_RESUME_MODE=always"} {
		if !strings.Contains(out, keep+"\n") {
			t.Errorf("« %s » modifié ou perdu :\n%s", keep, out)
		}
	}
	if bashValue(t, out, "TEAM_HIDE_IDLE") != "12" || bashValue(t, out, "RESUME_MODE") != "never" {
		t.Errorf("bash ne lit pas les valeurs écrites :\n%s", out)
	}
}

// Formes de l'affectation : `export`, `A=1; B=2` sur une ligne, tableau multiligne
// avec commentaires, valeur entre apostrophes. Le set n'abîme jamais le reste.
func TestConfAssignmentForms(t *testing.T) {
	unixOnly(t)
	src := "export TEAM_HIDE_IDLE=45\nUSAGE_REFRESH_SEC=100; RESUME_MAX_AGE_H=6\nGITHUB_DIRS=(\n  \"/a\"   # premier\n  '/b c'\n)\nCLAUDE_OPTIONS='--x'\nAFTER=ok\n"
	m := ReadConfModel(src)
	if s := settingOf(m, "TEAM_HIDE_IDLE"); s.Value != 45 || !s.Set {
		t.Errorf("export : %+v", s)
	}
	if s := settingOf(m, "RESUME_MAX_AGE_H"); s.Value != 6 {
		t.Errorf("deuxième affectation d'une ligne : %+v", s)
	}
	if s, _ := settingOf(m, "GITHUB_DIRS").Value.([]string); len(s) != 2 || s[0] != "/a" || s[1] != "/b c" {
		t.Errorf("tableau multiligne : %v", settingOf(m, "GITHUB_DIRS").Value)
	}
	if s := settingOf(m, "CLAUDE_OPTIONS"); s.Value != "--x" {
		t.Errorf("apostrophes : %+v", s)
	}
	out := confApply(t, src, cset("TEAM_HIDE_IDLE", 5), cset("RESUME_MAX_AGE_H", 8), cset("GITHUB_DIRS", []any{"/n1", "/n 2"}))
	if !strings.Contains(out, "AFTER=ok") || !strings.Contains(out, "USAGE_REFRESH_SEC=100") {
		t.Errorf("ligne voisine perdue :\n%s", out)
	}
	for k, want := range map[string]string{"TEAM_HIDE_IDLE": "5", "RESUME_MAX_AGE_H": "8", "USAGE_REFRESH_SEC": "100", "CLAUDE_OPTIONS": "--x", "AFTER": "ok"} {
		if got := bashValue(t, out, k); got != want {
			t.Errorf("bash lit %s = %q, attendu %q\n%s", k, got, want, out)
		}
	}
	if HasErrors(ValidateConfig(ConfConf, []byte(out))) {
		t.Errorf("invalide : %v\n%s", ValidateConfig(ConfConf, []byte(out)), out)
	}
}

// Les 20 réglages : une valeur valide écrite est relue par le modèle, acceptée
// par la validation réelle, et bash source le fichier sans erreur.
func TestConfEverySettingWritesAndReadsBack(t *testing.T) {
	unixOnly(t)
	valid := map[string]any{
		"GITHUB_DIR": "/g", "GITHUB_DIRS": []any{"/g1", "/g2"}, "CLAUDE_OPTIONS": "--o", "CLAUDE_DISABLE_MOUSE": false,
		"CLAUDE_EXPERIMENTAL_TEAMS": false, "CLAUDE_CONFIG_DIR": "../.claude", "EXTRA_ENVS": []any{"A=1"},
		"RESUME_MODE": "always", "RESUME_MAX_AGE_H": 12, "TEAM_HIDE_IDLE": 0, "LAYOUT_2ROW_LEADER_PCT": 60,
		"LAYOUT_3ROW_LEADER_NUM": 2, "LAYOUT_3ROW_MID_NUM": 1, "LAYOUT_3ROW_DEN": 7, "PANE_ACTIVE_BRIGHTEN_STEP": 2,
		"PANE_INACTIVE_DARKEN_STEP": 3, "COLOR_PALETTE": []any{"17", "255", "0"}, "CTR_STOP_ON_EXIT": true, "USAGE_REFRESH_SEC": 60,
	}
	base := ReadConfModel("")
	for _, s := range base.Settings {
		if s.Type == "secret" {
			continue
		}
		v, ok := valid[s.Key]
		if !ok {
			t.Errorf("réglage %s sans valeur de test : ajouter au test", s.Key)
			continue
		}
		out, err := ApplyConfConfChanges("# c\n", []ConfigChange{cset(s.Key, v)})
		if err != nil {
			t.Errorf("%s = %v refusé : %v", s.Key, v, err)
			continue
		}
		if ps := ValidateConfig(ConfConf, []byte(out)); HasErrors(ps) {
			t.Errorf("%s : fichier invalide : %v\n%s", s.Key, ps, out)
		}
		got := settingOf(ReadConfModel(out), s.Key)
		if !got.Set || !got.Editable {
			t.Errorf("%s : relu %+v\n%s", s.Key, got, out)
		}
		bashValue(t, out, s.Key) // bash source sans erreur
	}
	// valeurs aux limites refusées pour chaque borne déclarée
	for _, bad := range []struct {
		k string
		v any
	}{{"USAGE_REFRESH_SEC", 59}, {"LAYOUT_2ROW_LEADER_PCT", 100}, {"LAYOUT_2ROW_LEADER_PCT", 0}, {"PANE_ACTIVE_BRIGHTEN_STEP", 6},
		{"RESUME_MAX_AGE_H", 0}, {"COLOR_PALETTE", []any{"256"}}, {"COLOR_PALETTE", []any{"x"}}, {"CTR_STOP_ON_EXIT", "oui"}} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{cset(bad.k, bad.v)}); err == nil {
			t.Errorf("%s = %v accepté", bad.k, bad.v)
		}
	}
}

// Un fichier CRLF garde ses fins de ligne après plusieurs changements ; le jeton
// n'apparaît dans aucune erreur renvoyée.
func TestConfCRLFAndTokenInErrors(t *testing.T) {
	const tok = "ghp_FACTICEcrlf0123456789ABCDEFghijklmn"
	src := "# c\r\nGITHUB_TOKEN=\"" + tok + "\"\r\nTEAM_HIDE_IDLE=45\r\n"
	out := confApply(t, src, cset("TEAM_HIDE_IDLE", 9), cset("RESUME_MODE", "never"))
	if strings.Count(out, "\n") != strings.Count(out, "\r\n") {
		t.Errorf("fins de ligne mélangées :\n%q", out)
	}
	if !strings.Contains(out, tok) {
		t.Error("jeton effacé")
	}
	_, err := ApplyConfConfChanges(src, []ConfigChange{cset("GITHUB_TOKEN", "avec\"guillemet")})
	if err == nil {
		t.Fatal("jeton à guillemet accepté")
	}
	if strings.Contains(err.Error(), tok) || strings.Contains(err.Error(), "guillemet") && strings.Contains(err.Error(), "avec") {
		t.Errorf("message d'erreur qui cite un jeton : %v", err)
	}
}

// hubs.<machine> : lecture sous deux systèmes, `effective` pour chaque clé, autre
// machine intacte ; nom de machine insensible à la casse et au domaine.
func TestHubModelSystemsAndHostMatching(t *testing.T) {
	content := []byte("hubs:\n  Mon-PC:\n    paths:\n      - {local: /mnt/x, remote: /share/x}\n    hub_cache: /c\n" +
		"    windows:\n      paths:\n        - {local: 'C:\\x', remote: /share/x}\n  AUTRE:\n    hub_cache: /z\n")
	for _, host := range []string{"mon-pc", "MON-PC.example.lan", "Mon-PC"} {
		m, err := ReadHubModel(content, host, "windows")
		if err != nil || !m.Present {
			t.Errorf("%s : %+v %v", host, m, err)
			continue
		}
		if m.Effective["paths"] != "windows" || m.Effective["hub_cache"] != "" {
			t.Errorf("%s sous windows : effective %v", host, m.Effective)
		}
		if l, err := ReadHubModel(content, host, "linux"); err != nil || l.Effective["paths"] != "" {
			t.Errorf("%s sous linux : %+v %v", host, l.Effective, err)
		}
	}
	m, _ := ReadHubModel(content, "inconnue", "linux")
	if m.Present {
		t.Error("section présente pour une machine inconnue")
	}
	out, err := ApplyHubChanges(content, "mon-pc", []ConfigChange{{Op: "set", At: []string{"hub_cache"}, Value: "/n"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "AUTRE:") || !strings.Contains(s, "/z") || !strings.Contains(s, "Mon-PC:") || strings.Count(s, "Mon-PC:") != 1 {
		t.Errorf("section de la machine dupliquée ou autre machine abîmée :\n%s", s)
	}
}

// Liste blanche contre l'injection : valeurs refusées / permises (cas limites de
// `$`), et clés ou formes de changement refusées avant toute écriture.
func TestCheckConfStringEdgeCases(t *testing.T) {
	for _, v := range []string{
		"a$", "$", "$1", "$$", "$?", "$@", "$*", "$-", "${HOME:-x}", "${!HOME}", "${#HOME}", "${HOME", "${}", "${1}",
		"$((1+1))", "$'x'", `$"x"`, "$(id)", "a`id`", `a\b`, `a"b`, "x\ry", "x\ny", "x\x00y", "${HOME}${",
		// variables hors liste blanche : jamais un secret recopié dans une autre valeur
		"$GITHUB_TOKEN", "${GITHUB_TOKEN}", "x=$GH_TOKEN", "${_x1}", "a$B_c-d", "$PATH",
	} {
		if err := checkConfString(v); err == nil {
			t.Errorf("%q accepté", v)
		}
	}
	for _, v := range []string{"$HOME", "${HOME}", "${XDG_CACHE_HOME}", "a$USER-d", "$GITHUB_DIR/x", "${CLAUDE_CONFIG_DIR}", "'simple'", "tab\tdedans", "100%", "a b  c", "é$HOME", "", "x # y", "a;b&c|d"} {
		if err := checkConfString(v); err != nil {
			t.Errorf("%q refusé : %v", v, err)
		}
	}
}

func TestConfApplyRefusesBadKeysAndShapes(t *testing.T) {
	src := "# c\nTEAM_HIDE_IDLE=45\n"
	for name, ch := range map[string]ConfigChange{
		"clé inconnue":           cset("PATH", "/x"),
		"clé minuscule":          cset("team_hide_idle", 1),
		"clé avec point-virgule": cset("TEAM_HIDE_IDLE; touch /tmp/x", 1),
		"clé avec espace":        cset("TEAM_HIDE_IDLE ", 1),
		"clé gérée ailleurs":     cset("PROJECT_COLORS", "x"),
		"LD_PRELOAD":             cset("LD_PRELOAD", "/x.so"),
		"chemin de 2 niveaux":    {Op: "set", At: []string{"TEAM_HIDE_IDLE", "x"}, Value: 1},
		"chemin vide":            {Op: "set", At: nil, Value: 1},
		"opération inconnue":     {Op: "append", At: []string{"TEAM_HIDE_IDLE"}, Value: 1},
		"rename":                 {Op: "rename", At: []string{"TEAM_HIDE_IDLE"}, Value: "AUTRE"},
		"delete inconnue":        {Op: "delete", At: []string{"PATH"}},
		"entier en objet":        cset("TEAM_HIDE_IDLE", map[string]any{"a": 1}),
		"liste d'objets":         cset("GITHUB_DIRS", []any{map[string]any{"a": 1}}),
		"liste pour un entier":   cset("TEAM_HIDE_IDLE", []any{1}),
		"texte pour bool":        cset("CTR_STOP_ON_EXIT", "1; touch x"),
		"enum avec injection":    cset("RESUME_MODE", "never; touch x"),
	} {
		out, err := ApplyConfConfChanges(src, []ConfigChange{ch})
		if err == nil {
			t.Errorf("%s accepté :\n%s", name, out)
		}
	}
	// atomique : un changement valide suivi d'un refusé ne rend rien d'écrit
	out, err := ApplyConfConfChanges(src, []ConfigChange{cset("TEAM_HIDE_IDLE", 5), cset("PATH", "/x")})
	if err == nil || strings.Contains(out, "TEAM_HIDE_IDLE=5") {
		t.Errorf("changements partiels : %v\n%s", err, out)
	}
}

// env.yaml du launcher : chaque clé réservée aux projets est refusée par la
// validation, et SaveConfig n'écrit rien (fichier inchangé).
func TestLauncherEnvRejectsProjectOnlyKeys(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", dir)
	path := LauncherEnvPath()
	const orig = "# outillage commun\npackages: [gh]\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	for _, key := range []string{"image", "prefer", "watch", "local", "mounts", "inconnue"} {
		var val any = "x"
		switch key {
		case "watch", "local":
			val = []any{"y"}
		case "mounts":
			val = []any{map[string]any{"path": "/x"}}
		}
		bad, err := ApplyConfigChanges(ConfLauncherEnv, []byte(orig), []ConfigChange{{Op: "set", At: []string{key}, Value: val}})
		if err != nil {
			continue // refusé dès l'application : acceptable
		}
		ps := ValidateConfig(ConfLauncherEnv, bad)
		if !HasErrors(ps) {
			t.Errorf("« %s » acceptée dans l'env.yaml du launcher", key)
			continue
		}
		if !strings.Contains(ps[0].Message, key) {
			t.Errorf("« %s » : le problème ne nomme pas la clé : %q", key, ps[0].Message)
		}
		if _, err := SaveConfig(ConfLauncherEnv, path, bad, st.ModTime()); err != ErrConfigInvalid {
			t.Errorf("« %s » : SaveConfig = %v, attendu ErrConfigInvalid", key, err)
		}
		if b, _ := os.ReadFile(path); string(b) != orig {
			t.Fatalf("« %s » : fichier modifié malgré le refus :\n%s", key, b)
		}
	}
	// clés permises, types invalides refusés
	ok := "packages: [a]\npackages_by_manager: {apt: [b]}\nsetup: [c]\non_start: [d]\n"
	if HasErrors(ValidateConfig(ConfLauncherEnv, []byte(ok))) {
		t.Errorf("clés permises refusées : %v", ValidateConfig(ConfLauncherEnv, []byte(ok)))
	}
	if !HasErrors(ValidateConfig(ConfLauncherEnv, []byte("packages: gh\n"))) {
		t.Error("packages en chaîne accepté")
	}
}

// Fichiers du launcher en 0600 à l'écriture (config.conf et sa copie, déjà vu) :
// un fichier créé par SaveConfig l'est aussi, y compris sous un dossier à créer.
func TestSaveConfigCreatesFileIs0600(t *testing.T) {
	unixOnly(t)
	dir := filepath.Join(t.TempDir(), "neuf", "cfg")
	t.Setenv("CL_CONFIG_DIR", dir)
	path := ConfigPath()
	if _, err := SaveConfig(ConfConf, path, []byte("TEAM_HIDE_IDLE=9\nGITHUB_TOKEN=\"ghp_FACTICE\"\n"), time.Time{}); err != nil {
		t.Fatalf("création : %v", err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("fichier créé : %v %v", st, err)
	}
}
