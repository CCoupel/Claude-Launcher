package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// #148 (LOT-3.2) — projectSettings / projectApply selon contracts/desktop-dispatch.md,
// par Dispatch, sur des projets factices en dossiers temporaires (jamais de
// vrai projet ni de vrai claude). Un projet « ouvert » est un projTerm sans tmux.

const projEnvSample = `# environnement de alpha — ne pas retirer gh
image: docker.io/library/debian:12   # socle
packages:
  - ripgrep     # recherche
setup:
  - make deps
mounts:
  - {path: /data, ro: true}
`

type projFix struct {
	t    *testing.T
	a    *App
	proj string
}

func (f projFix) call(m string, args map[string]any) map[string]any {
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

func (f projFix) write(p, s string) {
	f.t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func newProjFix(t *testing.T) projFix {
	t.Helper()
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	f := projFix{t, a, ps[0]}
	f.write(core.BackendsPath(), "backends:\n  srv1:\n    type: docker\n    host: tcp://10.0.0.1:2375\n    shared: [/share]\n")
	f.write(core.EnvPath(f.proj), projEnvSample)
	f.write(filepath.Join(f.proj, ".claude-launcher", "env-template.yaml"), "packages: [jq]\nsetup: [tpl-setup]\n")
	f.write(core.LauncherEnvPath(), "packages: [gh]\n")
	return f
}

func (f projFix) open() {
	f.t.Helper()
	f.a.mu.Lock()
	f.a.terms[f.proj] = newProjTerm(f.a, core.Node{Path: f.proj, Key: "alpha", Name: "alpha"}, core.LaunchNormal)
	f.a.mu.Unlock()
}

// Forme du résultat et aperçu fusionné avec la provenance de chaque élément.
func TestProjectSettingsShapeAndProvenance(t *testing.T) {
	f := newProjFix(t)
	r := f.call("projectSettings", map[string]any{"path": f.proj})
	for _, k := range []string{"open", "assigned", "backend", "backends", "image", "images", "color", "colorAuto",
		"palette", "env", "envPath", "envMtime", "envExists", "templatePath", "hasTemplate", "merged", "problems"} {
		if _, ok := r[k]; !ok {
			t.Errorf("champ %q absent : %v", k, r)
		}
	}
	if r["open"] != false || r["envExists"] != true || r["hasTemplate"] != true || r["colorAuto"] != true {
		t.Errorf("états : %v", r)
	}
	if r["envPath"] != core.EnvPath(f.proj) || r["envMtime"] == "" {
		t.Errorf("fichier : %v %v", r["envPath"], r["envMtime"])
	}
	if r["image"] != "docker.io/library/debian:12" {
		t.Errorf("image : %v", r["image"])
	}
	env := r["env"].(map[string]any)
	if pk, _ := env["packages"].([]any); len(pk) != 1 || pk[0] != "ripgrep" { // le projet seul
		t.Errorf("env du projet seul : %v", env)
	}
	from := map[string]string{}
	for _, l := range r["merged"].([]any) {
		m := l.(map[string]any)
		from[strings.TrimSpace(m["text"].(string))] = m["from"].(string)
	}
	for text, want := range map[string]string{"- gh": "launcher", "- jq": "template", "- ripgrep": "project",
		"- make deps": "project", "- tpl-setup": "template", "packages:": ""} {
		if got, ok := from[text]; !ok || got != want {
			t.Errorf("provenance de %q = %q (présent %v), attendu %q\n%v", text, got, ok, want, from)
		}
	}
	// ordre du bootstrap : le launcher avant le template avant le projet (paquets)
	var order []string
	for _, l := range r["merged"].([]any) {
		m := l.(map[string]any)
		if s := m["text"].(string); strings.HasPrefix(s, "  - ") && (s == "  - gh" || s == "  - jq" || s == "  - ripgrep") {
			order = append(order, strings.TrimSpace(s))
		}
	}
	if strings.Join(order, ",") != "- gh,- jq,- ripgrep" {
		t.Errorf("ordre de fusion des paquets : %v", order)
	}
}

// Aperçu en direct : `changes` donne l'aperçu d'un env.yaml modifié, sans rien écrire.
func TestProjectSettingsLivePreviewWritesNothing(t *testing.T) {
	f := newProjFix(t)
	before, _ := os.ReadFile(core.EnvPath(f.proj))
	r := f.call("projectSettings", map[string]any{"path": f.proj, "changes": []any{
		map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"ripgrep", "fd"}}}})
	found := false
	for _, l := range r["merged"].([]any) {
		if strings.TrimSpace(l.(map[string]any)["text"].(string)) == "- fd" {
			found = l.(map[string]any)["from"] == "project"
		}
	}
	if !found {
		t.Errorf("« fd » absent de l'aperçu fusionné : %v", r["merged"])
	}
	if after, _ := os.ReadFile(core.EnvPath(f.proj)); string(after) != string(before) {
		t.Error("l'aperçu a écrit env.yaml")
	}
}

// critical : env.yaml modifié sans perdre commentaires ni valeurs non touchées ;
// conflit de date ; invalide refusé sans rien écrire (ni couleur ni backend).
func TestProjectApplyEnvKeepsCommentsConflictInvalid(t *testing.T) {
	f := newProjFix(t)
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	mt := s["envMtime"]
	ch := []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"ripgrep", "fd"}}}
	r := f.call("projectApply", map[string]any{"path": f.proj, "env": map[string]any{"mtime": mt, "changes": ch}})
	if r["ok"] != true || r["env"].(map[string]any)["saved"] != true {
		t.Fatalf("env : %v", r)
	}
	b, _ := os.ReadFile(core.EnvPath(f.proj))
	for _, want := range []string{"# environnement de alpha — ne pas retirer gh", "# socle", "# recherche",
		"docker.io/library/debian:12", "make deps", "/data", "fd"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("%q perdu :\n%s", want, b)
		}
	}
	if _, err := os.Stat(core.EnvPath(f.proj) + ".bak"); err != nil {
		t.Error("copie .bak absente")
	}
	// conflit : ancienne date
	r = f.call("projectApply", map[string]any{"path": f.proj, "color": "52",
		"env": map[string]any{"mtime": "2000-01-01T00:00:00Z", "changes": ch}})
	if r["ok"] != false || r["env"].(map[string]any)["conflict"] != true {
		t.Errorf("conflit non signalé : %v", r)
	}
	// invalide : arrête tout avant toute autre écriture (la couleur n'est pas écrite)
	cur := f.call("projectSettings", map[string]any{"path": f.proj})
	bad := []any{map[string]any{"op": "set", "at": []string{"mounts"}, "value": "pas une liste"}}
	r = f.call("projectApply", map[string]any{"path": f.proj, "color": "52",
		"env": map[string]any{"mtime": cur["envMtime"], "changes": bad}})
	if r["ok"] != false || r["env"].(map[string]any)["invalid"] != true {
		t.Errorf("invalide accepté : %v", r)
	}
	if conf, _ := os.ReadFile(core.ConfigPath()); strings.Contains(string(conf), "52") && strings.Contains(string(conf), "PROJECT_COLORS[") {
		t.Errorf("couleur écrite malgré l'échec de env : %s", conf)
	}
}

// Projet ouvert : backend et image refusés avec le message du contrat, rien écrit ;
// couleur et env restent permis.
func TestProjectApplyRefusedWhenOpen(t *testing.T) {
	f := newProjFix(t)
	f.open()
	if s := f.call("projectSettings", map[string]any{"path": f.proj}); s["open"] != true {
		t.Fatalf("open : %v", s["open"])
	}
	assignBefore := core.ReadAssignments()
	for name, args := range map[string]map[string]any{
		"backend": {"path": f.proj, "backend": "srv1", "image": "docker.io/library/debian:12"},
		"image":   {"path": f.proj, "image": "docker.io/library/debian:13"},
	} {
		r := f.call("projectApply", args)
		if r["ok"] != false || !strings.Contains(r["msg"].(string), "projet ouvert") {
			t.Errorf("%s accepté sur un projet ouvert : %v", name, r)
		}
	}
	if len(core.ReadAssignments()) != len(assignBefore) {
		t.Error("affectation modifiée malgré le refus")
	}
	if r := f.call("projectApply", map[string]any{"path": f.proj, "color": "52"}); r["ok"] != true {
		t.Errorf("couleur refusée sur un projet ouvert : %v", r)
	}
}

// Projet fermé : backend + image enregistrés ; contrôles : backend inconnu, image
// absente, « host » sans image ; rien d'écrit en cas de refus.
func TestProjectApplyBackendClosed(t *testing.T) {
	f := newProjFix(t)
	for name, args := range map[string]map[string]any{
		"backend inconnu": {"path": f.proj, "backend": "nulle-part", "image": "docker.io/library/debian:12"},
	} {
		if r := f.call("projectApply", args); r["ok"] != false || r["msg"] == "" {
			t.Errorf("%s : %v", name, r)
		}
	}
	r := f.call("projectApply", map[string]any{"path": f.proj, "backend": "srv1", "image": "docker.io/library/debian:12"})
	if r["ok"] != true {
		t.Fatalf("backend : %v", r)
	}
	if got := core.ReadAssignments()[f.proj]; got != "srv1" {
		t.Errorf("affectation = %q", got)
	}
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	if s["backend"] != "srv1" || s["assigned"] != true {
		t.Errorf("relu : %v", s)
	}
	if r := f.call("projectApply", map[string]any{"path": f.proj, "backend": "host"}); r["ok"] != true {
		t.Errorf("host : %v", r)
	}
}

// Couleur : indice 0–255 ; vide = automatique ; seule la clé du projet est écrite
// dans config.conf, le reste (commentaires compris) est conservé ; valeur hors
// plage refusée sans écrire.
func TestProjectApplyColor(t *testing.T) {
	f := newProjFix(t)
	f.write(core.ConfigPath(), "# ma configuration\nPROJECT_BASE=\"/x\"\ndeclare -A PROJECT_COLORS=()  # couleurs\n")
	key := filepath.Base(f.proj)
	for _, bad := range []string{"256", "-1", "rouge", "17;rm"} {
		before, _ := os.ReadFile(core.ConfigPath())
		if r := f.call("projectApply", map[string]any{"path": f.proj, "color": bad}); r["ok"] != false {
			t.Errorf("couleur %q acceptée : %v", bad, r)
		}
		if after, _ := os.ReadFile(core.ConfigPath()); string(after) != string(before) {
			t.Errorf("config.conf modifié par la couleur invalide %q", bad)
		}
	}
	if r := f.call("projectApply", map[string]any{"path": f.proj, "color": "52"}); r["ok"] != true {
		t.Fatalf("couleur : %v", r)
	}
	b, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(b), "# ma configuration") || !strings.Contains(string(b), "PROJECT_BASE=\"/x\"") ||
		!strings.Contains(string(b), key) || !strings.Contains(string(b), "52") {
		t.Errorf("config.conf : %s", b)
	}
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	if s["color"] != "52" || s["colorAuto"] != false {
		t.Errorf("couleur relue : %v %v", s["color"], s["colorAuto"])
	}
	// vide : retour à l'automatique, clé retirée, reste intact
	if r := f.call("projectApply", map[string]any{"path": f.proj, "color": ""}); r["ok"] != true {
		t.Fatalf("couleur auto : %v", r)
	}
	b, _ = os.ReadFile(core.ConfigPath())
	if strings.Contains(string(b), "PROJECT_COLORS["+key) || !strings.Contains(string(b), "# ma configuration") {
		t.Errorf("clé non retirée ou fichier abîmé : %s", b)
	}
	if s := f.call("projectSettings", map[string]any{"path": f.proj}); s["colorAuto"] != true {
		t.Errorf("colorAuto : %v", s["colorAuto"])
	}
}
