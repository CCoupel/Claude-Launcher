package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// LOT-3 v6.2.0 — #151 (carte TEAM : teamHead, odd), #152 (panes[].state, options tmux, géométrie avec
// ligne de titre), #162 (onglet Teamleaders). Contrats : contracts/desktop-dispatch.md, contracts/tmux-options.md.
// Maquettes : team_card__versions_entete (A), pane_header__etat_activite (A), tab_bar__teamleaders_separe (B).

// ── #151 : teamHead et odd, calculés dans core ────────────────────────────

// tplProject : projet factice avec un registre d'agents (tags par agent) ; la version cible du template
// est celle du cache du launcher (TplVersionPath).
func tplProject(t *testing.T, proj string, tags map[string]string) {
	t.Helper()
	type reg struct {
		Name string `json:"name"`
		Tag  string `json:"tag"`
	}
	var registry []reg
	for _, n := range []string{"dev-a", "dev-b", "dev-c"} {
		registry = append(registry, reg{n, tags[n]})
	}
	cfg, _ := json.Marshal(map[string]any{"agents": map[string]any{"registry": registry}})
	dir := filepath.Join(proj, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project-config.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	// version du template du projet (#199) : la cible de la carte
	if err := os.WriteFile(filepath.Join(dir, ".template-source.json"), []byte(`{"tag":"v5.9.4","commit":"c"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cardsJSON(t *testing.T, a *App, proj string) map[string]any {
	t.Helper()
	b, err := json.Marshal(a.cardsFull(proj))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func oddOf(row any) []string {
	var out []string
	if l, ok := row.(map[string]any)["odd"].([]any); ok {
		for _, v := range l {
			out = append(out, v.(string))
		}
	}
	return out
}

// `cards.teamHead` : { tpl (version cible), claude (leader ; vide si projet fermé), odd (nombre de teammates
// non conformes) } ; `team[].odd` : ["tpl"] quand le tag de la ligne diffère de teamHead.tpl.
func TestCardsTeamHeadAndOdd(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta", "gamma")
	if err := os.MkdirAll(filepath.Dir(core.TplVersionPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(core.TplVersionPath(), []byte("v5.9.4\n"), 0o644)
	tplProject(t, ps[0], map[string]string{"dev-a": "v5.9.4", "dev-b": "v5.8.0", "dev-c": "v5.9.4"}) // dev-b en retard
	tplProject(t, ps[1], map[string]string{"dev-a": "v5.9.4", "dev-b": "v5.9.4", "dev-c": "v5.9.4"}) // tout conforme
	var r recorder
	a := newApp(t, r.emit)

	c := cardsJSON(t, a, ps[0])
	head, ok := c["teamHead"].(map[string]any)
	if !ok {
		t.Fatalf("cards sans teamHead : %v", c["team"])
	}
	if head["tpl"] != "v5.9.4" {
		t.Errorf("teamHead.tpl = %v, attendu v5.9.4 (version cible)", head["tpl"])
	}
	if cl, _ := head["claude"].(string); cl != "" {
		t.Errorf("teamHead.claude = %q, attendu vide (projet fermé : pas de leader)", cl)
	}
	if head["odd"] != 1.0 {
		t.Errorf("teamHead.odd = %v, attendu 1 (dev-b)", head["odd"])
	}
	team, _ := c["team"].([]any)
	got := map[string][]string{}
	for _, row := range team {
		got[row.(map[string]any)["name"].(string)] = oddOf(row)
	}
	if len(got["dev-b"]) != 1 || got["dev-b"][0] != "tpl" {
		t.Errorf("team[dev-b].odd = %v, attendu [tpl]", got["dev-b"])
	}
	for _, n := range []string{"dev-a", "dev-c"} {
		if len(got[n]) != 0 {
			t.Errorf("team[%s].odd = %v, attendu vide (conforme)", n, got[n])
		}
	}
	// champs existants conservés pour l'infobulle
	for _, row := range team {
		m := row.(map[string]any)
		if m["name"] == "dev-b" && (m["tag"] != "v5.8.0" || m["tplState"] != "late" || m["tplTarget"] != "v5.9.4") {
			t.Errorf("champs de l'infobulle perdus : %v", m)
		}
	}

	// tout conforme : odd = 0
	c = cardsJSON(t, a, ps[1])
	head, _ = c["teamHead"].(map[string]any)
	if head == nil || head["odd"] != 0.0 || head["tpl"] != "v5.9.4" {
		t.Errorf("projet conforme : teamHead = %v, attendu {tpl:v5.9.4, odd:0}", head)
	}
	for _, row := range c["team"].([]any) {
		if l := oddOf(row); len(l) != 0 {
			t.Errorf("ligne conforme avec odd = %v", l)
		}
	}

	// version cible inconnue : pas de version de template dans l'en-tête, aucun écart de template
	tplProject(t, ps[2], map[string]string{"dev-a": "v5.9.4", "dev-b": "v5.8.0", "dev-c": "v5.9.4"})
	_ = os.Remove(filepath.Join(ps[2], ".claude", ".template-source.json"))
	c = cardsJSON(t, a, ps[2])
	head, _ = c["teamHead"].(map[string]any)
	if head == nil || head["tpl"] != "" && head["tpl"] != nil || head["odd"] != 0.0 {
		t.Errorf("cible inconnue : teamHead = %v, attendu tpl vide et odd 0", head)
	}
	for _, row := range c["team"].([]any) {
		if l := oddOf(row); len(l) != 0 {
			t.Errorf("cible inconnue : odd = %v, attendu vide", l)
		}
	}
}

// ── #152 : panes[].state, options tmux, géométrie avec ligne de titre ─────

type lPane struct {
	ID, Role, State string
	X, Y, W, H      int
}

func parsePanes(t *testing.T, e map[string]any) (cols, rows int, out map[string]lPane) {
	t.Helper()
	l := e["layout"].(map[string]any)
	cols, rows = int(l["cols"].(float64)), int(l["rows"].(float64))
	out = map[string]lPane{}
	for _, x := range l["panes"].([]any) {
		m := x.(map[string]any)
		p := lPane{ID: m["id"].(string), X: int(m["x"].(float64)), Y: int(m["y"].(float64)), W: int(m["w"].(float64)), H: int(m["h"].(float64))}
		p.Role, _ = m["role"].(string)
		p.State, _ = m["state"].(string)
		out[p.ID] = p
	}
	return
}

func TestLayoutPanesStateAndBorderOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil { // jamais /tmp/tmux-<uid>
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Setenv("TMUX", "")
	sock := "cl-desk-l3-" + strconv.Itoa(os.Getpid())
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
	for i := 0; i < 4; i++ {
		mates = append(mates, tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sh -c 'echo mate-"+strconv.Itoa(i)+"; exec cat'"))
	}
	cdp, actif, calme, manual := mates[0], mates[1], mates[2], mates[3] // manual : pane ouvert à la main, sans agent
	writeTeam := func(activeID string) {
		t.Helper()
		member := func(name, typ, pane string) core.TeamMember {
			return core.TeamMember{Name: name, AgentType: typ, PaneID: pane, IsActive: pane == activeID}
		}
		cfg, _ := json.Marshal(core.TeamConfig{Members: []core.TeamMember{
			{Name: "team-lead", Cwd: core.ClaudePath(ps[0])},
			member("cdp", "cdp", cdp), member("dev-a", "dev-a", actif), member("dev-b", "dev-b", calme),
		}})
		d := filepath.Join(core.ClaudeDir(), "teams", "t1")
		_ = os.MkdirAll(d, 0o755)
		if err := os.WriteFile(filepath.Join(d, "config.json"), cfg, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTeam(actif)

	// panes[].state : lead (leader, cdp), active, idle, "" (pane sans agent) ; role et active inchangés
	waitState := func(want map[string]string, d time.Duration) map[string]lPane {
		t.Helper()
		var last map[string]lPane
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			if l := pt.lastLayout(); l != nil {
				b, _ := json.Marshal(map[string]any{"layout": l})
				var e map[string]any
				_ = json.Unmarshal(b, &e)
				_, _, last = parsePanes(t, e)
				ok := len(last) == 5
				for id, st := range want {
					if p, found := last[id]; !found || p.State != st {
						ok = false
					}
				}
				if ok {
					return last
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("états des panes %v après %v : %+v, attendu %v", d, d, last, want)
		return nil
	}
	want := map[string]string{leader: "lead", cdp: "lead", actif: "active", calme: "idle", manual: ""}
	got := waitState(want, 20*time.Second)
	for id, role := range map[string]string{leader: "leader", cdp: "cdp", actif: "actif", calme: "inactif", manual: ""} {
		if got[id].Role != role {
			t.Errorf("pane %s : role = %q, attendu %q (inchangé)", id, got[id].Role, role)
		}
	}
	// le JSON porte bien un champ « state » (même vide) pour chaque pane
	ev := r.wait(t, 10*time.Second, "layout à 5 panes", isTerm(ps[0], "layout", func(e map[string]any) bool {
		return len(e["layout"].(map[string]any)["panes"].([]any)) == 5
	}))
	for _, x := range ev["layout"].(map[string]any)["panes"].([]any) {
		if _, ok := x.(map[string]any)["state"]; !ok {
			t.Errorf("pane sans champ state dans l'évènement layout : %v", x)
		}
	}
	// géométrie avec une ligne de titre par pane : tous les panes dans la fenêtre, sans chevauchement
	cols, rows, geo := parsePanes(t, ev)
	var rects []lPane
	for _, p := range geo {
		if p.W <= 0 || p.H <= 0 || p.X < 0 || p.Y < 0 || p.X+p.W > cols || p.Y+p.H > rows {
			t.Errorf("pane %+v hors de la fenêtre %dx%d", p, cols, rows)
		}
		rects = append(rects, p)
	}
	for i := range rects {
		for j := i + 1; j < len(rects); j++ {
			a, b := rects[i], rects[j]
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Errorf("panes %s et %s se chevauchent : %+v %+v", a.ID, b.ID, a, b)
			}
		}
	}

	// options de pane (contracts/tmux-options.md) posées quand l'interface native a la main
	paneOpt := func(pane, opt string) string { return tm("show-options", "-pqv", "-t", pane, opt) }
	waitOpt := func(pane, opt, want string) {
		t.Helper()
		end := time.Now().Add(30 * time.Second)
		for time.Now().Before(end) {
			if paneOpt(pane, opt) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("pane %s : %s = %q, attendu %q", pane, opt, paneOpt(pane, opt), want)
	}
	for pane, st := range map[string]string{leader: "lead", cdp: "lead", actif: "active", calme: "idle"} {
		waitOpt(pane, "@cl_state", st)
	}
	waitOpt(leader, "@cl_name", "team-lead")
	waitOpt(cdp, "@cl_name", "cdp")
	waitOpt(actif, "@cl_name", "dev-a")
	waitOpt(calme, "@cl_name", "dev-b")
	waitOpt(manual, "@cl_state", "") // pane sans agent : options absentes
	waitOpt(manual, "@cl_name", "")
	// options de fenêtre : une ligne de titre par pane, format construit sur @cl_name et @cl_state
	if v := tm("show-options", "-wqv", "-t", win, "pane-border-status"); v != "top" {
		t.Errorf("pane-border-status = %q, attendu top", v)
	}
	if f := tm("show-options", "-wqv", "-t", win, "pane-border-format"); !strings.Contains(f, "@cl_name") || !strings.Contains(f, "@cl_state") {
		t.Errorf("pane-border-format = %q, attendu un format construit sur #{@cl_name} et #{@cl_state}", f)
	}

	// changement d'état reflété sans relance : dev-a devient inactif, dev-b actif
	writeTeam(calme)
	waitState(map[string]string{leader: "lead", cdp: "lead", actif: "idle", calme: "active", manual: ""}, 15*time.Second)
	waitOpt(actif, "@cl_state", "idle")
	waitOpt(calme, "@cl_state", "active")
}

// ── #162 : onglet Teamleaders (frontend) ──────────────────────────────────

// Ctrl+Tab : Teamleaders d'abord, puis les projets dans l'ordre des onglets (maquette B).
func TestCycleTabOrderTeamleadersFirst(t *testing.T) {
	js := asset(t, "app.js")
	m := regexp.MustCompile(`(?s)function cycleTab\(d\) \{(.{0,400}?)\n\}`).FindStringSubmatch(js)
	if m == nil {
		t.Fatal("cycleTab absente de app.js")
	}
	if !regexp.MustCompile(`\[\s*'leaders'\s*\]\s*\.concat\(`).MatchString(m[1]) {
		t.Errorf("cycleTab : Teamleaders doit venir en premier, puis les projets (concat) : %s", m[1])
	}
	if !strings.Contains(m[1], "tabs") || !strings.Contains(m[1], "% ids.length") {
		t.Errorf("cycleTab : parcours circulaire des onglets attendu : %s", m[1])
	}
}

// Teamleaders : pastille hors de la zone qui défile, suivie d'un filet ; les onglets de projets défilent à
// part ; réduction à l'icône mesurée avec la pastille à sa largeur COMPLÈTE (pas d'oscillation).
func TestTeamleadersTabOutsideScrollAndNoOscillation(t *testing.T) {
	js, css := asset(t, "app.js"), asset(t, "style.css")
	rt := regexp.MustCompile(`(?s)function renderTabs\(\) \{(.*?)\n\}\n`).FindStringSubmatch(js)
	if rt == nil {
		t.Fatal("renderTabs absente de app.js")
	}
	body := rt[1]
	if !regexp.MustCompile(`bar\.append\(\s*\w+\s*,\s*el\('div',\s*'tsep'\)\s*,\s*\w+\s*\)`).MatchString(body) &&
		!(strings.Contains(body, "tsep") && strings.Contains(body, "tscroll")) {
		t.Error("renderTabs : pastille Teamleaders, filet (tsep) puis zone défilante (tscroll) attendus dans la barre")
	}
	iLead, iScroll := strings.Index(body, "'tl"), strings.Index(body, "tscroll")
	if iLead < 0 || iScroll < 0 || iLead > iScroll {
		t.Error("renderTabs : la pastille Teamleaders doit être créée avant la zone défilante")
	}
	if !strings.Contains(body, "scroll.appendChild") && !regexp.MustCompile(`tscroll[^;]*;[\s\S]*appendChild`).MatchString(body) {
		t.Error("renderTabs : les onglets de projets doivent être ajoutés à la zone défilante, pas à la barre")
	}
	fit := regexp.MustCompile(`(?s)function fitLeadersTab\(\) \{(.*?)\n\}\n`).FindStringSubmatch(js)
	if fit == nil {
		t.Fatal("fitLeadersTab absente de app.js : réduction à l'icône à mesurer")
	}
	f := fit[1]
	rm, meas := strings.Index(f, "classList.remove('ico')"), strings.Index(f, "offsetWidth")
	if rm < 0 || meas < 0 || rm > meas {
		t.Error("fitLeadersTab : la pastille doit être remise à sa largeur complète (retrait de 'ico') AVANT la mesure — sinon oscillation réduit/étendu")
	}
	if !strings.Contains(f, "scrollWidth") || !strings.Contains(f, "classList.add('ico')") {
		t.Error("fitLeadersTab : réduction à l'icône quand les onglets de projets débordent (scrollWidth) attendue")
	}
	if !regexp.MustCompile(`addEventListener\(\s*'resize'[^)]*fitLeadersTab|fitLeadersTab\(\)\s*\)`).MatchString(js) || !strings.Contains(js, "'resize'") {
		t.Error("recalcul à chaque redimensionnement de la fenêtre absent (resize → fitLeadersTab)")
	}
	if !strings.Contains(f, "Teamleaders") {
		t.Error("fitLeadersTab : infobulle « Teamleaders » attendue sur l'icône")
	}
	if !regexp.MustCompile(`\.tscroll\s*\{[^}]*overflow-x:\s*(auto|scroll)`).MatchString(css) {
		t.Error("style.css : la zone .tscroll doit défiler horizontalement (overflow-x)")
	}
	if !regexp.MustCompile(`\.tl\s*\{[^}]*flex(-shrink)?:\s*(0|none)`).MatchString(css) {
		t.Error("style.css : la pastille .tl ne doit pas se comprimer (flex: none / flex-shrink: 0)")
	}
}

// ── #151 / #152 : rendu dans l'interface (contrôles statiques ; le rendu est vu en procédure) ──

func TestTeamCardHeaderAndRowsFrontend(t *testing.T) {
	js, css := asset(t, "app.js"), asset(t, "style.css")
	if !strings.Contains(js, "teamHead") {
		t.Error("app.js : en-tête de la carte TEAM (teamHead) absent")
	}
	if !regexp.MustCompile(`tpl\s+'?\s*\+|'\s*·\s*tpl |tpl \$\{`).MatchString(js) && !strings.Contains(js, "tpl ") {
		t.Error("app.js : titre « Team · N · tpl vX · claude Y » absent")
	}
	if !strings.Contains(js, "claude ") {
		t.Error("app.js : version de claude du leader absente du titre de la carte")
	}
	if !strings.Contains(js, "odd") || !strings.Contains(js, "⚠") {
		t.Error("app.js : ⚠ des teammates non conformes (team[].odd) absent")
	}
	m := regexp.MustCompile(`\.team \.tr \{[^}]*grid-template-columns:([^;]*);`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("grille .team .tr absente de style.css")
	}
	tracks := regexp.MustCompile(`calc\(\s*(\d+)\s*\*\s*var\(--cu\)\)|minmax\([^)]*\)|(\d+)px`).FindAllStringSubmatch(m[1], -1)
	var widths []int
	for _, tr := range tracks {
		w := 0
		for _, g := range tr[1:] {
			if g != "" {
				w, _ = strconv.Atoi(g)
			}
		}
		widths = append(widths, w)
	}
	// 12 | 1fr (minmax) | 110 | 12 : plus de colonne de version (62), ⚠ en dernière colonne étroite
	if len(widths) != 4 || widths[2] != 110 || widths[3] > 16 {
		t.Errorf("grille .team .tr = %q (pistes %v), attendu « 12 | 1fr | 110 | 12 » (colonne de version retirée, ⚠ en dernière colonne)", strings.TrimSpace(m[1]), widths)
	}
}

func TestPaneHeaderUsesStateField(t *testing.T) {
	js, css := asset(t, "app.js"), asset(t, "style.css")
	if !regexp.MustCompile(`p\.state`).MatchString(js) {
		t.Error("app.js : l'en-tête du pane doit se fonder sur panes[].state (plus sur le texte de role)")
	}
	if regexp.MustCompile(`role\s*===?\s*'(actif|inactif)'`).MatchString(js) {
		t.Error("app.js : état déduit du texte de role ('actif' / 'inactif') — utiliser panes[].state")
	}
	for _, st := range []string{"active", "idle"} {
		if !regexp.MustCompile(`\.phead[^{]*\b` + st + `\b[^{]*\{`).MatchString(css) {
			t.Errorf("style.css : bandeau d'en-tête du pane à l'état %q absent (maquette A : vert actif, gris sombre inactif)", st)
		}
	}
}
