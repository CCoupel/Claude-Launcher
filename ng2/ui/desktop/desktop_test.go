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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// recorder : événements émis par l'App (à la place de l'interface).
type recorder struct {
	mu  sync.Mutex
	evs []map[string]any
}

func (r *recorder) emit(name string, data any) {
	b, _ := json.Marshal(data)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	m["_name"] = name
	r.mu.Lock()
	r.evs = append(r.evs, m)
	r.mu.Unlock()
}

// wait : premier événement qui satisfait ok, au plus d.
func (r *recorder) wait(t *testing.T, d time.Duration, what string, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	end := time.Now().Add(d)
	seen := 0
	for time.Now().Before(end) {
		r.mu.Lock()
		evs := r.evs[seen:]
		seen = len(r.evs)
		r.mu.Unlock()
		for _, e := range evs {
			if ok(e) {
				return e
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pas d'événement %s en %s", what, d)
	return nil
}

func (r *recorder) output(path string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, e := range r.evs {
		if e["_name"] == "term" && e["kind"] == "out" && e["path"] == path {
			for _, c := range e["chunks"].([]any) {
				b.WriteString(c.(map[string]any)["data"].(string))
			}
		}
	}
	return b.String()
}

func isTerm(path, kind string, more func(map[string]any) bool) func(map[string]any) bool {
	return func(e map[string]any) bool {
		return e["_name"] == "term" && e["path"] == path && e["kind"] == kind && (more == nil || more(e))
	}
}

// sandbox : HOME, config, CLAUDE_CONFIG_DIR et racine de projets isolés ; un
// projet git par nom.
func sandbox(t *testing.T, root string, names ...string) (projects []string) {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CL_CONFIG_DIR", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(k, filepath.Join(root, strings.ToLower(k)))
		_ = os.MkdirAll(filepath.Join(root, strings.ToLower(k)), 0o755)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(root, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(root, "localappdata"))
		t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	}
	t.Setenv("CL_ROOTS", filepath.Join(root, "PROJETS"))
	t.Setenv("CL_NO_UPDATE", "1")
	for _, n := range names {
		p := filepath.Join(root, "PROJETS", "TEST", n)
		_ = os.MkdirAll(p, 0o755)
		if out, err := exec.Command("git", "-C", p, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init : %v %s", err, out)
		}
		projects = append(projects, p)
	}
	return projects
}

func TestSnapshotRowsAndLegend(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	ps := sandbox(t, "", "alpha", "beta")
	var r recorder
	a := NewApp(r.emit)
	a.gitAll()
	s := a.Snapshot()
	var kinds, names []string
	for _, row := range s.Rows {
		kinds = append(kinds, row.Kind)
		names = append(names, row.Name)
	}
	if strings.Join(kinds, ",") != "root,cat,project,project" || names[2] != "alpha" || names[3] != "beta" {
		t.Fatalf("arbre : %v %v", kinds, names)
	}
	p := s.Rows[2]
	if p.Dot == nil || p.Dot.Icon != "○" || !p.GitOK || len(p.Badges) == 0 || p.Color == "" {
		t.Errorf("ligne projet : %+v", p)
	}
	// la légende des cartes = pastille puis badges de la ligne, même ordre
	a.sel = ps[0]
	c := a.Cards()
	if len(c.Legend) != 1+len(p.Badges) || c.Legend[0].Icon != p.Dot.Icon || c.Legend[1].Icon != p.Badges[0].Icon {
		t.Errorf("légende %+v / ligne %+v", c.Legend, p.Badges)
	}
	if c.Name != "alpha" || !c.Loaded {
		t.Errorf("cartes : %+v", c)
	}
}

func TestDispatch(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := NewApp(r.emit)
	res, err := a.Dispatch("init", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if _, ok := m["state"].(Snapshot); !ok || m["help"] == nil || m["tools"] == nil {
		t.Errorf("init : %#v", m)
	}
	if _, err := a.Dispatch("inconnu", nil); err == nil {
		t.Error("appel inconnu accepté")
	}
	if _, err := a.Dispatch("select", json.RawMessage(`{"path":42}`)); err == nil {
		t.Error("arguments invalides acceptés")
	}
	// fermeture d'un projet où rien ne tourne : message, pas de question
	res, err = a.Dispatch("close", json.RawMessage(`{"path":`+strconv.Quote(a.projectPaths()[0])+`}`))
	if err != nil || !strings.Contains(fmt.Sprint(res), "rien ne tourne") {
		t.Errorf("close : %v %v", res, err)
	}
	if _, err := a.Dispatch("create", json.RawMessage(`{"base":`+strconv.Quote(filepath.Dir(a.projectPaths()[0]))+`,"name":"../x"}`)); err == nil {
		t.Error("nom invalide accepté")
	}
	if _, err := a.Dispatch("create", json.RawMessage(`{"base":`+strconv.Quote(filepath.Dir(a.projectPaths()[0]))+`,"name":"alpha"}`)); err == nil || !strings.Contains(err.Error(), "existe déjà") {
		t.Errorf("dossier existant accepté : %v", err)
	}
}

func TestReleaseSpansAndMsCard(t *testing.T) {
	ms := core.MsState{Kind: core.MsIdle, Pending: []core.MsRow{{Title: "v1 — a", Open: 1, Closed: 3}}, Release: "v1.0.0", ReleaseAt: time.Now().Add(-2 * time.Hour)}
	sp := releaseSpans(ms, "v0.9.0")
	if len(sp) != 3 || sp[0].Text != "v1.0.0" || sp[2].Tone != "yellow" {
		t.Errorf("release : %+v", sp)
	}
	if sp := releaseSpans(core.MsState{}, "v2"); sp[1].Text != " (tag git)" {
		t.Errorf("tag seul : %+v", sp)
	}
	c := msCard(ms)
	if c.Kind != "idle" || len(c.Pending) != 1 || c.Pending[0].Total != 4 {
		t.Errorf("milestone : %+v", c)
	}
	if msCard(core.MsState{}) != nil {
		t.Error("carte sans milestone")
	}
}

func TestLaunchInputLineEditing(t *testing.T) {
	var r recorder
	a := &App{emit: r.emit, cfg: core.LoadConfig()}
	pt := newProjTerm(a, core.Node{Path: "/p", Name: "p"}, core.LaunchNormal)
	pt.launchInput("lx\x7fp")
	pt.launchInput("\r")
	select {
	case l := <-pt.lines:
		if l != "lp\n" {
			t.Errorf("ligne = %q", l)
		}
	default:
		t.Fatal("pas de ligne")
	}
}

// tmux local : client de contrôle, mise en page de core appliquée, amorçage
// et saisie, comme pour un projet en conteneur (même code, autre transport).
func TestHostProjectWithTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	sock := "cl-desk-test-" + strconv.Itoa(os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host") // pas de question d'exécution (moteurs détectés)

	var r recorder
	a := NewApp(r.emit)
	// ouvertures simultanées (double-clic et Entrée, reattach) : un seul onglet
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := a.open(ps[0], core.LaunchNormal)
			if err != nil || fmt.Sprint(res) != fmt.Sprint(map[string]any{"tab": ps[0]}) {
				t.Errorf("open : %v %v", res, err)
			}
		}()
	}
	wg.Wait()
	a.mu.Lock()
	tabs := len(a.order)
	a.mu.Unlock()
	if tabs != 1 {
		t.Fatalf("ouvertures simultanées : %d onglets", tabs)
	}
	pt := a.term(ps[0])
	pt.resize(120, 40)
	r.wait(t, 20*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 10*time.Second, "layout", isTerm(ps[0], "layout", nil))
	end := time.Now().Add(10 * time.Second)
	for !strings.Contains(r.output(ps[0]), "faux claude prêt") && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(r.output(ps[0]), "faux claude prêt") {
		t.Fatalf("sortie du leader : %q", r.output(ps[0]))
	}
	leader := pt.lastLayout().Leader
	pt.input(leader, "bonjour é\r")
	end = time.Now().Add(10 * time.Second)
	for !strings.Contains(r.output(ps[0]), "reçu:bonjour é") && time.Now().Before(end) {
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(r.output(ps[0]), "reçu:bonjour é") {
		t.Fatalf("saisie non reçue : %q", r.output(ps[0]))
	}

	// une team de 7 teammates (dont un cdp) : leader et cdp en haut, puis 6
	// teammates sur deux lignes (3 / 3) : 3 lignes en tout
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	win := "=" + hostSession(ps[0]) + ":"
	for i := 0; i < 7; i++ {
		tm("split-window", "-d", "-t", win, "sh -c 'echo teammate; exec cat'")
		tm("select-layout", "-t", win, "tiled")
	}
	ids := strings.Fields(tm("list-panes", "-t", win, "-F", "#{pane_id}"))
	if len(ids) != 8 {
		t.Fatalf("panes : %v", ids)
	}
	members := []core.TeamMember{{Name: "team-lead", Cwd: core.ClaudePath(ps[0])}}
	for i, id := range ids {
		if id == leader {
			continue
		}
		m := core.TeamMember{Name: fmt.Sprintf("agent-%d", i), PaneID: id, IsActive: i%2 == 0}
		if len(members) == 1 {
			m.AgentType, m.Name = "cdp", "cdp"
		}
		members = append(members, m)
	}
	d := filepath.Join(core.ClaudeDir(), "teams", "t1")
	_ = os.MkdirAll(d, 0o755)
	b, _ := json.Marshal(core.TeamConfig{Members: members})
	_ = os.WriteFile(filepath.Join(d, "config.json"), b, 0o644)
	ev := r.wait(t, 15*time.Second, "mise en page de la team", isTerm(ps[0], "layout", func(e map[string]any) bool {
		l := e["layout"].(map[string]any)
		ps := l["panes"].([]any)
		if len(ps) != 8 {
			return false
		}
		top := 0
		for _, p := range ps {
			if p.(map[string]any)["y"].(float64) == 0 {
				top++
			}
		}
		return top == 2 // leader + cdp seuls sur la première ligne
	}))
	rows := map[float64]int{}
	for _, p := range ev["layout"].(map[string]any)["panes"].([]any) {
		rows[p.(map[string]any)["y"].(float64)]++
	}
	if len(rows) != 3 || rows[0] != 2 {
		t.Errorf("lignes : %v (attendu 2 / 3 / 3)", rows)
	}
	// teintes : actifs éclaircis, inactifs assombris (couleur du projet)
	cfg := core.LoadConfig()
	base := cfg.ProjectColor(filepath.Base(ps[0]))
	want := map[string]string{}
	for _, m := range members[1:] {
		mm := m
		want[m.PaneID] = core.Colour256Hex(core.PaneShade(cfg, base, &mm, false))
	}
	r.wait(t, 10*time.Second, "teintes", isTerm(ps[0], "layout", func(e map[string]any) bool {
		for _, p := range e["layout"].(map[string]any)["panes"].([]any) {
			pm := p.(map[string]any)
			if w, ok := want[pm["id"].(string)]; ok && pm["bg"] != w {
				return false
			}
		}
		return true
	}))

	// fenêtre redimensionnée : la team est remise en page à la nouvelle taille
	// (3 lignes : le haut sur 3/7 de la hauteur)
	pt.resize(150, 60)
	r.wait(t, 10*time.Second, "mise en page après redimensionnement", isTerm(ps[0], "layout", func(e map[string]any) bool {
		l := e["layout"].(map[string]any)
		if l["rows"].(float64) != 60 {
			return false
		}
		for _, p := range l["panes"].([]any) {
			pm := p.(map[string]any)
			if pm["id"] == leader {
				return pm["h"].(float64) == float64(60*cfg.Layout3RowLeaderNum/cfg.Layout3RowDen)
			}
		}
		return false
	}))

	// un tour lent sans changement : une recherche de team, un seul list-panes
	time.Sleep(1500 * time.Millisecond) // mise en page des tours précédents faite
	for i := 0; i < 2; i++ {
		pt.tick(false)
	}
	r0, f0 := atomic.LoadInt32(&pt.reads), atomic.LoadInt32(&pt.finds)
	pt.tick(false)
	if dr, df := atomic.LoadInt32(&pt.reads)-r0, atomic.LoadInt32(&pt.finds)-f0; df > 2 || dr > 2 || df < 1 {
		// le watcher peut glisser un tour pendant la mesure : au plus 2
		t.Errorf("un tour : %d list-panes, %d recherches de team", dr, df)
	}

	// carte Team : panes relevés par le watcher, pas d'appel tmux
	if infos := pt.paneInfos(); len(infos) != 8 || infos[0].PID == "" {
		t.Errorf("panes en cache : %+v", infos)
	}

	// détacher l'onglet : la session continue dans tmux
	a.detachTab(ps[0])
	if tm("has-session", "-t", "="+hostSession(ps[0])) != "" || exec.Command("tmux", "-L", sock, "has-session", "-t", "="+hostSession(ps[0])).Run() != nil {
		t.Error("session perdue au détachement")
	}
}
