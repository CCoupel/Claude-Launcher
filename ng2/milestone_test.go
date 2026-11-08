package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"reflect"
	"strings"
	"testing"
	"time"
)

func issue(ms string, labels ...string) rawIssue {
	var i rawIssue
	if ms != "" {
		i.Milestone = &struct {
			Title string `json:"title"`
		}{ms}
	}
	for _, l := range labels {
		i.Labels = append(i.Labels, struct {
			Name string `json:"name"`
		}{l})
	}
	return i
}

func TestMilestoneState(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	closed := func(title string, d int) rawMilestone {
		return rawMilestone{Title: title, State: "closed", Closed: 3, ClosedAt: t0.AddDate(0, 0, d)}
	}
	open := func(title string, o, c int) rawMilestone {
		return rawMilestone{Title: title, State: "open", Open: o, Closed: c}
	}
	dated := func(title string, o, c int, due string) rawMilestone {
		m := open(title, o, c)
		m.DueOn = due
		return m
	}
	cases := []struct {
		name   string
		ms     []rawMilestone
		issues []rawIssue
		kind   MsKind
		title  string
	}{
		{"aucun", nil, nil, MsNone, ""},
		{"publié : dernier clos", []rawMilestone{closed("v1.0.0", 5), closed("v0.9.0", 1)}, nil, MsDone, "v1.0.0"},
		// cas Ansible_SecAgent : issues ouvertes sans label de phase → le panneau dit « aucun en cours »
		{"issues ouvertes sans phase : en attente",
			[]rawMilestone{closed("v3.0.3", 1), open("v3.0.4 — Confiance", 1, 10), open("v3.0.5 — Durcissement", 23, 0)},
			[]rawIssue{issue("v3.0.4 — Confiance", "bug"), issue("v3.0.5 — Durcissement", "status:todo")},
			MsIdle, ""},
		{"en cours : issue avec phase",
			[]rawMilestone{open("v1.1.0", 2, 5)},
			[]rawIssue{issue("v1.1.0", "EN COURS"), issue("v1.1.0", "bug")}, MsWorking, "v1.1.0"},
		{"en cours : le premier par version, pas le plus avancé",
			[]rawMilestone{open("v1.10.0", 2, 1), open("v1.9.0", 4, 0)},
			[]rawIssue{issue("v1.10.0", "PRET"), issue("v1.9.0", "EN COURS")}, MsWorking, "v1.9.0"},
		// cas BuzzMaster : v11.5.0 n'a que des issues en PLANNING, pas de due date → non démarré
		{"PLANNING seul, sans due date : en attente",
			[]rawMilestone{open("v11.5.0 — Optimisation des tests", 4, 0)},
			[]rawIssue{issue("v11.5.0 — Optimisation des tests", "PLANNING")}, MsIdle, ""},
		{"PLANNING ne démarre pas, le suivant en PRET oui",
			[]rawMilestone{open("v1.9.0", 4, 0), open("v1.10.0", 2, 1)},
			[]rawIssue{issue("v1.9.0", "PLANNING"), issue("v1.10.0", "PRET")}, MsWorking, "v1.10.0"},
		{"due date : en cours même sans phase, devant un milestone en PRET",
			[]rawMilestone{dated("v1.2.0", 3, 1, "2026-11-01T00:00:00Z"), open("v1.1.0", 2, 0)},
			[]rawIssue{issue("v1.1.0", "PRET")}, MsWorking, "v1.2.0"},
		{"due date : la plus ancienne",
			[]rawMilestone{dated("v1.1.0", 3, 1, "2026-11-05T00:00:00Z"), dated("v1.2.0", 3, 0, "2026-11-01T00:00:00Z")},
			nil, MsWorking, "v1.2.0"},
		{"due date toutes fermées : à publier, même si le suivant est en PLANNING",
			[]rawMilestone{dated("v1.1.0", 0, 4, "2026-11-01T00:00:00Z"), open("v1.2.0", 3, 0)},
			[]rawIssue{issue("v1.2.0", "PLANNING")}, MsReleasing, "v1.1.0"},
		{"en cours passe devant à publier (comme le panneau)",
			[]rawMilestone{open("v1.1.0", 0, 4), open("v1.2.0", 3, 0)},
			[]rawIssue{issue("v1.2.0", "EN COURS")}, MsWorking, "v1.2.0"},
		{"à publier : toutes fermées, rien de démarré",
			[]rawMilestone{open("v1.1.0", 0, 4), open("v1.2.0", 3, 0)},
			[]rawIssue{issue("v1.2.0")}, MsReleasing, "v1.1.0"},
		{"phase sans milestone ignorée", []rawMilestone{open("v2.0.0", 1, 0)}, []rawIssue{issue("", "EN COURS")}, MsIdle, ""},
	}
	for _, c := range cases {
		if st := milestoneState(c.ms, c.issues); st.Kind != c.kind || st.Title != c.title {
			t.Errorf("%s : %+v", c.name, st)
		}
	}

	// détail du milestone en cours : phases dans l'ordre du panneau, bloquées
	st := milestoneState([]rawMilestone{open("v1.1.0", 4, 6)}, []rawIssue{
		issue("v1.1.0", "EN REVIEW"), issue("v1.1.0", "PRET", "BLOQUE"), issue("v1.1.0", "PRET"), issue("v1.1.0", "bug")})
	want := []MsPhase{{"PRET", 2}, {"EN REVIEW", 1}, {msNoPhase, 1}}
	if !reflect.DeepEqual(st.Phases, want) || st.Blocked != 1 || st.Open != 4 || st.Closed != 6 {
		t.Errorf("détail : %+v", st)
	}
	if l := strings.Join(msLines(st, 40), "\n"); !strings.Contains(l, "60%") || !strings.Contains(l, "6/10") || !strings.Contains(l, "pret 2") {
		t.Errorf("msLines=%q", l)
	}

	// en attente : ordre du panneau (sans version en dernier, avancement décroissant)
	st = milestoneState([]rawMilestone{open("v1.x", 1, 9), open("v1.2.0", 3, 1), open("v1.3.0", 1, 3)}, nil)
	var got []string
	for _, r := range st.Pending {
		got = append(got, r.Title)
	}
	if !reflect.DeepEqual(got, []string{"v1.3.0", "v1.2.0", "v1.x"}) {
		t.Errorf("ordre en attente : %v", got)
	}
}

func TestCompareTitles(t *testing.T) {
	if compareTitles("v1.9", "v1.10") >= 0 || compareTitles("v3.0.4 — Confiance", "v3.0.5 — X.Y") >= 0 {
		t.Error("ordre des versions")
	}
	if !lessMilestone(rawMilestone{Title: "v1.2.0", DueOn: "2026-12-01T00:00:00Z"}, rawMilestone{Title: "v1.1.0"}) {
		t.Error("échéance d'abord")
	}
	if !unversioned("v2.x — backlog") || unversioned("v2.4.4 — Versioning X.Y.Z.a") {
		t.Error("unversioned")
	}
}

func TestPRIndicator(t *testing.T) {
	if _, ok := prIndicator(MsState{}); ok {
		t.Error("badge PR sans PR ouverte")
	}
	n, d := countPRs([]rawPR{{false}, {true}, {false}})
	i, ok := prIndicator(MsState{PRs: n, Drafts: d})
	if !ok || i.icon != "⇄3" || !strings.Contains(i.text, "3 pull request") || !strings.Contains(i.text, "dont 1 brouillon") {
		t.Errorf("prIndicator=%+v ok=%v", i, ok)
	}
	ins := badgeIndicators(projInfo{ms: MsState{Kind: MsWorking, PRs: 2}})
	if len(ins) != 2 || ins[0].icon != "⚑" || ins[1].icon != "⇄2" {
		t.Errorf("ordre des badges=%+v", ins)
	}
}

// Barres d'avancement : à la largeur du pane (10 à 40 cellules), sans la dépasser.
func TestMsBarWidth(t *testing.T) {
	cells := func(s string) int { return strings.Count(s, "█") + strings.Count(s, "░") }
	r := MsRow{Open: 2, Closed: 0}
	for _, c := range []struct {
		width int
		title string
		want  int
	}{{52, "v5.4.1", 36}, {120, "v5.4.1", 40}, {30, "", 21}, {20, "un titre très long", 10}} {
		b := msBar(r, c.width, c.title)
		if n := cells(b); n != c.want {
			t.Errorf("largeur %d, titre %q : %d cellules, attendu %d", c.width, c.title, n, c.want)
		}
		if c.want > msBarMin && lipgloss.Width(b) > c.width {
			t.Errorf("largeur %d dépassée : %d", c.width, lipgloss.Width(b))
		}
	}
	// liste (cas BuzzMaster) : « titre n/m barre P% », colonnes alignées ; quand
	// la largeur manque : sans barre, puis titre tronqué, puis sans P%
	idle := MsState{Kind: MsIdle, Pending: []MsRow{
		{Title: "v15.0 — Éclairage ambiance — Extensions", Open: 4, Closed: 1},
		{Title: "v12.0 — Roue de la Fortune", Open: 2},
		{Title: "v11.6.0 — Ambiance de musique d'événement — Extensions", Open: 14}}}
	strip := func(w int) []string {
		var o []string
		for _, l := range msLines(idle, w)[1:] {
			if lipgloss.Width(l) > w {
				t.Errorf("largeur %d dépassée : %q", w, ansi.Strip(l))
			}
			o = append(o, ansi.Strip(l))
		}
		return o
	}
	if l := strip(100); cells(l[0]) < msBarMin || !strings.HasSuffix(l[0], " 20%") || !strings.HasPrefix(l[0], "v15.0 — Éclairage ambiance — Extensions ") {
		t.Errorf("complet : %q", l)
	}
	if l := strip(68); cells(l[0]) != 0 || !strings.HasSuffix(l[2], "Extensions 0/14   0%") {
		t.Errorf("sans barre : %q", l)
	}
	if l := strip(40); cells(l[0]) != 0 || !strings.HasSuffix(l[2], "… 0/14   0%") {
		t.Errorf("titre tronqué : %q", l)
	}
	if l := strip(17); l[2] != "v11.6.0 0/14   0%" || l[0] != "v15.0    1/5  20%" {
		t.Errorf("version jamais coupée : %q", l)
	}
	if l := strip(13); !reflect.DeepEqual(l, []string{"v15.0     1/5", "v12.0     0/2", "v11.6.0  0/14"}) {
		t.Errorf("au plus étroit : %q", l)
	}
}
