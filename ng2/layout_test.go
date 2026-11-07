package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Valeurs de référence : brighten_color / darken_color du script bash v3.
var bashColors = []struct {
	c    string
	step int
	b, d string
}{
	{"17", 1, "18", "16"},
	{"52", 1, "88", "16"},
	{"22", 1, "28", "16"},
	{"53", 1, "90", "16"},
	{"58", 1, "100", "16"},
	{"23", 1, "30", "16"},
	{"54", 1, "91", "17"},
	{"238", 1, "240", "236"},
	{"18", 1, "19", "17"},
	{"59", 1, "102", "16"},
	{"16", 1, "16", "16"},
	{"231", 1, "231", "188"},
	{"232", 1, "234", "232"},
	{"255", 1, "255", "253"},
	{"7", 1, "7", "7"},
	{"17", 2, "19", "16"},
	{"52", 2, "124", "16"},
	{"22", 2, "34", "16"},
	{"53", 2, "127", "16"},
	{"58", 2, "142", "16"},
	{"23", 2, "37", "16"},
	{"54", 2, "128", "16"},
	{"238", 2, "242", "234"},
	{"18", 2, "20", "16"},
	{"59", 2, "145", "16"},
	{"16", 2, "16", "16"},
	{"231", 2, "231", "145"},
	{"232", 2, "236", "232"},
	{"255", 2, "255", "251"},
	{"7", 2, "7", "7"},
	{"17", 5, "21", "16"},
	{"52", 5, "196", "16"},
	{"22", 5, "46", "16"},
	{"53", 5, "201", "16"},
	{"58", 5, "226", "16"},
	{"23", 5, "51", "16"},
	{"54", 5, "201", "16"},
	{"238", 5, "248", "232"},
	{"18", 5, "21", "16"},
	{"59", 5, "231", "16"},
	{"16", 5, "16", "16"},
	{"231", 5, "231", "16"},
	{"232", 5, "242", "232"},
	{"255", 5, "255", "245"},
	{"7", 5, "7", "7"},
}

func TestShiftColorMatchesBash(t *testing.T) {
	for _, c := range bashColors {
		if got := brightenColor(c.c, c.step); got != c.b {
			t.Errorf("brighten(%s, %d) = %s, bash %s", c.c, c.step, got, c.b)
		}
		if got := darkenColor(c.c, c.step); got != c.d {
			t.Errorf("darken(%s, %d) = %s, bash %s", c.c, c.step, got, c.d)
		}
	}
}

func TestLayoutChecksum(t *testing.T) {
	// relevé sur un vrai tmux 3.4
	const real = "ce52,120x40,0,0[120x20,0,0{60x20,0,0,0,59x20,61,0,2},120x19,0,21,1]"
	body := real[5:]
	if got := layoutChecksum(body); got != real[:4] {
		t.Fatalf("checksum = %s, tmux %s", got, real[:4])
	}
}

func TestLayoutString(t *testing.T) {
	cfg := Config{Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2, Layout3RowDen: 7}
	// attendus : sortie de build_row / do_layout du script bash v3
	cases := []struct {
		top, bot []int
		want     string
	}{
		{[]int{0}, nil, "120x40,0,0,0"},
		{[]int{0}, []int{1, 2}, "120x40,0,0[120x20,0,0,0,120x19,0,21{59x19,0,21,1,60x19,60,21,2}]"},
		{[]int{0}, []int{1, 2, 3, 4, 5, 6}, "120x40,0,0[120x17,0,0,0,120x11,0,18{39x11,0,18,1,39x11,40,18,2,40x11,80,18,3},120x10,0,30{39x10,0,30,4,39x10,40,30,5,40x10,80,30,6}]"},
	}
	for _, c := range cases {
		got := layoutString(cfg, 120, 40, c.top, c.bot)
		if got[5:] != c.want || got[:4] != layoutChecksum(c.want) {
			t.Errorf("layout %v/%v =\n %s\nattendu\n %s", c.top, c.bot, got, c.want)
		}
	}
}

func TestClassifyMRU(t *testing.T) {
	ws := winState{panes: []winPane{{id: "%0"}, {id: "%1"}, {id: "%2"}, {id: "%3"}, {id: "%4"}, {id: "%9"}}}
	order := filepath.Join(t.TempDir(), "order")
	team := func(active ...string) *teamConfig {
		tc := &teamConfig{Members: []teamMember{
			{Name: "team-lead"},
			{Name: "cdp", AgentType: "cdp", PaneID: "%1"},
			{Name: "dev-b", PaneID: "%2"},
			{Name: "dev-a", PaneID: "%3"},
			{Name: "qa", PaneID: "%4"},
			{Name: "parti", PaneID: "%7"}, // pane fermé
		}}
		for i := range tc.Members {
			for _, a := range active {
				if tc.Members[i].PaneID == a {
					tc.Members[i].IsActive = true
				}
			}
		}
		return tc
	}
	check := func(tc *teamConfig, wantTop, wantBot string) {
		t.Helper()
		top, bot := classify(ws, "%0", tc, order)
		if g := strings.Join(top, " "); g != wantTop {
			t.Errorf("top = %s, attendu %s", g, wantTop)
		}
		if g := strings.Join(bot, " "); g != wantBot {
			t.Errorf("bot = %s, attendu %s", g, wantBot)
		}
	}
	// aucun actif : par nom, puis le pane manuel %9
	check(team(), "%0 %1", "%3 %2 %4 %9")
	// qa devient actif : en tête
	check(team("%4"), "%0 %1", "%4 %3 %2 %9")
	// dev-b devient actif à son tour : passe devant qa
	check(team("%4", "%2"), "%0 %1", "%2 %4 %3 %9")
	// qa redevient inactif : retrouve sa place par nom
	check(team("%2"), "%0 %1", "%2 %3 %4 %9")
	// sans team : leader seul en haut
	check(nil, "%0", "%1 %2 %3 %4 %9")
	_ = os.Remove(order)
}
