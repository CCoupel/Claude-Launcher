package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #153 (LOT-5) — organisations de la fenêtre d'un projet, interface native. Contrat :
// contracts/desktop-dispatch.md, section LOT-5 (projectSettings : layout, layoutAuto, layouts ; projectApply :
// layout « top » | « left » | « » = comme le réglage global ; mise en page du projet ouvert).

func l153Call(t *testing.T, a *App, method string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(method, b)
	if err != nil {
		t.Fatalf("%s : %v", method, err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func confText(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(core.ConfigPath())
	return string(b)
}

// withoutLayoutEntries : config.conf sans les lignes qui citent PROJECT_LAYOUTS (pour vérifier que « seule cette
// clé est écrite »).
func withoutLayoutEntries(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.Contains(l, "PROJECT_LAYOUTS") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// projectSettings : organisation effective, « Comme le réglage global » tant qu'aucune entrée n'existe,
// liste des organisations.
func TestProjectSettingsLayoutFields(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta")
	var r recorder
	a := newApp(t, r.emit)
	s := l153Call(t, a, "projectSettings", map[string]any{"path": ps[0]})
	if s["layout"] != "top" || s["layoutAuto"] != true {
		t.Errorf("défaut : layout=%v layoutAuto=%v, attendu top / true", s["layout"], s["layoutAuto"])
	}
	list, _ := s["layouts"].([]any)
	var ids []string
	for _, x := range list {
		m := x.(map[string]any)
		ids = append(ids, fmt.Sprint(m["id"]))
		if m["label"] == nil || m["label"] == "" {
			t.Errorf("organisation sans libellé : %v", m)
		}
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "left,top" {
		t.Errorf("layouts = %v, attendu top et left", list)
	}
	// réglage global « left » : le projet le suit
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=left\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configReload(core.ConfConf)
	s = l153Call(t, a, "projectSettings", map[string]any{"path": ps[0]})
	if s["layout"] != "left" || s["layoutAuto"] != true {
		t.Errorf("global left : layout=%v layoutAuto=%v, attendu left / true", s["layout"], s["layoutAuto"])
	}
	// exception du projet : l'entrée l'emporte, layoutAuto faux ; l'autre projet suit le global
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=left\ndeclare -A PROJECT_LAYOUTS=([\"alpha\"]=\"top\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configReload(core.ConfConf)
	if s = l153Call(t, a, "projectSettings", map[string]any{"path": ps[0]}); s["layout"] != "top" || s["layoutAuto"] != false {
		t.Errorf("exception : layout=%v layoutAuto=%v, attendu top / false", s["layout"], s["layoutAuto"])
	}
	if s = l153Call(t, a, "projectSettings", map[string]any{"path": ps[1]}); s["layout"] != "left" || s["layoutAuto"] != true {
		t.Errorf("autre projet : layout=%v layoutAuto=%v, attendu left / true", s["layout"], s["layoutAuto"])
	}
}

// projectApply layout : écrit l'entrée, « » la retire (retour au global), absent = inchangé ; seule cette clé est
// écrite (commentaires et autres clés conservés) ; colorSaved vrai dès que config.conf est écrite ; valeur inconnue
// refusée avant toute écriture.
func TestProjectApplyLayout(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	orig := "# ma config\nTEAM_HIDE_IDLE=45   # inactivité\nLAYOUT_MODE=top\ndeclare -A PROJECT_COLORS=([\"alpha\"]=\"22\")\n"
	if err := os.WriteFile(core.ConfigPath(), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configReload(core.ConfConf)

	ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": "left"})
	if ap["ok"] != true || ap["colorSaved"] != true {
		t.Fatalf("layout left : %v (colorSaved vrai dès que config.conf est écrite)", ap)
	}
	got := confText(t)
	if !strings.Contains(got, "PROJECT_LAYOUTS") || !strings.Contains(got, "alpha") || !strings.Contains(got, "left") {
		t.Fatalf("entrée absente :\n%s", got)
	}
	for _, keep := range []string{"# ma config", "TEAM_HIDE_IDLE=45   # inactivité", "LAYOUT_MODE=top", "PROJECT_COLORS"} {
		if !strings.Contains(got, keep) {
			t.Errorf("« %s » perdu : seule la clé PROJECT_LAYOUTS doit être écrite\n%s", keep, got)
		}
	}
	if s := l153Call(t, a, "projectSettings", map[string]any{"path": ps[0]}); s["layout"] != "left" || s["layoutAuto"] != false {
		t.Errorf("après left : layout=%v layoutAuto=%v", s["layout"], s["layoutAuto"])
	}
	// absent : inchangé
	before := confText(t)
	if ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "color": "22"}); ap["ok"] != true {
		t.Fatalf("couleur seule : %v", ap)
	}
	if !strings.Contains(confText(t), "left") || s0(t, a, ps[0])["layout"] != "left" {
		t.Errorf("l'organisation a changé alors que layout est absent :\n%s\navant :\n%s", confText(t), before)
	}
	// couleur et organisation dans la même passe : un seul enregistrement de config.conf
	if ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "color": "52", "layout": "top"}); ap["ok"] != true || ap["colorSaved"] != true {
		t.Fatalf("couleur + organisation : %v", ap)
	}
	if s := s0(t, a, ps[0]); s["layout"] != "top" || s["layoutAuto"] != false {
		t.Errorf("après top explicite : %v %v", s["layout"], s["layoutAuto"])
	}
	// « Comme le réglage global » : l'entrée est retirée
	if ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": ""}); ap["ok"] != true {
		t.Fatalf("layout vide : %v", ap)
	}
	if s := s0(t, a, ps[0]); s["layoutAuto"] != true || s["layout"] != "top" {
		t.Errorf("après « » : layout=%v layoutAuto=%v, attendu top / true (global top)", s["layout"], s["layoutAuto"])
	}
	if strings.Contains(strings.Join(entryLines(confText(t)), "\n"), "alpha") {
		t.Errorf("entrée PROJECT_LAYOUTS conservée après « Comme le réglage global » :\n%s", confText(t))
	}
	if !strings.Contains(confText(t), "# ma config") || !strings.Contains(confText(t), "TEAM_HIDE_IDLE=45") {
		t.Errorf("le reste de config.conf a changé :\n%s", confText(t))
	}
	// valeur inconnue : refusée avant toute écriture
	snap := confText(t)
	ap = l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": "diagonal", "color": "99"})
	if ap["ok"] != false || ap["msg"] == nil || ap["msg"] == "" {
		t.Errorf("layout inconnu accepté : %v", ap)
	}
	if confText(t) != snap {
		t.Errorf("config.conf modifié par un refus (la couleur de la même passe aussi) :\n%s", confText(t))
	}
	// projet hors de l'arbre : refusé comme pour la couleur
	b, _ := json.Marshal(map[string]any{"path": t.TempDir(), "layout": "left"})
	if _, err := a.Dispatch("projectApply", b); err == nil || !strings.Contains(err.Error(), "projet inconnu") {
		t.Errorf("projet hors arbre : %v", err)
	}
}

func s0(t *testing.T, a *App, p string) map[string]any {
	return l153Call(t, a, "projectSettings", map[string]any{"path": p})
}

func entryLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "PROJECT_LAYOUTS") {
			out = append(out, l)
		}
	}
	return out
}

// Projet ouvert : le réglage est permis (appliqué au tour suivant) ; backend et image, eux, restent refusés.
func TestProjectApplyLayoutAllowedOnOpenProject(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.openTabs.Store(ps[0], true)
	if s := s0(t, a, ps[0]); s["open"] != true {
		t.Fatalf("précondition : projet ouvert, %v", s["open"])
	}
	ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": "left"})
	if ap["ok"] != true {
		t.Errorf("layout sur un projet ouvert refusé : %v", ap)
	}
	if !strings.Contains(confText(t), "left") {
		t.Errorf("entrée non écrite :\n%s", confText(t))
	}
}

// ── mise en page de la fenêtre native (tmux dédié, faux claude) ───────────

type nrect struct {
	id         string
	x, y, w, h int
}

func l153Rects(t *testing.T, e map[string]any) (cols, rows int, rs map[string]nrect) {
	t.Helper()
	l := e["layout"].(map[string]any)
	cols, rows = int(l["cols"].(float64)), int(l["rows"].(float64))
	rs = map[string]nrect{}
	for _, x := range l["panes"].([]any) {
		m := x.(map[string]any)
		id := m["id"].(string)
		rs[id] = nrect{id, int(m["x"].(float64)), int(m["y"].(float64)), int(m["w"].(float64)), int(m["h"].(float64))}
	}
	return
}

func l153IsLeft(rs map[string]nrect, leader string, cols int) bool {
	L := rs[leader]
	return L.x == 0 && L.w < cols && L.w > 0 && len(rs) > 1
}

// Organisation « left » dans l'interface native : leader en colonne de gauche (≈ 46 %), teammates à droite en une
// colonne jusqu'à 4 puis deux colonnes ; changement par projectApply pris en compte sans relance du projet, retour
// « Comme le réglage global ».
func TestNativeLayoutLeftAndSwitchWithoutRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Setenv("TMUX", "")
	sock := "cl-desk-l5-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	if err := os.WriteFile(core.ConfigPath(), []byte("LAYOUT_MODE=left\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var r recorder
	a := newApp(t, r.emit)
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(150, 50)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))
	leader := pt.lastLayout().Leader
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	win := "=" + hostSession(ps[0]) + ":"
	var mates []string
	members := []core.TeamMember{{Name: "team-lead", Cwd: core.ClaudePath(ps[0])}}
	writeTeam := func() {
		cfg, _ := json.Marshal(core.TeamConfig{Members: members})
		d := filepath.Join(core.ClaudeDir(), "teams", "t1")
		_ = os.MkdirAll(d, 0o755)
		if err := os.WriteFile(filepath.Join(d, "config.json"), cfg, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	addMate := func() {
		id := tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sh -c 'echo mate; exec cat'")
		tm("select-layout", "-t", win, "tiled")
		mates = append(mates, id)
		members = append(members, core.TeamMember{Name: fmt.Sprintf("dev-%02d", len(mates)), AgentType: "dev", PaneID: id, IsActive: true})
		writeTeam()
	}
	// vérifie la disposition d'un événement layout
	checkLeft := func(e map[string]any) bool {
		cols, rows, rs := l153Rects(t, e)
		if len(rs) != len(mates)+1 {
			return false
		}
		L := rs[leader]
		if !l153IsLeft(rs, leader, cols) || L.h < rows-1 {
			return false
		}
		if want := cols * 46 / 100; L.w < want-3 || L.w > want+3 {
			return false
		}
		xs := map[int]int{}
		for _, m := range mates {
			if rs[m].x <= L.x+L.w {
				return false
			}
			xs[rs[m].x]++
		}
		n := len(mates)
		if n <= 4 {
			return len(xs) == 1
		}
		if len(xs) != 2 {
			return false
		}
		var cnt []int
		for _, c := range xs {
			cnt = append(cnt, c)
		}
		sort.Ints(cnt)
		return cnt[1] == (n+1)/2 && cnt[0] == n/2
	}
	for n := 1; n <= 8; n++ {
		addMate()
		r.wait(t, 20*time.Second, fmt.Sprintf("disposition left à %d teammates", n), isTerm(ps[0], "layout", checkLeft))
	}

	// exception du projet : top, pris en compte sans relancer le projet
	ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": "top"})
	if ap["ok"] != true {
		t.Fatalf("projectApply top : %v", ap)
	}
	r.wait(t, 20*time.Second, "retour à top (exception du projet)", isTerm(ps[0], "layout", func(e map[string]any) bool {
		cols, _, rs := l153Rects(t, e)
		return len(rs) == len(mates)+1 && rs[leader].w == cols
	}))
	// « Comme le réglage global » : le global (left) s'applique de nouveau
	if ap := l153Call(t, a, "projectApply", map[string]any{"path": ps[0], "layout": ""}); ap["ok"] != true {
		t.Fatalf("projectApply « » : %v", ap)
	}
	r.wait(t, 20*time.Second, "retour au réglage global (left)", isTerm(ps[0], "layout", checkLeft))
	if s := s0(t, a, ps[0]); s["layout"] != "left" || s["layoutAuto"] != true {
		t.Errorf("projectSettings : %v %v", s["layout"], s["layoutAuto"])
	}
}
