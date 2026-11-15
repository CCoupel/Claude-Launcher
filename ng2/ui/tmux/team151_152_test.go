package tmux

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// LOT-3 v6.2.0 — #151 (carte TEAM : versions en en-tête) et #152 (état d'activité des panes), coque Linux.
// Contrats : contracts/desktop-dispatch.md (teamHead, odd, panes[].state), contracts/tmux-options.md
// (@cl_state, @cl_name). Maquettes : team_card__versions_entete.html (A), pane_header__etat_activite.html (A).

func teamModel(lines []core.TeamLine) stateModel {
	m := stateModel{cfg: visCfg(), w: 100, h: 60, loaded: true, hasSel: true, sel: Selection{Name: "p", Key: "p", Path: ""}}
	m.panes = paneData{open: true, lines: lines}
	return m
}

func lot3Lines() []core.TeamLine {
	return []core.TeamLine{
		{Name: "leader", Pane: "%1", Started: true, Claude: "2.1.87", Tag: "v5.9.4", TplState: core.TplLineOK, TplTarget: "v5.9.4"},
		{Name: "dev-a", Pane: "%2", Started: true, Active: true, Model: "claude-sonnet", Effort: "medium", Claude: "2.1.87", Tag: "v5.9.4", TplState: core.TplLineOK, TplTarget: "v5.9.4"},
		{Name: "dev-b", Pane: "%3", Started: true, Model: "claude-sonnet", Effort: "medium", Claude: "2.1.87", Tag: "v5.8.0", TplState: core.TplLineLate, TplTarget: "v5.9.4"},
		{Name: "dev-c", Pane: "%4", Started: true, Model: "claude-haiku", Effort: "low", Claude: "2.1.80", Tag: "v5.9.4", TplState: core.TplLineOK, TplTarget: "v5.9.4"},
	}
}

// Titre de la carte : « Team · N · tpl vX · claude Y » — version cible du template et version de claude
// du leader dans l'en-tête, quelle que soit la largeur (plus seulement en pane étroit).
func TestTeamCardTitleCarriesVersions(t *testing.T) {
	for _, w := range []int{100, 40} {
		m := teamModel(lot3Lines())
		m.w = w
		title := strings.ToLower(m.teamTitle())
		if !strings.Contains(title, "team") || !strings.Contains(title, "3") {
			t.Errorf("w=%d : titre %q, attendu « Team · 3 … » (3 teammates lancés)", w, m.teamTitle())
		}
		if !strings.Contains(title, "tpl v5.9.4") {
			t.Errorf("w=%d : titre %q sans « tpl v5.9.4 » (version cible du template)", w, m.teamTitle())
		}
		if !strings.Contains(title, "claude 2.1.87") {
			t.Errorf("w=%d : titre %q sans « claude 2.1.87 » (version de claude du leader)", w, m.teamTitle())
		}
	}
}

// Titre sans version connue (projet fermé, cible inconnue) : pas de « tpl » ni « claude » vides.
func TestTeamCardTitleOmitsUnknownVersions(t *testing.T) {
	lines := []core.TeamLine{{Name: "dev-a", Tag: "v5.9.4"}, {Name: "dev-b", Tag: "v5.9.4"}}
	title := strings.ToLower(teamModel(lines).teamTitle())
	if strings.Contains(title, "tpl ") || strings.Contains(title, "claude ") {
		t.Errorf("titre %q : version inconnue affichée", title)
	}
}

// Lignes : marque, nom, modèle · effort, ⚠, état — plus aucune colonne de version (ni du template, ni
// de claude) ; ⚠ seulement sur les écarts (template en retard, version de claude différente de celle du leader).
func TestTeamCardRowsHaveNoVersionColumn(t *testing.T) {
	m := teamModel(lot3Lines())
	rows := strings.Split(m.teamBody(98), "\n")
	if len(rows) != 4 {
		t.Fatalf("lignes : %q", rows)
	}
	for i, r := range rows {
		for _, v := range []string{"v5.9.4", "v5.8.0", "2.1.87", "2.1.80", "→"} {
			if strings.Contains(r, v) {
				t.Errorf("ligne %d %q : colonne de version %q conservée (maquette A : versions dans le titre)", i, r, v)
			}
		}
	}
	for i, wantWarn := range []bool{false, false, true, true} {
		if got := strings.Contains(rows[i], "⚠"); got != wantWarn {
			t.Errorf("ligne %d %q : ⚠=%v, attendu %v", i, rows[i], got, wantWarn)
		}
	}
	// le ⚠ remplace le ▲ de la v6.1.0 (un seul signe pour « pas conforme »)
	for i, r := range rows {
		if strings.Contains(r, "▲") {
			t.Errorf("ligne %d %q : ▲ conservé (remplacé par ⚠)", i, r)
		}
	}
	// modèle · effort conservés
	if !strings.Contains(rows[1], "sonnet") || !strings.Contains(rows[1], "med") {
		t.Errorf("ligne dev-a %q : modèle et effort attendus", rows[1])
	}
	// l'état reste en fin de ligne
	if !strings.Contains(rows[1], "actif") {
		t.Errorf("ligne dev-a %q : état « actif » attendu", rows[1])
	}
}

// Pane étroit : même règle (les versions sont dans le titre, jamais dans les lignes).
func TestTeamCardNarrowRowsHaveNoVersionColumn(t *testing.T) {
	m := teamModel(lot3Lines())
	m.w = 40
	for i, r := range strings.Split(m.teamBody(38), "\n") {
		for _, v := range []string{"v5.9.4", "v5.8.0", "2.1.87", "2.1.80"} {
			if strings.Contains(r, v) {
				t.Errorf("ligne %d %q : version %q dans la ligne", i, r, v)
			}
		}
	}
}

// ── #152 : @cl_state / @cl_name posées au tour du surveillant de mise en page ──

func paneOpt(t *testing.T, pane, opt string) string {
	t.Helper()
	out, _ := dtmux("show-options", "-pqv", "-t", pane, opt).Output()
	return strings.TrimSpace(string(out))
}

func waitPaneOpt(t *testing.T, pane, opt, want string, d time.Duration) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if paneOpt(t, pane, opt) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("pane %s : %s = %q après %v, attendu %q", pane, opt, paneOpt(t, pane, opt), d, want)
}

// Le launcher qui a la main pose @cl_state (lead | active | idle) et @cl_name sur chaque pane de la team,
// au tour du surveillant (1 s), et suit un changement d'état
// sans relance (≤ ~1 s : borne de 4 s pour une machine chargée).
func TestLayoutWatchSetsStateOptions(t *testing.T) {
	marker := isoInner(t)
	_, win, teamPath, proj, leader, mates := visProject(t, marker)
	// (pas de pane ouvert à la main ici : un pane hors team fait attendre le surveillant jusqu'à 8 s que la
	// team l'inscrive ; le pane sans agent est couvert par le test de l'interface native)
	done := make(chan struct{})
	go func() { defer close(done); RunLayoutWatch(win, leader, proj) }()
	t.Cleanup(func() { // avant le nettoyage d'isoInner : la fenêtre disparaît, le surveillant s'arrête
		_ = dtmux("kill-server").Run()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("le surveillant de mise en page ne s'est pas arrêté")
		}
	})

	// précondition (comportement existant) : le surveillant tourne — la teinte du pane actif diffère de celle
	// de l'inactif
	tinted := false
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if a, b := paneOpt(t, mates[0], "window-style"), paneOpt(t, mates[1], "window-style"); a != "" && b != "" && a != b {
			tinted = true
			break
		}
	}
	if !tinted {
		t.Fatal("précondition : le surveillant de mise en page n'a pas teinté les panes (il ne tourne pas ?)")
	}
	waitPaneOpt(t, leader, "@cl_state", "lead", 4*time.Second)
	waitPaneOpt(t, leader, "@cl_name", "team-lead", 4*time.Second)
	waitPaneOpt(t, mates[0], "@cl_state", "active", 4*time.Second)
	waitPaneOpt(t, mates[0], "@cl_name", "dev-a", 4*time.Second)
	waitPaneOpt(t, mates[1], "@cl_state", "idle", 4*time.Second)
	waitPaneOpt(t, mates[1], "@cl_name", "dev-b", 4*time.Second)

	// changement d'état : dev-a devient inactif, dev-b actif — reflété sans relance
	cfg := fmt.Sprintf(`{"members":[
	 {"name":"team-lead","agentType":"team-lead","cwd":%q},
	 {"name":"dev-a","agentType":"dev-a","tmuxPaneId":%q},
	 {"name":"dev-b","agentType":"dev-b","tmuxPaneId":%q,"isActive":true},
	 {"name":"dev-c","agentType":"dev-c","tmuxPaneId":%q}]}`, proj, mates[0], mates[1], mates[2])
	styleBefore := paneOpt(t, mates[0], "window-style")
	if err := os.WriteFile(teamPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// précondition (comportement existant) : la teinte suit le changement d'état
	flipped := false
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if paneOpt(t, mates[0], "window-style") != styleBefore {
			flipped = true
			break
		}
	}
	if !flipped {
		t.Fatal("précondition : la teinte du pane n'a pas suivi le changement d'état (fixture ou surveillant)")
	}
	waitPaneOpt(t, mates[0], "@cl_state", "idle", 4*time.Second)
	waitPaneOpt(t, mates[1], "@cl_state", "active", 4*time.Second)
	waitPaneOpt(t, leader, "@cl_state", "lead", 4*time.Second)
}
