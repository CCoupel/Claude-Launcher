package desktop

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #149 (LOT-3.3) — configModel / configApply pour config.conf, env.yaml du
// launcher et hubs.<machine>, selon contracts/desktop-dispatch.md (3657682).
// Fichiers factices en dossiers temporaires, jeton factice, jamais de vrai
// projet. RED tant que 3.3.1 n'est pas livré.

const fakeToken = "ghp_FACTICE0123456789abcdefABCDEF012345"

const confSample = `# Configuration de mon launcher — ne pas effacer
PROJECT_BASE="/share/PROJETS"   # base
GITHUB_DIRS=("$PROJECT_BASE" "/autre")
GITHUB_TOKEN="` + fakeToken + `"
RESUME_MODE=never   # jamais
TEAM_HIDE_IDLE=45
for d in /x /y; do CLAUDE_OPTIONS+=" $d"; done
USAGE_REFRESH_SEC=120
declare -A PROJECT_COLORS=(["a"]="52")
`

type lot33 struct {
	t *testing.T
	a *App
}

func newLot33(t *testing.T) lot33 {
	t.Helper()
	sandbox(t, "", "alpha")
	var r recorder
	return lot33{t, newApp(t, r.emit)}
}

func (l lot33) call(m string, args map[string]any) (map[string]any, string) {
	l.t.Helper()
	b, _ := json.Marshal(args)
	res, err := l.a.Dispatch(m, b)
	if err != nil {
		l.t.Fatalf("%s %v : %v", m, args["kind"], err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v, string(out)
}

func (l lot33) setting(model map[string]any, key string) map[string]any {
	l.t.Helper()
	for _, s := range model["model"].(map[string]any)["settings"].([]any) {
		if m := s.(map[string]any); m["key"] == key {
			return m
		}
	}
	l.t.Fatalf("réglage %s absent du modèle", key)
	return nil
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setKey(key string, v any) map[string]any {
	return map[string]any{"op": "set", "at": []string{key}, "value": v}
}

func apply(l lot33, kind string, mtime any, changes ...any) (map[string]any, string) {
	return l.call("configApply", map[string]any{"kind": kind, "mtime": mtime, "changes": changes})
}

// Modèle de config.conf : réglages typés, valeur écrite / défaut, forme non reconnue
// en lecture seule, jeton jamais renvoyé.
func TestConfigConfModel(t *testing.T) {
	l := newLot33(t)
	write(t, core.ConfigPath(), confSample)
	m, raw := l.call("configModel", map[string]any{"kind": "config.conf"})
	if m["exists"] != true {
		t.Fatalf("modèle : %v", m)
	}
	if s := l.setting(m, "TEAM_HIDE_IDLE"); s["value"] != 45.0 || s["set"] != true || s["default"] != 30.0 || s["type"] != "int" || s["editable"] != true {
		t.Errorf("TEAM_HIDE_IDLE : %v", s)
	}
	if s := l.setting(m, "RESUME_MODE"); s["value"] != "never" || s["type"] != "enum" || len(s["options"].([]any)) == 0 {
		t.Errorf("RESUME_MODE : %v", s)
	}
	if s := l.setting(m, "USAGE_REFRESH_SEC"); s["value"] != 120.0 {
		t.Errorf("USAGE_REFRESH_SEC : %v", s)
	}
	if s := l.setting(m, "LAYOUT_2ROW_LEADER_PCT"); s["set"] != false || s["value"] != s["default"] {
		t.Errorf("clé absente = valeur par défaut, set faux : %v", s)
	}
	// valeur non développée
	if s := l.setting(m, "GITHUB_DIRS"); s["type"] != "list" || len(s["value"].([]any)) != 2 || s["value"].([]any)[0] != "$PROJECT_BASE" {
		t.Errorf("GITHUB_DIRS non développé attendu : %v", s)
	}
	// forme non reconnue : lecture seule + signalée
	if s := l.setting(m, "CLAUDE_OPTIONS"); s["editable"] != false || s["why"] == nil || s["why"] == "" {
		t.Errorf("CLAUDE_OPTIONS (boucle +=) doit être en lecture seule : %v", s)
	}
	if un, _ := m["model"].(map[string]any)["unrecognized"].([]any); len(un) == 0 || un[0] != "CLAUDE_OPTIONS" {
		t.Errorf("unrecognized : %v", un)
	}
	// critical : le jeton n'est jamais renvoyé
	if s := l.setting(m, "GITHUB_TOKEN"); s["type"] != "secret" || s["set"] != true || s["value"] != nil {
		t.Errorf("GITHUB_TOKEN : %v", s)
	}
	if strings.Contains(raw, fakeToken) || strings.Contains(raw, "ghp_") {
		t.Errorf("jeton dans la réponse de configModel : %s", raw)
	}
}

// critical : un changement ne réécrit que la ligne de sa clé ; commentaires, forme
// non reconnue, jeton et autres lignes conservés ; delete retire l'affectation.
func TestConfigConfApplyKeepsEverythingElse(t *testing.T) {
	l := newLot33(t)
	write(t, core.ConfigPath(), confSample)
	m, _ := l.call("configModel", map[string]any{"kind": "config.conf"})
	r, raw := apply(l, "config.conf", m["mtime"], setKey("TEAM_HIDE_IDLE", 15), setKey("RESUME_MODE", "ask"))
	if r["saved"] != true || strings.Contains(raw, fakeToken) {
		t.Fatalf("enregistrement : %s", raw)
	}
	b, _ := os.ReadFile(core.ConfigPath())
	want := strings.NewReplacer("TEAM_HIDE_IDLE=45", "TEAM_HIDE_IDLE=15", "RESUME_MODE=never   # jamais", "RESUME_MODE=ask").Replace(confSample)
	got := string(b)
	for _, line := range strings.Split(want, "\n") {
		if strings.HasPrefix(line, "RESUME_MODE=") { // commentaire de fin de ligne : toléré
			continue
		}
		if line != "" && !strings.Contains(got, line) {
			t.Errorf("ligne perdue ou changée : %q\n%s", line, got)
		}
	}
	if !strings.Contains(got, "TEAM_HIDE_IDLE=15") || !strings.Contains(got, "RESUME_MODE=ask") || strings.Contains(got, "TEAM_HIDE_IDLE=45") {
		t.Errorf("valeurs non écrites :\n%s", got)
	}
	if !strings.Contains(got, fakeToken) {
		t.Error("jeton effacé par un enregistrement qui n'y touchait pas")
	}
	if _, err := os.Stat(core.ConfigPath() + ".bak"); err != nil {
		t.Error("copie .bak absente")
	}
	// delete : l'affectation est retirée, valeur par défaut au modèle
	m, _ = l.call("configModel", map[string]any{"kind": "config.conf"})
	r, _ = apply(l, "config.conf", m["mtime"], map[string]any{"op": "delete", "at": []string{"TEAM_HIDE_IDLE"}})
	if r["saved"] != true {
		t.Fatalf("delete : %v", r)
	}
	b, _ = os.ReadFile(core.ConfigPath())
	if strings.Contains(string(b), "TEAM_HIDE_IDLE=") {
		t.Errorf("affectation conservée :\n%s", b)
	}
	m, _ = l.call("configModel", map[string]any{"kind": "config.conf"})
	if s := l.setting(m, "TEAM_HIDE_IDLE"); s["set"] != false || s["value"] != 30.0 {
		t.Errorf("après delete : %v", s)
	}
	// clé absente : ajoutée, fichier toujours lisible par le launcher
	r, _ = apply(l, "config.conf", m["mtime"], setKey("CTR_STOP_ON_EXIT", true))
	if r["saved"] != true || !strings.Contains(read(t, core.ConfigPath()), "CTR_STOP_ON_EXIT=1") {
		t.Errorf("clé absente : %v\n%s", r, read(t, core.ConfigPath()))
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Valeurs refusées avant toute écriture (injection shell, type, borne) ; `$NOM`
// permis ; fichier inchangé.
func TestConfigConfApplyRefusesUnsafeValues(t *testing.T) {
	l := newLot33(t)
	write(t, core.ConfigPath(), confSample)
	m, _ := l.call("configModel", map[string]any{"kind": "config.conf"})
	for name, ch := range map[string]map[string]any{
		"$(":             setKey("CLAUDE_CONFIG_DIR", "$(rm -rf /)"),
		"guillemet":      setKey("CLAUDE_CONFIG_DIR", `a"b`),
		"antislash":      setKey("CLAUDE_CONFIG_DIR", `a\b`),
		"guillemet inv.": setKey("CLAUDE_CONFIG_DIR", "a`b`"),
		"retour ligne":   setKey("CLAUDE_CONFIG_DIR", "a\nrm -rf /"),
		"entier texte":   setKey("TEAM_HIDE_IDLE", "abc"),
		"entier négatif": setKey("TEAM_HIDE_IDLE", -1),
		"enum inconnue":  setKey("RESUME_MODE", "peut-être"),
		"liste piégée":   setKey("GITHUB_DIRS", []any{"/a", "$(id)"}),
	} {
		r, raw := apply(l, "config.conf", m["mtime"], ch)
		if r["invalid"] != true || len(r["problems"].([]any)) == 0 {
			t.Errorf("%s accepté : %s", name, raw)
		}
		if strings.Contains(raw, fakeToken) {
			t.Errorf("%s : jeton dans les problèmes : %s", name, raw)
		}
		if read(t, core.ConfigPath()) != confSample {
			t.Fatalf("%s : fichier modifié malgré le refus", name)
		}
	}
	if r, raw := apply(l, "config.conf", m["mtime"], setKey("CLAUDE_CONFIG_DIR", "$HOME/.claude")); r["saved"] != true {
		t.Errorf("$HOME refusé : %s", raw)
	}
	// conflit : date périmée
	if r, _ := apply(l, "config.conf", "2000-01-01T00:00:00Z", setKey("RESUME_MODE", "ask")); r["conflict"] != true {
		t.Errorf("conflit non signalé : %v", r)
	}
}

// Jeton : écrit seulement par un set explicite, jamais renvoyé, fichier en 0600 ;
// delete le retire ; un message de problème ne le contient pas.
func TestConfigConfTokenNeverLeaksAndFileIs0600(t *testing.T) {
	l := newLot33(t)
	write(t, core.ConfigPath(), confSample)
	m, _ := l.call("configModel", map[string]any{"kind": "config.conf"})
	nouveau := "ghp_NOUVEAUFACTICE9876543210zyxwvuTSRQP"
	r, raw := apply(l, "config.conf", m["mtime"], setKey("GITHUB_TOKEN", nouveau))
	if r["saved"] != true || strings.Contains(raw, nouveau) || strings.Contains(raw, fakeToken) {
		t.Fatalf("set du jeton : %s", raw)
	}
	if c := read(t, core.ConfigPath()); !strings.Contains(c, nouveau) || strings.Contains(c, fakeToken) {
		t.Errorf("jeton non remplacé :\n%s", c)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(core.ConfigPath()); st.Mode().Perm() != 0o600 {
			t.Errorf("droits de config.conf = %v, attendu 0600", st.Mode().Perm())
		}
	}
	m, raw = l.call("configModel", map[string]any{"kind": "config.conf"})
	if strings.Contains(raw, nouveau) {
		t.Errorf("jeton relu en clair : %s", raw)
	}
	r, _ = apply(l, "config.conf", m["mtime"], map[string]any{"op": "delete", "at": []string{"GITHUB_TOKEN"}})
	if r["saved"] != true || strings.Contains(read(t, core.ConfigPath()), "GITHUB_TOKEN=") {
		t.Errorf("delete du jeton : %v\n%s", r, read(t, core.ConfigPath()))
	}
	// un fichier invalide ailleurs : les problèmes ne citent pas le jeton
	write(t, core.ConfigPath(), strings.Replace(confSample, "USAGE_REFRESH_SEC=120", "USAGE_REFRESH_SEC=(", 1))
	if _, raw = l.call("configModel", map[string]any{"kind": "config.conf"}); strings.Contains(raw, fakeToken) {
		t.Errorf("jeton dans les problèmes d'un fichier invalide : %s", raw)
	}
}

// config.conf absent : modèle aux valeurs par défaut ; créé depuis le modèle à
// l'enregistrement, en 0600 ; CRLF conservé sur un fichier Windows.
func TestConfigConfCreatedAndCRLF(t *testing.T) {
	l := newLot33(t)
	_ = os.Remove(core.ConfigPath())
	m, _ := l.call("configModel", map[string]any{"kind": "config.conf"})
	if m["exists"] != false || l.setting(m, "TEAM_HIDE_IDLE")["set"] != false {
		t.Fatalf("fichier absent : %v", m)
	}
	r, raw := apply(l, "config.conf", "", setKey("TEAM_HIDE_IDLE", 20))
	if r["saved"] != true {
		t.Fatalf("création : %s", raw)
	}
	if !strings.Contains(read(t, core.ConfigPath()), "TEAM_HIDE_IDLE=20") {
		t.Errorf("clé absente du fichier créé :\n%s", read(t, core.ConfigPath()))
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(core.ConfigPath()); st.Mode().Perm() != 0o600 {
			t.Errorf("droits du fichier créé = %v", st.Mode().Perm())
		}
	}
	crlf := strings.ReplaceAll(confSample, "\n", "\r\n")
	write(t, core.ConfigPath(), crlf)
	m, _ = l.call("configModel", map[string]any{"kind": "config.conf"})
	if r, raw := apply(l, "config.conf", m["mtime"], setKey("TEAM_HIDE_IDLE", 10)); r["saved"] != true {
		t.Fatalf("CRLF : %s", raw)
	}
	got := read(t, core.ConfigPath())
	if !strings.Contains(got, "TEAM_HIDE_IDLE=10") || !strings.Contains(got, "# Configuration de mon launcher — ne pas effacer\r\n") ||
		!strings.Contains(got, "USAGE_REFRESH_SEC=120") {
		t.Errorf("fichier CRLF abîmé : %q", got)
	}
}

// env.yaml du launcher : seules les quatre clés ; commentaires conservés ; une
// clé réservée au projet est refusée.
func TestLauncherEnvKind(t *testing.T) {
	l := newLot33(t)
	write(t, core.LauncherEnvPath(), "# outillage commun\npackages:\n  - gh   # cli\nsetup:\n  - echo hi\n")
	m, _ := l.call("configModel", map[string]any{"kind": "env.yaml"})
	md := m["model"].(map[string]any)
	for _, k := range []string{"packages", "packages_by_manager", "setup", "on_start"} {
		if _, ok := md[k]; !ok {
			t.Errorf("clé %q absente du modèle : %v", k, md)
		}
	}
	r, raw := apply(l, "env.yaml", m["mtime"], map[string]any{"op": "set", "at": []string{"on_start"}, "value": []any{"git config --global --add safe.directory '*'"}})
	if r["saved"] != true {
		t.Fatalf("env.yaml : %s", raw)
	}
	got := read(t, core.LauncherEnvPath())
	if !strings.Contains(got, "# outillage commun") || !strings.Contains(got, "# cli") || !strings.Contains(got, "safe.directory") {
		t.Errorf("env.yaml du launcher :\n%s", got)
	}
	m, _ = l.call("configModel", map[string]any{"kind": "env.yaml"})
	r, _ = apply(l, "env.yaml", m["mtime"], map[string]any{"op": "set", "at": []string{"image"}, "value": "debian:12"})
	if r["invalid"] != true || !strings.Contains(read(t, core.LauncherEnvPath()), "# outillage commun") || strings.Contains(read(t, core.LauncherEnvPath()), "image:") {
		t.Errorf("clé réservée au projet acceptée : %v\n%s", r, read(t, core.LauncherEnvPath()))
	}
	// fichier absent : modèle vide, création à l'enregistrement
	_ = os.Remove(core.LauncherEnvPath())
	m, _ = l.call("configModel", map[string]any{"kind": "env.yaml"})
	if m["exists"] != false {
		t.Errorf("fichier absent : %v", m)
	}
	if r, _ := apply(l, "env.yaml", "", map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"jq"}}); r["saved"] != true {
		t.Errorf("création : %v", r)
	}
}

// hubs.<machine> : section de CE poste lue et écrite, surcharges par système,
// autres machines jamais touchées ; section absente créée au premier set.
func TestHubKind(t *testing.T) {
	l := newLot33(t)
	host, _ := os.Hostname()
	yaml := "# réglages par poste\nhubs:\n  " + host + ":\n    paths:\n      - {local: /mnt/x, remote: /share/x}   # commun\n" +
		"    windows:\n      paths:\n        - {local: 'C:\\x', remote: /share/x}\n  AUTRE-PC:\n    paths:\n      - {local: /a, remote: /b}\n"
	write(t, core.BackendsPath(), yaml)
	m, _ := l.call("configModel", map[string]any{"kind": "hub"})
	md := m["model"].(map[string]any)
	if md["hub"] == "" || md["present"] != true || md["goos"] != runtime.GOOS {
		t.Fatalf("hub : %v", md)
	}
	if ps := md["settings"].(map[string]any)["paths"].([]any); len(ps) != 1 || ps[0].(map[string]any)["local"] != "/mnt/x" {
		t.Errorf("paths communs : %v", ps)
	}
	ov := md["overrides"].(map[string]any)
	if w, ok := ov["windows"].(map[string]any); !ok || len(w["keys"].([]any)) != 1 || w["keys"].([]any)[0] != "paths" {
		t.Errorf("surcharge windows : %v", ov)
	}
	wantEff := ""
	if runtime.GOOS == "windows" {
		wantEff = "windows"
	}
	if eff := md["effective"].(map[string]any)["paths"]; eff != wantEff {
		t.Errorf("effective.paths = %v, attendu %q", eff, wantEff)
	}
	// set d'un réglage commun : commentaires et section de l'autre machine intacts
	r, raw := apply(l, "hub", m["mtime"], map[string]any{"op": "set", "at": []string{"hub_cache"}, "value": "/cache/hub"})
	if r["saved"] != true {
		t.Fatalf("hub set : %s", raw)
	}
	got := read(t, core.BackendsPath())
	for _, want := range []string{"# réglages par poste", "# commun", "AUTRE-PC:", "/cache/hub", "{local: /a, remote: /b}"} {
		if !strings.Contains(strings.ReplaceAll(got, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("%q perdu :\n%s", want, got)
		}
	}
	// surcharge par système : at relatif à la section, la forme d'un autre système reste
	m, _ = l.call("configModel", map[string]any{"kind": "hub"})
	r, raw = apply(l, "hub", m["mtime"], map[string]any{"op": "set", "at": []string{"linux", "hub_cache"}, "value": "/mnt/c/cache"})
	if r["saved"] != true || !strings.Contains(read(t, core.BackendsPath()), "C:\\x") {
		t.Errorf("surcharge linux : %s\n%s", raw, read(t, core.BackendsPath()))
	}
	// section absente : créée sous le nom de la machine, rien d'autre touché
	write(t, core.BackendsPath(), "# seul\nhubs:\n  AUTRE-PC:\n    hub_cache: /z\n")
	m, _ = l.call("configModel", map[string]any{"kind": "hub"})
	if m["model"].(map[string]any)["present"] != false {
		t.Fatalf("present attendu faux : %v", m["model"])
	}
	if r, raw := apply(l, "hub", m["mtime"], map[string]any{"op": "set", "at": []string{"hub_cache"}, "value": "/c"}); r["saved"] != true {
		t.Fatalf("création de section : %s", raw)
	}
	got = read(t, core.BackendsPath())
	if !strings.Contains(got, "AUTRE-PC:") || !strings.Contains(got, "/z") || !strings.Contains(got, "# seul") || !strings.Contains(got, "/c") {
		t.Errorf("après création :\n%s", got)
	}
}
