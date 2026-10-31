package main

import (
	"testing"
	"time"
)

func TestMilestoneState(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	closed := func(title string, d int) rawMilestone {
		return rawMilestone{Title: title, State: "closed", Closed: 3, ClosedAt: t0.AddDate(0, 0, d)}
	}
	open := func(title string, o, c int) rawMilestone {
		return rawMilestone{Title: title, State: "open", Open: o, Closed: c}
	}
	cases := []struct {
		name  string
		ms    []rawMilestone
		kind  MsKind
		title string
	}{
		{"aucun", nil, MsNone, ""},
		{"publié : dernier clos", []rawMilestone{closed("v1.0.0", 5), closed("v0.9.0", 1)}, MsDone, "v1.0.0"},
		{"en cours, même sans issue fermée", []rawMilestone{closed("v1.0.0", 5), open("v1.1.0", 1, 0)}, MsWorking, "v1.1.0"},
		{"en cours : la plus petite version", []rawMilestone{open("v1.10.0", 2, 1), open("v1.9.0", 4, 0)}, MsWorking, "v1.9.0"},
		{"à publier passe devant en cours", []rawMilestone{open("v1.2.0", 3, 0), open("v1.1.0", 0, 4)}, MsReleasing, "v1.1.0"},
		{"backlog sans version ignoré", []rawMilestone{closed("v1.0.0", 5), open("v1.x", 7, 0)}, MsDone, "v1.0.0"},
		{"x dans le titre, pas dans le numéro", []rawMilestone{open("v2.4.4 — Versioning X.Y.Z.a", 1, 0)}, MsWorking, "v2.4.4 — Versioning X.Y.Z.a"},
		{"milestone ouvert vide ignoré", []rawMilestone{open("v2.0.0", 0, 0)}, MsNone, ""},
	}
	for _, c := range cases {
		if st := milestoneState(c.ms); st.Kind != c.kind || st.Title != c.title {
			t.Errorf("%s : %+v", c.name, st)
		}
	}
	if st := milestoneState([]rawMilestone{open("v1.1.0", 2, 3)}); st.Open != 2 || st.Closed != 3 {
		t.Errorf("compteurs : %+v", st)
	}
}

func TestLessMilestone(t *testing.T) {
	a := rawMilestone{Title: "v1.2.0", DueOn: "2026-12-01T00:00:00Z"}
	b := rawMilestone{Title: "v1.1.0"}
	if !lessMilestone(a, b) {
		t.Error("échéance d'abord")
	}
	if !lessMilestone(rawMilestone{Title: "v1.9"}, rawMilestone{Title: "v1.10"}) {
		t.Error("v1.9 avant v1.10")
	}
}
