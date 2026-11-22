//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"claude-launcher-ng2/core"
)

func TestProjectMoveDispatch216(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	base := a.conf().Roots[0]
	src := ps[0] // <base>/TEST/alpha
	info := dispatchJSON(t, a, "projectMoveInfo", map[string]any{"path": src})
	if info["base"] != base || info["category"] != "TEST" || info["name"] != "alpha" || info["key"] == "" {
		t.Fatalf("projectMoveInfo = %v", info)
	}
	if bl, _ := info["blockers"].([]any); len(bl) != 0 {
		t.Errorf("blockers = %v", bl)
	}
	// refus de chemin : {ok:false, field}
	v := dispatchJSON(t, a, "projectMove", map[string]any{"path": src, "base": base, "category": "../x", "name": "beta"})
	if v["ok"] != false || v["field"] != "category" {
		t.Errorf("traversée = %v", v)
	}
	v = dispatchJSON(t, a, "projectMove", map[string]any{"path": src, "base": base, "category": "AUTRE", "newCategory": true, "name": "beta"})
	dst := filepath.Join(base, "AUTRE", "beta")
	if v["ok"] != true || v["path"] != dst || v["key"] == "" {
		t.Fatalf("projectMove = %v", v)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		t.Error("projet absent de la destination")
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("source restée")
	}
	if a.Snapshot().Selected != dst {
		t.Errorf("sélection = %q, attendu %q", a.Snapshot().Selected, dst)
	}
	// destination existante : refus
	v = dispatchJSON(t, a, "projectMove", map[string]any{"path": dst, "base": base, "category": "AUTRE", "name": "beta"})
	if v["ok"] == true {
		t.Errorf("déplacement sur soi-même accepté : %v", v)
	}
	// chemin inconnu
	if _, err := a.Dispatch("projectMoveInfo", []byte(`{"path":"/etc"}`)); err == nil {
		t.Error("projet inconnu accepté")
	}
}

// projectMoveInfo avec destination, projectMoveResume / projectMoveAbort par id (contrat ed1faa0).
func TestProjectMoveInfoDestEtResumeAbort216(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	base := a.conf().Roots[0]
	src := ps[0]
	v := dispatchJSON(t, a, "projectMoveInfo", map[string]any{"path": src, "base": base, "category": "AUTRE", "newCategory": true, "name": "beta"})
	d, _ := v["dest"].(map[string]any)
	if d == nil || d["ok"] != true || d["path"] != filepath.Join(base, "AUTRE", "beta") || d["keyChanged"] != true || d["crossDevice"] != false {
		t.Fatalf("dest = %v", v["dest"])
	}
	if _, ok := d["impacts"].([]any); !ok {
		t.Errorf("impacts = %v", d["impacts"])
	}
	// reprise / abandon : entrée du journal fabriquée en échec injecté côté core (même dossier de config)
	env := a.moveEnv()
	env.Fail = func(s string, undo bool) error {
		if s == "history" && !undo {
			return os.ErrPermission
		}
		if s == "dir" && undo {
			return os.ErrPermission
		}
		return nil
	}
	dst := filepath.Join(base, "AUTRE", "beta")
	if res := coreMove(env, src, base, "AUTRE", "beta"); res != "partial" {
		t.Fatalf("partial attendu : %s", res)
	}
	info := dispatchJSON(t, a, "projectMoveInfo", map[string]any{"path": dst})
	pend, _ := info["pending"].(map[string]any)
	if pend == nil || pend["id"] == "" || pend["step"] != "assign" {
		t.Fatalf("pending = %v", info["pending"])
	}
	ab := dispatchJSON(t, a, "projectMoveAbort", map[string]any{"id": pend["id"]})
	if ab["ok"] != true || ab["rolledBack"] != true || ab["path"] != src {
		t.Fatalf("abandon = %v", ab)
	}
	if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
		t.Error("projet non remis à son chemin d'origine")
	}
	if v := dispatchJSON(t, a, "projectMoveResume", map[string]any{"id": "inconnu"}); v["ok"] == true {
		t.Errorf("id inconnu accepté : %v", v)
	}
}

func coreMove(env core.MoveEnv, path, base, cat, name string) string {
	return core.MoveProject(env, path, core.MoveTarget{Base: base, Category: cat, NewCategory: true, Name: name}).State
}
