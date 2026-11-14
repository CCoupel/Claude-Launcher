package core

import (
	"strings"
	"testing"
)

// #136 — état partagé entre launchers : tout est dans tmux (options de pane et
// de fenêtre), donc un launcher qui prend la main reprend sans perte ce qu'un
// autre a posé : début d'inactivité, choix manuels, panes cachés (contrat :
// @cl_idle_at « reste valable si la main change de launcher »).
// Chaque « launcher » est ici un appelant sans mémoire (mêmes fonctions de core).
func TestHandoverBetweenLaunchers(t *testing.T) {
	i, win, home, leader, p, tc := visFixture(t)
	b, c := p["b"], p["c"]

	// launcher A voit b et c inactifs à t=1000 et épingle c visible
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1000, false); err != nil {
		t.Fatal(err)
	}
	if r := TogglePane(i.run, win, home, tc, c, false); !r.OK { // visible → caché à la main
		t.Fatal(r.Msg)
	}
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1001, false); err != nil {
		t.Fatal(err)
	}
	if r := TogglePane(i.run, win, home, tc, c, false); !r.OK { // caché → fixé visible
		t.Fatal(r.Msg)
	}
	if _, err := ApplyPaneVis(i.run, win, home, tc, 30, 1002, false); err != nil {
		t.Fatal(err)
	}

	// launcher B prend la main (aucune mémoire) : il lit le même état
	panes, choices, err := ProjectPanes(i.run, win, home, false)
	if err != nil {
		t.Fatal(err)
	}
	if choices[c] != "s" || len(choices) != 1 {
		t.Fatalf("choix lus par B : %v", choices)
	}
	for _, q := range panes {
		if q.ID == b && q.IdleAt != 1000 {
			t.Fatalf("début d'inactivité de b lu par B : %d, attendu 1000 (posé par A)", q.IdleAt)
		}
	}
	// B applique à t=1030 : le délai court depuis A → b caché, c (fixé) reste visible
	r, err := ApplyPaneVis(i.run, win, home, tc, 30, 1030, false)
	if err != nil || len(r.Hid) != 1 || r.Hid[0] != b {
		t.Fatalf("B : %+v %v", r, err)
	}
	// puis A revient et lit : b caché dans la réserve, rattaché au projet, idle_at conservé
	panes, choices, _ = ProjectPanes(i.run, win, home, false)
	var hidden []string
	for _, q := range panes {
		if q.Hidden {
			hidden = append(hidden, q.ID)
			if q.IdleAt != 1000 {
				t.Errorf("idle_at de %s perdu dans la réserve : %d", q.ID, q.IdleAt)
			}
		}
	}
	if len(hidden) != 1 || hidden[0] != b || choices[c] != "s" {
		t.Fatalf("A relit : cachés %v, choix %v", hidden, choices)
	}
	if all := i.must(t, "list-panes", "-a", "-F", "#{pane_id}"); !strings.Contains(all, leader) || !strings.Contains(all, b) || !strings.Contains(all, c) {
		t.Fatalf("pane perdu pendant la passation : %q", all)
	}
	// B quitte : le teammate b redevient actif, A (nouvelle main) le ramène sans autre état
	tc.Members[2].IsActive = true
	if r, err := ApplyPaneVis(i.run, win, home, tc, 30, 1040, false); err != nil || len(r.Shown) != 1 || r.Shown[0] != b {
		t.Fatalf("retour par A : %+v %v", r, err)
	}
	if got := i.must(t, "show-options", "-pqv", "-t", b, optIdleAt); got != "" {
		t.Errorf("@cl_idle_at non retiré au retour à l'activité : %q", got)
	}
}
