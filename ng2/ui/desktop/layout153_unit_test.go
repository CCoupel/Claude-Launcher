package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #153 : organisation du projet dans projectSettings / projectApply.
func TestProjectLayoutRoundTrip(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	args := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	settings := func() map[string]any {
		res, err := a.Dispatch("projectSettings", args(map[string]any{"path": ps[0]}))
		if err != nil {
			t.Fatal(err)
		}
		return res.(map[string]any)
	}
	s := settings()
	if s["layout"] != "top" || s["layoutAuto"] != true {
		t.Errorf("défaut : layout=%v auto=%v, attendu top, auto", s["layout"], s["layoutAuto"])
	}
	if l, _ := s["layouts"].([]any); len(l) != 2 {
		t.Errorf("layouts : %v", s["layouts"])
	}
	res, err := a.Dispatch("projectApply", args(map[string]any{"path": ps[0], "layout": "left"}))
	if err != nil || res.(map[string]any)["ok"] != true || res.(map[string]any)["colorSaved"] != true {
		t.Fatalf("projectApply left : %v %v", res, err)
	}
	if b, _ := os.ReadFile(core.ConfigPath()); !strings.Contains(string(b), `PROJECT_LAYOUTS`) || !strings.Contains(string(b), `"alpha"]="left"`) {
		t.Errorf("config.conf : %s", b)
	}
	if s = settings(); s["layout"] != "left" || s["layoutAuto"] != false {
		t.Errorf("après left : layout=%v auto=%v", s["layout"], s["layoutAuto"])
	}
	if res, _ = a.Dispatch("projectApply", args(map[string]any{"path": ps[0], "layout": "diagonal"})); res.(map[string]any)["ok"] != false {
		t.Errorf("valeur inconnue acceptée : %v", res)
	}
	if res, _ = a.Dispatch("projectApply", args(map[string]any{"path": ps[0], "layout": ""})); res.(map[string]any)["ok"] != true {
		t.Fatalf("retrait : %v", res)
	}
	if s = settings(); s["layout"] != "top" || s["layoutAuto"] != true {
		t.Errorf("après retrait : layout=%v auto=%v", s["layout"], s["layoutAuto"])
	}
	// couleur et organisation dans la même passe : un seul enregistrement, les deux écrits
	res, _ = a.Dispatch("projectApply", args(map[string]any{"path": ps[0], "color": "52", "layout": "left"}))
	if res.(map[string]any)["ok"] != true {
		t.Fatalf("couleur + organisation : %v", res)
	}
	b, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(b), `PROJECT_COLORS`) || !strings.Contains(string(b), `PROJECT_LAYOUTS`) {
		t.Errorf("config.conf : %s", b)
	}
}

// config.conf modifié hors de l'interface : le surveillant de la fenêtre native relit la configuration et remet
// la team en page selon la nouvelle organisation, sans relance.
func TestNativeWatcherReappliesOnConfigChange(t *testing.T) {
	f := openFakeProject(t)
	win := "=" + hostSession(f.path) + ":"
	for i := 0; i < 3; i++ {
		f.tmux("split-window", "-d", "-t", win, "sh -c 'echo teammate; exec cat'")
	}
	ids := strings.Fields(f.tmux("list-panes", "-t", win, "-F", "#{pane_id}"))
	leader := ids[0]
	members := []core.TeamMember{{Name: "team-lead", Cwd: core.ClaudePath(f.path)}}
	for _, id := range ids[1:] {
		members = append(members, core.TeamMember{Name: "agent-" + id[1:], PaneID: id})
	}
	d := core.ClaudeDir() + "/teams/t1"
	_ = os.MkdirAll(d, 0o755)
	b, _ := json.Marshal(core.TeamConfig{Members: members})
	_ = os.WriteFile(d+"/config.json", b, 0o644)

	leaderRect := func() (x, w, cols float64, ok bool) {
		l := f.a.term(f.path).lastLayout()
		if l == nil {
			return
		}
		for _, p := range l.Panes {
			if p.ID == leader {
				return float64(p.X), float64(p.W), float64(l.Cols), true
			}
		}
		return
	}
	waitLeader := func(what string, pred func(x, w, cols float64) bool) {
		t.Helper()
		end := time.Now().Add(20 * time.Second)
		for time.Now().Before(end) {
			if x, w, cols, ok := leaderRect(); ok && cols > 0 && pred(x, w, cols) {
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
		x, w, cols, _ := leaderRect()
		t.Fatalf("%s : leader x=%v w=%v sur %v colonnes", what, x, w, cols)
	}
	waitLeader("top", func(x, w, cols float64) bool { return w >= cols-1 }) // leader sur toute la largeur (ligne du haut)
	time.Sleep(1100 * time.Millisecond)
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=left\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitLeader("left", func(x, w, cols float64) bool { return x == 0 && w > cols*0.3 && w < cols*0.6 })
}
