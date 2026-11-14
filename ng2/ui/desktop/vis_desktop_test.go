package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #136 — teammates cachés dans l'interface native : bascule par teamToggle,
// pane qui quitte la mise en page puis recapturé à son retour, carte Team par
// teammate, refus (actif, leader, lecture seule). Faux claude, tmux dédié.
func TestTeamToggleHidesPane(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-vis-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")

	var r recorder
	a := newApp(t, r.emit)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(120, 40)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))
	leader := pt.lastLayout().Leader

	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	win := "=" + hostSession(ps[0]) + ":"
	var mates []string
	for i := 0; i < 2; i++ {
		mates = append(mates, tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sh -c 'echo mate-"+strconv.Itoa(i)+"; exec cat'"))
	}
	actif, calme := mates[0], mates[1]
	cfg, _ := json.Marshal(core.TeamConfig{Members: []core.TeamMember{
		{Name: "team-lead", Cwd: core.ClaudePath(ps[0])},
		{Name: "dev-a", AgentType: "dev-a", PaneID: actif, IsActive: true},
		{Name: "dev-b", AgentType: "dev-b", PaneID: calme},
	}})
	d := filepath.Join(core.ClaudeDir(), "teams", "t1")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "config.json"), cfg, 0o644)
	panesIn := func(n int) func(map[string]any) bool {
		return func(e map[string]any) bool { return len(e["layout"].(map[string]any)["panes"].([]any)) == n }
	}
	r.wait(t, 15*time.Second, "3 panes", isTerm(ps[0], "layout", panesIn(3)))

	toggle := func(pane string) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"path": ps[0], "pane": pane})
		res, err := a.Dispatch("teamToggle", raw)
		if err != nil {
			t.Fatal(err)
		}
		return res.(map[string]any)
	}
	for _, p := range []string{actif, leader} {
		if res := toggle(p); res["ok"] != false || res["msg"] == "" {
			t.Fatalf("bascule permise sur %s : %v", p, res)
		}
	}
	// la carte Team du projet sélectionné est renvoyée tout de suite
	selRaw, _ := json.Marshal(map[string]string{"path": ps[0]})
	if _, err := a.Dispatch("select", selRaw); err != nil {
		t.Fatal(err)
	}
	// laisser passer un tour du watcher (team lue, recensement fait)
	time.Sleep(2500 * time.Millisecond)
	cardHidden := func(from int, pane string, hidden bool) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, e := range r.evs[from:] {
			if e["_name"] != "cards" {
				continue
			}
			tm, _ := e["team"].([]any)
			for _, x := range tm {
				if m := x.(map[string]any); m["pane"] == pane && m["hidden"] == hidden {
					return true
				}
			}
		}
		return false
	}
	// inactif : caché, il quitte la mise en page mais tourne toujours — sans
	// attendre un tour du watcher : géométrie et carte à jour au retour de l'appel
	pid := tm("display-message", "-p", "-t", calme, "#{pane_pid}")
	n0 := len(r.evs)
	// pas de tick du watcher : aucune recherche de team (une par tour lent) ni
	// lecture de la fenêtre par le tour ; seule la bascule elle-même travaille
	f0 := atomic.LoadInt32(&pt.finds)
	t0 := time.Now()
	if res := toggle(calme); res["ok"] != true {
		t.Fatalf("bascule refusée : %v", res)
	}
	if df := atomic.LoadInt32(&pt.finds) - f0; df > 1 { // au plus un tour du watcher glissé pendant la mesure
		t.Errorf("%d recherches de team pendant la bascule : un tour complet a été attendu", df)
	}
	if d := time.Since(t0); d > 2*time.Second { // borne large : machine chargée (CI)
		t.Errorf("bascule en %v : lente", d)
	}
	if l := pt.lastLayout(); l == nil || len(l.Panes) != 2 {
		t.Fatalf("géométrie non publiée au retour de l'appel : %+v", l)
	}
	if !cardHidden(n0, calme, true) {
		t.Fatal("carte Team non renvoyée (caché) au retour de l'appel")
	}
	r.wait(t, 10*time.Second, "2 panes", isTerm(ps[0], "layout", panesIn(2)))
	if exec.Command("kill", "-0", pid).Run() != nil {
		t.Fatal("processus du pane caché tué")
	}
	// carte Team : une ligne par instance, caché signalé
	var row *TeamRow
	for _, tr := range a.readTeam(ps[0]) {
		tr := tr
		if tr.Pane == calme {
			row = &tr
		}
	}
	if row == nil || !row.Hidden || !row.Started || row.Name != "dev-b" || row.Pinned {
		t.Fatalf("ligne caché : %+v", row)
	}
	// retour : visible fixé, terminal recapturé (Reset)
	nev := len(r.evs)
	t1 := time.Now()
	if res := toggle(calme); res["ok"] != true {
		t.Fatalf("second basculement : %v", res)
	}
	if d := time.Since(t1); d > 2*time.Second {
		t.Errorf("retour en %v : lent", d)
	}
	if l := pt.lastLayout(); l == nil || len(l.Panes) != 3 {
		t.Fatalf("géométrie non publiée au retour de l'appel : %+v", l)
	}
	if !cardHidden(nev, calme, false) {
		t.Fatal("carte Team non renvoyée (visible) au retour de l'appel")
	}
	r.wait(t, 10*time.Second, "3 panes au retour", isTerm(ps[0], "layout", panesIn(3)))
	deadline := time.Now().Add(10 * time.Second)
	recaptured := false
	for !recaptured && time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.evs[nev:] {
			if e["_name"] == "term" && e["kind"] == "out" {
				for _, c := range e["chunks"].([]any) {
					if m := c.(map[string]any); m["pane"] == calme && m["reset"] == true {
						recaptured = true
					}
				}
			}
		}
		r.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	if !recaptured {
		t.Fatal("pane ramené non recapturé")
	}
	for _, tr := range a.readTeam(ps[0]) {
		if tr.Pane == calme && (tr.Hidden || !tr.Pinned) {
			t.Fatalf("ligne après retour : %+v", tr)
		}
	}
	// lecture seule : refus
	pt.mu.Lock()
	pt.readOnly = true
	pt.mu.Unlock()
	if res := toggle(calme); res["ok"] != false || !strings.Contains(fmt.Sprint(res["msg"]), "lecture seule") {
		t.Fatalf("lecture seule : %v", res)
	}
}
