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
	settle(t) // le surveillant est au repos
	// partage réseau (Windows → \\wsl.localhost) : chaque tour lent du surveillant prend plusieurs secondes
	tickSlow.Store(int64(2 * time.Second))
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
	settleSlow(t, 2*time.Second)
	if res, _ := f.a.Dispatch("projectApply", args(map[string]any{"path": f.path, "layout": ""})); res.(map[string]any)["ok"] != true {
		t.Fatalf("projectApply top : %v", res)
	}
	d2 := waitW("retour vers top", 3*time.Second, func(w float64) bool { return w > 90 })
	t.Logf("bascule retour → leader en haut : %v", d2)
	if d2 > time.Second {
		t.Errorf("retour : %v, attendu moins d'une seconde", d2)
	}
	// 2. LAYOUT_MODE enregistré depuis les Réglages (config.conf)
	settleSlow(t, 2*time.Second)
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

// fakeTeamProject : projet factice ouvert avec leader + 3 teammates et sa team ; leaderW rend la largeur du leader (%).
func fakeTeamProject(t *testing.T) (f fakeProj, leaderW func() float64) {
	t.Helper()
	f = openFakeProject(t)
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
	leaderW = func() float64 {
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
	return f, leaderW
}

func waitUntil(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s : non atteint en %v", what, d)
}

// Bascule PENDANT un tour lent du surveillant : une seule remise en page, et l'organisation finale est la
// nouvelle (le tour ne ramène pas l'ancienne valeur).
func TestToggleDuringSlowTickLaysOutOnceAndKeepsNewOrganisation(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	tickSlow.Store(int64(1200 * time.Millisecond)) // le prochain tour lent dure 1,2 s
	defer tickSlow.Store(0)
	waitTicksStarted(t) // un tour lent est en cours : sa configuration est déjà lue
	before := arrangeCalls.Load()
	args, _ := json.Marshal(map[string]any{"path": f.path, "layout": "left"})
	if res, err := f.a.Dispatch("projectApply", args); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("projectApply : %v %v", res, err)
	}
	waitUntil(t, "left", 3*time.Second, func() bool { w := leaderW(); return w > 30 && w < 60 })
	settleTicks(t, 2) // plusieurs tours lents complets
	if w := leaderW(); w < 30 || w > 60 {
		t.Errorf("organisation finale : leader à %.0f %%, attendu la nouvelle (left)", w)
	}
	if n := arrangeCalls.Load() - before; n != 1 {
		t.Errorf("%d remises en page pour une bascule, attendu 1", n)
	}
}

// Lecture de la fenêtre en échec au moment de la bascule : rien n'est perdu, le tour suivant remet en page.
func TestToggleWithUnreadableWindowIsRetried(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	readWinFail.Store(true)
	args, _ := json.Marshal(map[string]any{"path": f.path, "layout": "left"})
	if res, err := f.a.Dispatch("projectApply", args); err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("projectApply : %v %v", res, err)
	}
	settleTicks(t, 0) // relayoutNow a échoué (fenêtre illisible)
	time.Sleep(100 * time.Millisecond)
	if w := leaderW(); w < 90 {
		t.Fatalf("mise en page appliquée malgré une fenêtre illisible : leader à %.0f %%", w)
	}
	readWinFail.Store(false)
	waitUntil(t, "mise en page retentée au tour suivant", 5*time.Second, func() bool { w := leaderW(); return w > 30 && w < 60 })
}

// Sous Windows chaque aller-retour sur le canal tmux coûte plusieurs centaines de ms (wsl.exe, kubectl exec) :
// la bascule en consomme au plus deux (lecture de la fenêtre ; mise en page + relecture envoyées d'un coup).
func TestToggleUsesAtMostTwoTmuxRoundTrips(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	for _, layout := range []string{"left", "", "left"} {
		settle(t)
		lastRelayoutRounds.Store(0)
		args, _ := json.Marshal(map[string]any{"path": f.path, "layout": layout})
		if res, err := f.a.Dispatch("projectApply", args); err != nil || res.(map[string]any)["ok"] != true {
			t.Fatalf("projectApply %q : %v %v", layout, res, err)
		}
		rounds := lastRelayoutRounds.Load()
		t.Logf("bascule vers %q : %d aller(s)-retour(s) tmux dans la remise en page", layout, rounds)
		if rounds < 1 || rounds > 2 {
			t.Errorf("bascule vers %q : %d allers-retours tmux, attendu 1 ou 2", layout, rounds)
		}
	}
}

// La mise en page part AVANT la persistance : config.conf est écrit ensuite (ici retardé de 3 s) ; pendant ce
// temps projectSettings annonce déjà la nouvelle organisation.
func TestLayoutAppliedBeforeConfigIsWritten(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	persistDelay.Store(int64(1500 * time.Millisecond))
	defer persistDelay.Store(0)
	t0 := time.Now()
	args, _ := json.Marshal(map[string]any{"path": f.path, "layout": "left"})
	replied := make(chan map[string]any, 1)
	go func() { res, _ := f.a.Dispatch("projectApply", args); replied <- res.(map[string]any) }() // répond après l'écriture
	waitUntil(t, "left", 2*time.Second, func() bool { w := leaderW(); return w > 30 && w < 60 })
	if d := time.Since(t0); d > time.Second {
		t.Errorf("mise en page après %v, attendu moins d'une seconde malgré l'écriture retardée", d)
	}
	if b, _ := os.ReadFile(core.ConfigPath()); strings.Contains(string(b), "PROJECT_LAYOUTS") {
		t.Error("config.conf déjà écrit : la mise en page n'a pas précédé la persistance")
	}
	sa, _ := json.Marshal(map[string]any{"path": f.path})
	res, err := f.a.Dispatch("projectSettings", sa)
	if err != nil || res.(map[string]any)["layout"] != "left" || res.(map[string]any)["layoutAuto"] != false {
		t.Errorf("projectSettings pendant l'écriture : %v %v, attendu layout=left explicite", res, err)
	}
	select {
	case r := <-replied:
		if r["ok"] != true || r["colorSaved"] != true {
			t.Errorf("réponse après l'écriture : %v", r)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("projectApply ne répond pas après l'écriture de config.conf")
	}
	if b, _ := os.ReadFile(core.ConfigPath()); !strings.Contains(string(b), `"left"`) {
		t.Errorf("config.conf non écrit : %s", b)
	}
}

// Écriture refusée : retour à l'organisation précédente et message.
func TestLayoutRolledBackWhenConfigCannotBeWritten(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	// config.conf illisible (un dossier à sa place) : l'écriture échoue, portable
	if err := os.MkdirAll(core.ConfigPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": f.path, "layout": "left"})
	res, err := f.a.Dispatch("projectApply", args)
	if err != nil || res.(map[string]any)["ok"] != false || !strings.Contains(res.(map[string]any)["msg"].(string), "non enregistrée") {
		t.Fatalf("projectApply : %v %v, attendu {ok:false, msg « … non enregistrée … »}", res, err)
	}
	waitUntil(t, "retour à top", 8*time.Second, func() bool { return leaderW() > 90 })
}

// Cycle 3 de revue : clics rapides — dernier clic gagnant, une seule écriture finale, aucun retour visible.
func TestRapidTogglesLastClickWins(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	persistDelay.Store(int64(600 * time.Millisecond)) // chaque écriture dure 0,6 s
	defer persistDelay.Store(0)
	layoutWrites.Store(0)
	isLeft := func() bool { w := leaderW(); return w > 30 && w < 60 }
	args := func(l string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"path": f.path, "layout": l})
		return b
	}
	var replies [3]map[string]any
	done := make(chan int, 3)
	click := func(i int, l string) {
		go func() {
			res, err := f.a.Dispatch("projectApply", args(l))
			if err != nil {
				t.Errorf("clic %d : %v", i, err)
			}
			replies[i], _ = res.(map[string]any)
			done <- i
		}()
	}
	click(0, "left")
	time.Sleep(80 * time.Millisecond)
	click(1, "top")
	time.Sleep(80 * time.Millisecond)
	click(2, "left")
	// dès que le 3e clic est affiché, l'organisation ne doit plus jamais revenir en arrière
	waitUntil(t, "left après le 3e clic", 3*time.Second, isLeft)
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if !isLeft() {
			t.Fatalf("retour visible à l'ancienne organisation (leader à %.0f %%) après le dernier clic", leaderW())
		}
		time.Sleep(15 * time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Fatal("un projectApply ne répond pas")
		}
	}
	for i, r := range replies {
		if r["ok"] != true {
			t.Errorf("réponse du clic %d : %v (aucun clic ne doit échouer : conflit de date, etc.)", i+1, r)
		}
	}
	if b, _ := os.ReadFile(core.ConfigPath()); !strings.Contains(string(b), `"alpha"]="left"`) {
		t.Errorf("config.conf final : %s, attendu la dernière valeur (left)", b)
	}
	if n := layoutWrites.Load(); n > 2 {
		t.Errorf("%d écritures de config.conf, attendu 2 au plus (l'écriture en cours + la finale)", n)
	}
	s, _ := f.a.Dispatch("projectSettings", args(""))
	_ = s
	res, _ := f.a.Dispatch("projectSettings", func() json.RawMessage { b, _ := json.Marshal(map[string]any{"path": f.path}); return b }())
	if m := res.(map[string]any); m["layout"] != "left" || m["layoutAuto"] != false {
		t.Errorf("projectSettings final : %v", m)
	}
}

// Échec de TRANSPORT (wsl.exe / kubectl exec) : pas un refus de tmux — ni repli sur « top », ni signature fixée ;
// la mise en page est retentée au tour suivant.
func TestTransportFailureIsNotATmuxRefusal(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	batchFail.Store(true)
	args, _ := json.Marshal(map[string]any{"path": f.path, "layout": "left"})
	_, _ = f.a.Dispatch("projectApply", args)
	time.Sleep(100 * time.Millisecond)
	if m := f.a.Snapshot().Msg; strings.Contains(m, "refusé par tmux") {
		t.Errorf("échec de transport pris pour un refus de tmux : %q", m)
	}
	if sig := f.a.term(f.path).watch.layoutSig; strings.HasPrefix(sig, "left") {
		t.Errorf("layoutSig fixée (%q) malgré l'échec de transport", sig)
	}
	batchFail.Store(false)
	waitUntil(t, "mise en page retentée", 6*time.Second, isLeftOf(leaderW))
}

func isLeftOf(leaderW func() float64) func() bool {
	return func() bool { w := leaderW(); return w > 30 && w < 60 }
}

// Une seule photo de la configuration par mise en page : la signature fixée est celle de l'organisation réellement
// appliquée, même si la demande change entre le calcul de la signature et celui du plan.
func TestRelayoutUsesOneConfigSnapshot(t *testing.T) {
	f, leaderW := fakeTeamProject(t)
	waitUntil(t, "top initial", 20*time.Second, func() bool { return leaderW() > 90 })
	settle(t)
	base := filepath.Base(f.path)
	relayoutMid = func() { f.a.setLayoutOverride(base, "top") } // la demande change entre la signature et le plan
	defer func() { relayoutMid = nil }()
	f.a.setLayoutOverride(base, "left")
	f.a.term(f.path).relayoutNow()
	relayoutMid = nil
	sig := f.a.term(f.path).watch.layoutSig
	left := isLeftOf(leaderW)()
	if left != strings.HasPrefix(sig, "left") {
		t.Errorf("signature %q incohérente avec la mise en page appliquée (leader à %.0f %%)", sig, leaderW())
	}
}

// fastTicks : tour lent du surveillant à 150 ms au lieu d'1 s pour la durée du test (aucune attente de secondes).
func fastTicks(t *testing.T) {
	t.Helper()
	old := tickEvery.Swap(int64(150 * time.Millisecond))
	t.Cleanup(func() { tickEvery.Store(old) })
}

// settleTicks : attend que le surveillant ait terminé n tours lents de plus (attente sur évènement, échéance 20 s).
func settleTicks(t *testing.T, n int64) {
	t.Helper()
	target := tickDone.Load() + n
	end := time.Now().Add(20 * time.Second)
	for tickDone.Load() < target {
		if time.Now().After(end) {
			t.Fatalf("surveillant : %d tour(s) lent(s) non terminé(s) en 20 s", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settle : le surveillant est au repos — deux tours lents complets sans changement.
func settle(t *testing.T) { t.Helper(); settleTicks(t, 2) }

// waitTicksStarted : un tour lent est démarré (le tour lent suivant le démarrage d'une bascule lit déjà la config).
func waitTicksStarted(t *testing.T) {
	t.Helper()
	target := tickBegun.Load() + 1
	end := time.Now().Add(20 * time.Second)
	for tickBegun.Load() < target {
		if time.Now().After(end) {
			t.Fatal("surveillant : aucun nouveau tour lent démarré en 20 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // le tour a lu sa configuration
}

// settleSlow : repos du surveillant alors qu'un tour lent simulé (d) est en cours : le tour en cours finit, puis deux
// tours rapides complets ; le ralentissement est rétabli ensuite.
func settleSlow(t *testing.T, d time.Duration) {
	t.Helper()
	tickSlow.Store(0)
	settle(t)
	tickSlow.Store(int64(d))
}
