package core

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// #136 — compléments aux tests de panevis_test.go, depuis contracts/tmux-options.md
// et la maquette team_card__panes_caches.html : bords de la règle, protection
// (leader/cdp/sans agent), refus de bascule, nettoyage des choix, réserve à la
// demande, fermeture limitée à son projet, mode conteneur, carte TEAM.

func TestDecidePaneVisContractEdges(t *testing.T) {
	const now = 1000
	run := func(p VisPane, choice string, hideIdle int) VisDecision {
		ch := map[string]string{}
		if choice != "" {
			ch[p.ID] = choice
		}
		return DecidePaneVis([]VisPane{p}, ch, hideIdle, now)
	}
	// bord du délai : 1 s avant = visible, au délai = caché
	if run(VisPane{ID: "%3", IdleAt: now - 29}, "", 30).Hide["%3"] {
		t.Error("caché 1 s avant le délai")
	}
	if !run(VisPane{ID: "%3", IdleAt: now - 30}, "", 30).Hide["%3"] {
		t.Error("pas caché au délai")
	}
	// un actif caché est ramené (non caché) et ses choix h ET s sont retirés
	for _, mode := range []string{"h", "s"} {
		d := run(VisPane{ID: "%3", Active: true, Hidden: true, IdleAt: 5}, mode, 30)
		if d.Hide["%3"] || len(d.Choices) != 0 || d.IdleAt["%3"] != 0 {
			t.Errorf("actif caché, choix %s : %+v", mode, d)
		}
	}
	// priorité 3 : « h » prime sur le focus ; « s » prime sur le délai
	if !run(VisPane{ID: "%3", Focus: true, IdleAt: now}, "h", 30).Hide["%3"] {
		t.Error("choix « h » ignoré à cause du focus")
	}
	if run(VisPane{ID: "%3", IdleAt: 1}, "s", 30).Hide["%3"] {
		t.Error("visible fixé caché par le délai")
	}
	// protégé : jamais caché, même avec un choix « h » parasite
	if run(VisPane{ID: "%1", Protected: true, IdleAt: 1}, "h", 30).Hide["%1"] {
		t.Error("leader caché")
	}
	// plusieurs panes : décisions indépendantes
	d := DecidePaneVis([]VisPane{{ID: "%1", Protected: true}, {ID: "%2", Active: true},
		{ID: "%3", IdleAt: now - 100}, {ID: "%4", IdleAt: now - 1}}, nil, 30, now)
	if !reflect.DeepEqual(d.Hide, map[string]bool{"%3": true}) {
		t.Errorf("décisions indépendantes : %v", d.Hide)
	}
	// un pane revenu d'un « h » (retour à l'activité) puis inactif repart du délai à zéro
	if d := run(VisPane{ID: "%3"}, "", 30); d.IdleAt["%3"] != now || d.Hide["%3"] {
		t.Errorf("nouvelle inactivité : %+v", d)
	}
}

func TestVisPanesProtection(t *testing.T) {
	mk := func(ids ...string) []ProjPane {
		var ps []ProjPane
		for _, id := range ids {
			ps = append(ps, ProjPane{PaneInfo: PaneInfo{ID: id}})
		}
		return ps
	}
	tc := &TeamConfig{Members: []TeamMember{
		{Name: "team-lead", AgentType: "team-lead", PaneID: "%9"},
		{Name: "chef", AgentType: "cdp", PaneID: "%10"},
		{Name: "dev-a", AgentType: "dev-a", PaneID: "%11", IsActive: true},
		{Name: "dev-b", AgentType: "dev-b", PaneID: "%12"},
	}}
	// %9 < %10 numériquement (pas lexicalement) ; %20 est ouvert à la main
	got := map[string]VisPane{}
	for _, v := range visPanes(mk("%12", "%9", "%10", "%11", "%20"), tc) {
		got[v.ID] = v
	}
	for id, wantProt := range map[string]bool{"%9": true, "%10": true, "%20": true, "%11": false, "%12": false} {
		if got[id].Protected != wantProt {
			t.Errorf("%s protégé = %v, attendu %v", id, got[id].Protected, wantProt)
		}
	}
	if !got["%11"].Active || got["%12"].Active {
		t.Errorf("actif : %+v %+v", got["%11"], got["%12"])
	}
	// plus petit %id = leader même sans team-lead dans la config ; sans team : tout est protégé
	if v := visPanes(mk("%4", "%3"), &TeamConfig{Members: []TeamMember{{AgentType: "dev-a", PaneID: "%4"}}}); !v[1].Protected {
		t.Error("le plus petit %id n'est pas le leader")
	}
	for _, v := range visPanes(mk("%1", "%2"), nil) {
		if !v.Protected {
			t.Errorf("sans team, %s doit rester visible", v.ID)
		}
	}
}

// fenêtre « proj » avec leader + 4 panes : a actif, b/c/d inactifs, e hors team
func visFixture(t *testing.T) (i isoTmux, win, home, leader string, p map[string]string, tc *TeamConfig) {
	t.Helper()
	i = newIsoTmux(t)
	if _, err := i.run("new-session", "-d", "-s", "hub", "-x", "200", "-y", "100", "-n", "proj", i.sleepCmd()); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	win = "=hub:=proj"
	leader = i.must(t, "display-message", "-p", "-t", win, "#{pane_id}")
	p = map[string]string{}
	for _, n := range []string{"a", "b", "c", "d"} {
		p[n] = i.must(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", leader, i.sleepCmd())
	}
	tc = &TeamConfig{Members: []TeamMember{
		{Name: "team-lead", AgentType: "team-lead", PaneID: leader},
		{Name: "dev-a", AgentType: "dev-a", PaneID: p["a"], IsActive: true},
		{Name: "dev-b", AgentType: "dev-b", PaneID: p["b"]},
		{Name: "dev-c", AgentType: "dev-c", PaneID: p["c"]},
		{Name: "chef", AgentType: "cdp", PaneID: p["d"]},
	}}
	return i, win, i.must(t, "display-message", "-p", "-t", win, "#{window_id}"), leader, p, tc
}

// Refus de bascule (cdp, leader, actif, inconnu, projet fermé), puis cycle
// visible → caché (h) → visible fixé (s) → caché (h), lu dans @cl_panevis ;
// une entrée d'un pane disparu est nettoyée par le tour suivant.
func TestToggleRefusalsAndCycle(t *testing.T) {
	i, win, home, leader, p, tc := visFixture(t)
	for name, pane := range map[string]string{"cdp": p["d"], "leader": leader, "actif": p["a"], "inconnu": "%9999"} {
		if r := TogglePane(i.run, win, home, tc, pane, false); r.OK || r.Msg == "" {
			t.Errorf("bascule de %s permise : %+v", name, r)
		}
	}
	if got := i.must(t, "show-options", "-wqv", "-t", win, optVis); got != "" {
		t.Fatalf("un refus a écrit @cl_panevis : %q", got)
	}
	if r := TogglePane(i.run, "=hub:=disparue", home, tc, p["b"], false); r.OK || r.Msg == "" {
		t.Errorf("projet fermé : %+v", r)
	}
	vis := func() string { return i.must(t, "show-options", "-wqv", "-t", win, optVis) }
	b := p["b"]
	if r := TogglePane(i.run, win, home, tc, b, false); !r.OK || vis() != b+":h" {
		t.Fatalf("visible → caché : %+v %q", r, vis())
	}
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1000, false); err != nil {
		t.Fatal(err)
	}
	if r := TogglePane(i.run, win, home, tc, b, false); !r.OK || vis() != b+":s" {
		t.Fatalf("caché → fixé visible : %+v %q", r, vis())
	}
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1001, false); err != nil {
		t.Fatal(err)
	}
	if r := TogglePane(i.run, win, home, tc, b, false); !r.OK || vis() != b+":h" {
		t.Fatalf("fixé → caché : %+v %q", r, vis())
	}
	// entrée d'un pane qui n'existe plus : retirée au tour suivant, sans toucher les autres
	i.must(t, "set-option", "-w", "-t", win, optVis, b+":h %9999:s")
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1002, false); err != nil {
		t.Fatal(err)
	}
	if got := vis(); got != b+":h" {
		t.Errorf("entrée périmée conservée : %q", got)
	}
}

// Délai : rien ne bouge avant TEAM_HIDE_IDLE ; 0 = jamais ; le leader, le cdp et
// l'actif restent dans la fenêtre ; aucun pane n'est tué (mêmes %id au retour).
func TestApplyDelayAndProtected(t *testing.T) {
	i, win, home, leader, p, tc := visFixture(t)
	inWin := func() []string {
		return strings.Fields(i.must(t, "list-panes", "-t", win, "-F", "#{pane_id}"))
	}
	all := func() []string {
		return strings.Fields(i.must(t, "list-panes", "-a", "-F", "#{pane_id}"))
	}
	if _, err := ApplyPaneVis(i.run, win, home, tc, 0, 1000, false); err != nil { // 0 : jamais
		t.Fatal(err)
	}
	if r, _ := ApplyPaneVis(i.run, win, home, tc, 0, 999999, false); r.Changed() {
		t.Fatalf("TEAM_HIDE_IDLE=0 a masqué : %+v", r)
	}
	r, err := ApplyPaneVis(i.run, win, home, tc, 30, 1000000, false)
	if err != nil || len(r.Hid) != 2 {
		t.Fatalf("masquage : %+v %v", r, err)
	}
	w := strings.Join(inWin(), " ")
	for _, keep := range []string{leader, p["a"], p["d"]} { // leader, actif, cdp
		if !strings.Contains(w+" ", keep+" ") {
			t.Errorf("%s caché alors que protégé ou actif : %q", keep, w)
		}
	}
	// aucun pane tué : tous les %id existent encore (fenêtre + réserve)
	before := strings.Join(all(), " ")
	for _, id := range []string{leader, p["a"], p["b"], p["c"], p["d"]} {
		if !strings.Contains(before+" ", id+" ") {
			t.Errorf("pane %s disparu", id)
		}
	}
	// la réserve n'est pas comptée avec son amorce : exactement 2 panes cachés recensés
	ps, _, _ := ProjectPanes(i.run, win, home, false)
	hidden := 0
	for _, q := range ps {
		if q.Hidden {
			hidden++
		}
	}
	if hidden != 2 || len(ps) != 5 {
		t.Errorf("recensement : %d cachés sur %d", hidden, len(ps))
	}
}

// Réserve créée à la demande, à la taille de la fenêtre du projet ; fermeture
// d'un projet : seuls ses panes sont tués, la réserve reste tant qu'un autre
// projet y a un pane caché, puis disparaît.
func TestReserveOnDemandAndCloseScopedToProject(t *testing.T) {
	i, win, home, _, p, tc := visFixture(t)
	if _, err := i.run("has-session", "-t", "="+HiddenSession); err == nil {
		t.Fatal("réserve créée avant tout masquage")
	}
	// second projet
	i.must(t, "new-window", "-d", "-t", "=hub:", "-n", "autre", i.sleepCmd())
	win2 := "=hub:=autre"
	l2 := i.must(t, "display-message", "-p", "-t", win2, "#{pane_id}")
	b2 := i.must(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", l2, i.sleepCmd())
	home2 := ProjectHome(i.run, win2, false)
	tc2 := &TeamConfig{Members: []TeamMember{{Name: "team-lead", PaneID: l2}, {Name: "dev-b", AgentType: "dev-b", PaneID: b2}}}
	for _, a := range []struct {
		win, home string
		tc        *TeamConfig
	}{{win, home, tc}, {win2, home2, tc2}} {
		ApplyPaneVis(i.run, a.win, a.home, a.tc, 30, 1000, false)
		if r, err := ApplyPaneVis(i.run, a.win, a.home, a.tc, 30, 5000, false); err != nil || !r.Changed() {
			t.Fatalf("masquage %s : %+v %v", a.win, r, err)
		}
	}
	size := i.must(t, "display-message", "-p", "-t", win, "#{window_width}x#{window_height}")
	if got := i.must(t, "display-message", "-p", "-t", "="+HiddenSession+":", "#{window_width}x#{window_height}"); got != size {
		t.Errorf("taille de la réserve %s, fenêtre %s", got, size)
	}
	// fermer le projet 1 : ses 2 panes tués, celui du projet 2 vivant, réserve conservée
	if n := KillHiddenPanes(i.run, home); n != 2 {
		t.Fatalf("panes tués du projet 1 : %d", n)
	}
	if _, err := i.run("has-session", "-t", "="+HiddenSession); err != nil {
		t.Fatal("réserve supprimée alors que le projet 2 y a un pane")
	}
	left := i.must(t, "list-panes", "-a", "-F", "#{pane_id}")
	if strings.Contains(left, p["b"]) || strings.Contains(left, p["c"]) || !strings.Contains(left, b2) {
		t.Fatalf("panes restants : %q", left)
	}
	if n := KillHiddenPanes(i.run, home2); n != 1 {
		t.Fatalf("panes tués du projet 2 : %d", n)
	}
	if _, err := i.run("has-session", "-t", "="+HiddenSession); err == nil {
		t.Error("réserve conservée sans aucun pane caché")
	}
	if n := KillHiddenPanes(i.run, ""); n != 0 { // home inconnu : ne tue rien
		t.Errorf("home vide : %d tués", n)
	}
}

// Conteneur (un seul projet par tmux) : @cl_home vaut « ctr » et le PID, d'un
// autre espace de noms, n'est pas exposé.
func TestContainerModeHome(t *testing.T) {
	i, win, _, _, p, tc := visFixture(t)
	if h := ProjectHome(i.run, win, true); h != HomeCtr || HomeCtr != "ctr" {
		t.Fatalf("home conteneur = %q", h)
	}
	ApplyPaneVis(i.run, win, HomeCtr, tc, 30, 1000, true)
	if r, err := ApplyPaneVis(i.run, win, HomeCtr, tc, 30, 5000, true); err != nil || len(r.Hid) != 2 {
		t.Fatalf("masquage en conteneur : %+v %v", r, err)
	}
	if got := i.must(t, "show-options", "-pqv", "-t", p["b"], optHome); got != "ctr" {
		t.Errorf("@cl_home = %q", got)
	}
	ps, _, _ := ProjectPanes(i.run, win, HomeCtr, true)
	if len(ps) != 5 {
		t.Fatalf("recensement conteneur : %d panes", len(ps))
	}
	for _, q := range ps {
		if q.PID != "" {
			t.Errorf("PID exposé pour %s en conteneur : %q", q.ID, q.PID)
		}
	}
}

// Carte TEAM : la version du template, le délai restant décroît avec le temps,
// 0 si TEAM_HIDE_IDLE=0 ou si un choix manuel existe, instances d'un même type
// triées numériquement par %id, pane ouvert à la main ignoré, un seul passage
// par teammate du registre (pas de ligne « non démarré » en double).
func TestTeamLinesCountdownAndInstances(t *testing.T) {
	unixOnly(t)
	root, proj := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	_ = os.MkdirAll(root+"/teams/s1", 0o755)
	_ = os.WriteFile(root+"/teams/s1/config.json", []byte(`{"members":[
		{"name":"team-lead","agentType":"team-lead","tmuxPaneId":"%1","cwd":"`+proj+`"},
		{"name":"dev-1","agentType":"dev-backend","tmuxPaneId":"%10"},
		{"name":"dev-2","agentType":"dev-backend","tmuxPaneId":"%9"}]}`), 0o644)
	pane := func(id string, idle int64) ProjPane {
		return ProjPane{PaneInfo: PaneInfo{ID: id, Cmd: "2.1.87"}, IdleAt: idle}
	}
	panes := []ProjPane{pane("%10", 100), pane("%1", 0), pane("%9", 100), pane("%20", 0)}
	reg := []Teammate{{Name: "dev-backend", Tag: "v5"}, {Name: "qa"}}
	names := func(ls []TeamLine) []string {
		var n []string
		for _, l := range ls {
			n = append(n, l.Name)
		}
		return n
	}
	got := TeamLines(reg, proj, panes, map[string]string{}, 30, 110)
	if want := []string{"leader", "dev-2", "dev-1", "qa"}; !reflect.DeepEqual(names(got), want) { // %9 avant %10, %20 ignoré
		t.Fatalf("lignes : %v, attendu %v", names(got), want)
	}
	if got[1].IdleLeft != 20 || got[1].Tag != "v5" || got[3].Started {
		t.Errorf("dev-2 / qa : %+v %+v", got[1], got[3])
	}
	if l := TeamLines(reg, proj, panes, map[string]string{}, 30, 125)[1]; l.IdleLeft != 5 {
		t.Errorf("délai restant à +25 s : %d", l.IdleLeft)
	}
	if l := TeamLines(reg, proj, panes, map[string]string{}, 30, 500)[1]; l.IdleLeft != 0 {
		t.Errorf("délai dépassé : %d", l.IdleLeft)
	}
	if l := TeamLines(reg, proj, panes, map[string]string{}, 0, 110)[1]; l.IdleLeft != 0 {
		t.Errorf("TEAM_HIDE_IDLE=0 : %d", l.IdleLeft)
	}
	if l := TeamLines(reg, proj, panes, map[string]string{"%9": "h"}, 30, 110)[1]; l.IdleLeft != 0 || l.Pinned {
		t.Errorf("caché à la main : %+v", l)
	}
	if l := TeamLines(reg, proj, panes, map[string]string{"%9": "s"}, 30, 110)[1]; l.IdleLeft != 0 || !l.Pinned {
		t.Errorf("fixé visible : %+v", l)
	}
}
