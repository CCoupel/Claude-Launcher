//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"testing"
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
