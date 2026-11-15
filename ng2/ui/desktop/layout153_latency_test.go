package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// GATE LOT-5 (#153) : la bascule de l'organisation (projectApply avec layout, ou LAYOUT_MODE enregistré depuis
// les Réglages) remet le projet ouvert en page tout de suite — pas au prochain tour du surveillant, qui peut
// durer plusieurs secondes sur un partage réseau (tour lent : lecture de la team, de tmux).
func TestLayoutToggleRelayoutsImmediately(t *testing.T) {
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

	leaderW := func() float64 { // largeur du leader dans la mise en page publiée, 0 : inconnue
		l := f.a.term(f.path).lastLayout()
		if l == nil || l.Cols == 0 {
			return 0
		}
		for _, p := range l.Panes {
			if p.ID == leader {
				return float64(p.W) * 100 / float64(l.Cols)
			}
		}
		return 0
	}
	waitW := func(what string, within time.Duration, pred func(w float64) bool) time.Duration {
		t.Helper()
		t0 := time.Now()
		for time.Since(t0) < within {
			if w := leaderW(); w > 0 && pred(w) {
				return time.Since(t0)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s : leader à %.0f %% après %v", what, leaderW(), within)
		return 0
	}
	waitW("top initial", 20*time.Second, func(w float64) bool { return w > 90 })
	time.Sleep(1500 * time.Millisecond) // le surveillant est au repos
	// partage réseau (Windows → \\wsl.localhost) : chaque tour lent du surveillant prend plusieurs secondes
	tickSlow.Store(int64(4 * time.Second))
	defer tickSlow.Store(0)

	args := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	// 1. bouton de bascule d'un projet
	if res, err := f.a.Dispatch("projectApply", args(map[string]any{"path": f.path, "layout": "left"})); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("projectApply left : %v %v", res, err)
	}
	d1 := waitW("bascule du projet vers left", 3*time.Second, func(w float64) bool { return w > 30 && w < 60 })
	t.Logf("bascule projectApply → leader à gauche : %v", d1)
	if d1 > time.Second {
		t.Errorf("bascule du projet : %v, attendu moins d'une seconde", d1)
	}
	time.Sleep(1500 * time.Millisecond)
	if res, _ := f.a.Dispatch("projectApply", args(map[string]any{"path": f.path, "layout": ""})); res.(map[string]any)["ok"] != true {
		t.Fatalf("projectApply top : %v", res)
	}
	d2 := waitW("retour vers top", 3*time.Second, func(w float64) bool { return w > 90 })
	t.Logf("bascule retour → leader en haut : %v", d2)
	if d2 > time.Second {
		t.Errorf("retour : %v, attendu moins d'une seconde", d2)
	}
	// 2. LAYOUT_MODE enregistré depuis les Réglages (config.conf)
	time.Sleep(1500 * time.Millisecond)
	mt := func() string { st, _ := os.Stat(core.ConfigPath()); return st.ModTime().Format(time.RFC3339Nano) }
	if res, err := f.a.Dispatch("configApply", args(map[string]any{"kind": "config.conf", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"LAYOUT_MODE"}, "value": "left"}}})); err != nil || res.(map[string]any)["saved"] != true {
		t.Fatalf("configApply LAYOUT_MODE : %v %v", res, err)
	}
	d3 := waitW("LAYOUT_MODE=left des Réglages", 3*time.Second, func(w float64) bool { return w > 30 && w < 60 })
	t.Logf("LAYOUT_MODE enregistré → leader à gauche : %v", d3)
	if d3 > time.Second {
		t.Errorf("LAYOUT_MODE : %v, attendu moins d'une seconde", d3)
	}
}
