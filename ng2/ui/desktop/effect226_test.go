//go:build !windows

package desktop

// #226 : effet de chaque option de config.conf (table unique dans core : effect / effectHow dans configModel) et retour
// `apply` de configApply / configSave (options dont la valeur EFFECTIVE a changé, relance du launcher, projets ouverts
// concernés). Écrit sur le contrat (contracts/desktop-dispatch.md « v6.6.0 — LOT-4 ») : boîte noire par Dispatch, jamais de
// vrai projet ni de vrai settings.json (sandbox : HOME / CLAUDE_CONFIG_DIR temporaires).

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

type fix226 struct {
	t    *testing.T
	a    *App
	proj string
}

func newFix226(t *testing.T) fix226 {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	return fix226{t, a, ps[0]}
}

func (f fix226) call(m string, args map[string]any) map[string]any {
	f.t.Helper()
	b, _ := json.Marshal(args)
	res, err := f.a.Dispatch(m, b)
	if err != nil {
		f.t.Fatalf("%s : %v", m, err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func set226(k string, v any) map[string]any {
	return map[string]any{"op": "set", "at": []string{k}, "value": v}
}

// apply226 : configApply de config.conf ; rend la réponse (mtime relu à chaque fois).
func (f fix226) apply(changes ...map[string]any) map[string]any {
	f.t.Helper()
	m := f.call("configModel", map[string]any{"kind": "config.conf"})
	cs := make([]any, len(changes))
	for i, c := range changes {
		cs[i] = c
	}
	r := f.call("configApply", map[string]any{"kind": "config.conf", "mtime": m["mtime"], "changes": cs})
	if r["saved"] != true {
		f.t.Fatalf("configApply : %v", r)
	}
	return r
}

func changed226(t *testing.T, r map[string]any) map[string]string {
	t.Helper()
	ap, _ := r["apply"].(map[string]any)
	if ap == nil {
		t.Fatalf("réponse sans `apply` : %v", r)
	}
	out := map[string]string{}
	l, _ := ap["changed"].([]any)
	for _, it := range l {
		m := it.(map[string]any)
		out[m["key"].(string)], _ = m["effect"].(string)
	}
	return out
}

var effects226 = map[string]bool{"immediate": true, "project": true, "claude": true, "launcher": true}

// Couverture de la table : CHAQUE réglage du modèle a un effet dans la liste fermée et une bulle complète ; les valeurs
// annoncées par le contrat sont exactes.
func TestEffetDeChaqueOptionDuModele226(t *testing.T) {
	f := newFix226(t)
	m := f.call("configModel", map[string]any{"kind": "config.conf"})
	settings := m["model"].(map[string]any)["settings"].([]any)
	if len(settings) < 15 {
		t.Fatalf("modèle : %d réglages", len(settings))
	}
	want := map[string]string{
		"CLAUDE_DISABLE_MOUSE": "claude", "CLAUDE_EXPERIMENTAL_TEAMS": "claude", "EXTRA_ENVS": "claude", "GITHUB_TOKEN": "claude",
		"CLAUDE_OPTIONS": "project", "RESUME_MODE": "project", "RESUME_MAX_AGE_H": "project",
		"GITHUB_DIR": "launcher", "GITHUB_DIRS": "launcher", "CLAUDE_CONFIG_DIR": "launcher",
		"TEAM_HIDE_IDLE": "immediate", "TEAM_HIDE_IDLE_ENABLED": "immediate", "LAYOUT_MODE": "immediate", "USAGE_REFRESH_SEC": "immediate",
	}
	seen := map[string]bool{}
	for _, it := range settings {
		s := it.(map[string]any)
		key := s["key"].(string)
		seen[key] = true
		eff, _ := s["effect"].(string)
		if !effects226[eff] {
			t.Errorf("%s : effect = %q, attendu immediate|project|claude|launcher (clé connue du modèle : pas de trou dans la table)", key, eff)
			continue
		}
		if w, ok := want[key]; ok && eff != w {
			t.Errorf("%s : effect = %q, attendu %q (contrat)", key, eff, w)
		}
		how, _ := s["effectHow"].(map[string]any)
		if how == nil {
			t.Errorf("%s : effectHow absent (bulle)", key)
			continue
		}
		for _, k := range []string{"why", "do", "shell"} {
			if v, _ := how[k].(string); strings.TrimSpace(v) == "" && !(eff == "immediate" && k != "why") {
				t.Errorf("%s : effectHow.%s vide (effet %s)", key, k, eff)
			}
		}
		act, _ := how["action"].(string)
		switch eff {
		case "immediate":
			if act != "" {
				t.Errorf("%s : immédiat mais action = %q (aucun lien d'action)", key, act)
			}
		case "launcher":
			if act != "relaunch" {
				t.Errorf("%s : action = %q, attendu relaunch (#219)", key, act)
			}
		case "claude":
			if act != "refresh" {
				t.Errorf("%s : action = %q, attendu refresh (#158)", key, act)
			}
		default:
			if act != "refresh" && act != "reopen" && act != "" {
				t.Errorf("%s : action = %q hors liste fermée", key, act)
			}
		}
		if _, old := s["effectHelp"]; old {
			t.Errorf("%s : effectHelp (remplacé par effectHow) encore présent", key)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("clé %s du contrat absente du modèle", k)
		}
	}
	// textes affichés au clavier / en bulle : jamais de balisage venu de la table (rendus en texte par l'interface)
	for _, it := range settings {
		if h, _ := it.(map[string]any)["effectHow"].(map[string]any); h != nil {
			for k, v := range h {
				if s, _ := v.(string); strings.ContainsAny(s, "<>") {
					t.Errorf("effectHow.%s contient du balisage : %q", k, s)
				}
			}
		}
	}
}

// apply.changed : seulement les options dont la valeur EFFECTIVE a changé ; relaunch seulement pour un effet `launcher`.
func TestApplyChangedValeurEffective226(t *testing.T) {
	f := newFix226(t)
	// souris : la valeur par défaut est 1 → écrire 0 change la valeur effective
	r := f.apply(set226("CLAUDE_DISABLE_MOUSE", false), set226("TEAM_HIDE_IDLE", 45))
	ch := changed226(t, r)
	if ch["CLAUDE_DISABLE_MOUSE"] != "claude" || ch["TEAM_HIDE_IDLE"] != "immediate" || len(ch) != 2 {
		t.Errorf("changed = %v, attendu {CLAUDE_DISABLE_MOUSE: claude, TEAM_HIDE_IDLE: immediate}", ch)
	}
	if ap := r["apply"].(map[string]any); ap["relaunch"] != false {
		t.Errorf("relaunch = %v, attendu false (aucune option launcher)", ap["relaunch"])
	}
	// même valeur réécrite : rien n'a changé
	r = f.apply(set226("TEAM_HIDE_IDLE", 45), set226("CLAUDE_DISABLE_MOUSE", false))
	if ch := changed226(t, r); len(ch) != 0 {
		t.Errorf("valeurs inchangées : changed = %v, attendu vide", ch)
	}
	// option `launcher` : relaunch vrai
	r = f.apply(set226("GITHUB_DIR", filepathJoin226(t, "autres-projets")))
	if ch := changed226(t, r); ch["GITHUB_DIR"] != "launcher" {
		t.Errorf("changed = %v, attendu GITHUB_DIR: launcher", ch)
	}
	if ap := r["apply"].(map[string]any); ap["relaunch"] != true {
		t.Errorf("relaunch = %v, attendu true (option launcher)", ap["relaunch"])
	}
}

func filepathJoin226(t *testing.T, name string) string {
	return t.TempDir() + string(os.PathSeparator) + name
}

// Un commentaire seul (éditeur texte) ne change AUCUNE option : changed vide, relaunch faux, aucun projet concerné.
func TestApplyCommentaireSeul226(t *testing.T) {
	f := newFix226(t)
	f.apply(set226("TEAM_HIDE_IDLE", 40))
	f.a.openTabs.Store(f.proj, true)
	b, _ := os.ReadFile(core.ConfigPath())
	st, _ := os.Stat(core.ConfigPath())
	r := f.call("configSave", map[string]any{"kind": "config.conf", "content": string(b) + "# un simple commentaire\n", "mtime": st.ModTime().Format("2006-01-02T15:04:05.999999999Z07:00")})
	if r["saved"] != true {
		t.Fatalf("configSave : %v", r)
	}
	ap, _ := r["apply"].(map[string]any)
	if ap == nil {
		t.Fatalf("configSave sans `apply` : %v", r)
	}
	if ch := changed226(t, r); len(ch) != 0 || ap["relaunch"] != false {
		t.Errorf("commentaire seul : changed=%v relaunch=%v", ch, ap["relaunch"])
	}
	if p, _ := ap["projects"].([]any); len(p) != 0 {
		t.Errorf("commentaire seul : projects = %v, attendu vide", p)
	}
}

// apply.projects : projets OUVERTS seulement, avec ce qui les concerne ; un projet fermé n'est jamais listé ; aucune action
// automatique (le projet reste ouvert, rien n'est rafraîchi tout seul).
func TestApplyProjetsOuvertsConcernes226(t *testing.T) {
	f := newFix226(t)
	proj := func(r map[string]any) map[string][]string {
		out := map[string][]string{}
		p, _ := r["apply"].(map[string]any)["projects"].([]any)
		for _, it := range p {
			m := it.(map[string]any)
			var fr []string
			for _, x := range m["for"].([]any) {
				fr = append(fr, x.(string))
			}
			sort.Strings(fr)
			if m["path"] == "" || m["name"] == "" {
				t.Errorf("projet sans path / name : %v", m)
			}
			out[m["path"].(string)] = fr
		}
		return out
	}
	// projet fermé : jamais listé, même pour une option claude
	if p := proj(f.apply(set226("CLAUDE_DISABLE_MOUSE", false))); len(p) != 0 {
		t.Errorf("projet fermé listé : %v", p)
	}
	f.a.openTabs.Store(f.proj, true)
	r := f.apply(set226("CLAUDE_DISABLE_MOUSE", true), set226("CLAUDE_OPTIONS", "--verbose"))
	got := proj(r)
	if want := []string{"claude", "project"}; strings.Join(got[f.proj], ",") != strings.Join(want, ",") {
		t.Errorf("projects = %v, attendu %s : [claude project]", got, f.proj)
	}
	// option immédiate seule : aucun projet à rafraîchir
	if p := proj(f.apply(set226("TEAM_HIDE_IDLE", 77))); len(p) != 0 {
		t.Errorf("option immédiate : projects = %v, attendu vide", p)
	}
	if _, still := f.a.openTabs.Load(f.proj); !still {
		t.Error("le projet a été fermé / rafraîchi automatiquement (H6 : aucune action automatique)")
	}
}

// ConfigReloadNote retiré : l'ancienne note globale n'est plus dans la réponse (`note` remplacée par `apply`).
func TestAncienneNoteGlobaleRetiree226(t *testing.T) {
	f := newFix226(t)
	r := f.apply(set226("CLAUDE_DISABLE_MOUSE", false))
	if n, _ := r["note"].(string); strings.Contains(n, "relancer") && strings.Contains(n, "ouverts") {
		t.Errorf("note globale encore rendue : %q", n)
	}
	if r["apply"] == nil {
		t.Error("`apply` absent")
	}
}

// `note` (texte de prise en compte rendu par core) : conservé pour backends.yaml, hub et env.yaml (projet et launcher) tant que
// leurs pastilles n'existent pas ; pour config.conf il est retiré (remplacé par effect / apply). Décision LOT-4.2.
func TestNoteParTypeDeFichier226(t *testing.T) {
	f := newFix226(t)
	note := func(r map[string]any) string { s, _ := r["note"].(string); return s }
	mt := func(kind, proj string) any {
		return f.call("configModel", map[string]any{"kind": kind, "path": proj})["mtime"]
	}
	// config.conf : pas de note
	if r := f.apply(set226("TEAM_HIDE_IDLE", 55)); note(r) != "" || r["apply"] == nil {
		t.Errorf("config.conf : note = %q (attendu absente), apply = %v", note(r), r["apply"])
	}
	// env.yaml du launcher
	r := f.call("configApply", map[string]any{"kind": "env.yaml", "mtime": mt("env.yaml", ""), "changes": []any{set226("packages", []any{"ripgrep"})}})
	if r["saved"] != true || note(r) == "" {
		t.Errorf("env.yaml launcher : saved=%v note=%q, attendu une note", r["saved"], note(r))
	}
	// env.yaml d'un projet
	r = f.call("configApply", map[string]any{"kind": "env.yaml", "project": f.proj, "mtime": mt("env.yaml", f.proj), "changes": []any{set226("packages", []any{"jq"})}})
	if r["saved"] != true || note(r) == "" {
		t.Errorf("env.yaml projet : %v", r)
	}
	// hub
	r = f.call("configApply", map[string]any{"kind": "hub", "mtime": mt("hub", ""), "changes": []any{set226("hub_cache", "/x226")}})
	if r["saved"] != true || note(r) == "" {
		t.Errorf("hub : %v", r)
	}
	// backends.yaml (éditeur texte)
	b, _ := os.ReadFile(core.BackendsPath())
	r = f.call("configSave", map[string]any{"kind": "backends.yaml", "content": string(b) + "\n# 226\n", "mtime": mt("backends.yaml", "")})
	if r["saved"] != true || note(r) == "" {
		t.Errorf("backends.yaml : %v", r)
	}
	// config.conf via l'éditeur texte : toujours pas de note
	cb, _ := os.ReadFile(core.ConfigPath())
	r = f.call("configSave", map[string]any{"kind": "config.conf", "content": string(cb) + "# 226\n", "mtime": mt("config.conf", "")})
	if r["saved"] != true || note(r) != "" {
		t.Errorf("config.conf configSave : saved=%v note=%q, attendu pas de note", r["saved"], note(r))
	}
}
