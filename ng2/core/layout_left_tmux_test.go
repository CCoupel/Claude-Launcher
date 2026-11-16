package core

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// « Leader à gauche » (#153) : la chaîne de mise en page est acceptée par un vrai tmux pour 0 à 10 teammates
// (avec et sans agent cdp), le leader occupe la colonne de gauche sur toute la hauteur, les teammates sont à
// droite : une colonne jusqu'à 4, deux à partir de 5 ; « top » rend la chaîne d'avant.
func TestLayoutLeftAcceptedByTmux(t *testing.T) {
	x := isolatedTmux(t)
	cfg := Config{Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2, Layout3RowDen: 7, LayoutMode: LayoutLeft, LayoutLeftLeaderPct: 46}
	for _, cdp := range []int{0, 1} {
		for n := 0; n <= 10; n++ {
			win := fmt.Sprintf("w%d_%d", cdp, n)
			if out, err := x.Output("new-window", "-d", "-P", "-F", "#{window_id}", "-n", win, "sleep 120"); err != nil {
				t.Fatalf("new-window : %v %s", err, out)
			} else {
				win = strings.TrimSpace(string(out))
			}
			for i := 0; i < n+cdp; i++ {
				if _, err := x.Output("split-window", "-d", "-t", win, "sleep 120"); err != nil {
					t.Fatalf("split : %v", err)
				}
				_ = x.Run("select-layout", "-t", win, "tiled")
			}
			out, _ := x.Output("list-panes", "-t", win, "-F", "#{pane_index}")
			var idx []int
			for _, f := range strings.Fields(string(out)) {
				v, _ := strconv.Atoi(f)
				idx = append(idx, v)
			}
			top, bot := idx[:1+cdp], idx[1+cdp:]
			s := LayoutString(cfg, 120, 40, top, bot)
			if err := x.Run("select-layout", "-t", win, s); err != nil {
				t.Fatalf("n=%d cdp=%d : tmux refuse %q : %v", n, cdp, s, err)
			}
			geo, _ := x.Output("list-panes", "-t", win, "-F", "#{pane_index} #{pane_left} #{pane_top} #{pane_width} #{pane_height}")
			rows := strings.Split(strings.TrimSpace(string(geo)), "\n")
			if len(rows) != len(idx) {
				t.Fatalf("n=%d : %d panes lus", n, len(rows))
			}
			leftEdge, maxRight := 0, 0
			cols := map[string]bool{}
			for i, r := range rows {
				var pi, px, py, pw, ph int
				fmt.Sscanf(r, "%d %d %d %d %d", &pi, &px, &py, &pw, &ph)
				if px+pw > maxRight {
					maxRight = px + pw
				}
				if i < 1+cdp { // leader (et cdp) : colonne de gauche (sans teammate : la rangée du haut d'avant)
					if px != leftEdge && (n > 0 || i == 0) {
						t.Errorf("n=%d cdp=%d : pane du haut %d en x=%d, attendu 0", n, cdp, pi, px)
					}
					if n > 0 && pw >= 60 {
						t.Errorf("n=%d : colonne du leader trop large (%d)", n, pw)
					}
				} else {
					cols[strconv.Itoa(px)] = true
					if px == 0 {
						t.Errorf("n=%d cdp=%d : teammate %d à gauche (x=0)", n, cdp, pi)
					}
				}
			}
			if maxRight != 120 {
				t.Errorf("n=%d cdp=%d : les panes couvrent %d colonnes sur 120", n, cdp, maxRight)
			}
			want := 0
			if n > 0 {
				want = LeftColumns(n)
			}
			if len(cols) != want {
				t.Errorf("n=%d cdp=%d : %d colonne(s) de teammates (%v), attendu %d", n, cdp, len(cols), cols, want)
			}
		}
	}
	// top : la chaîne actuelle, inchangée
	top := cfg
	top.LayoutMode = LayoutTop
	def := cfg
	def.LayoutMode = ""
	for n := 0; n <= 10; n++ {
		a, b := LayoutString(top, 120, 40, []int{0}, seq(1, n)), LayoutString(def, 120, 40, []int{0}, seq(1, n))
		if a != b {
			t.Errorf("n=%d : « top » diffère du défaut : %q %q", n, a, b)
		}
	}
}

func seq(from, n int) []int {
	var o []int
	for i := 0; i < n; i++ {
		o = append(o, from+i)
	}
	return o
}
