package core

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func panes5() []WinPane {
	var out []WinPane
	for i, id := range []string{"%1", "%2", "%3", "%4", "%5"} {
		out = append(out, WinPane{ID: id, Index: i})
	}
	return out
}

// Non-régression (QA, #153) : un classement qui cite PLUS de panes que la fenêtre n'en a (teammate fermé pendant le tour)
// ne fait pas paniquer PlanArrange (« slice bounds out of range [:6] with capacity 5 »).
func TestPlanArrangeClassementPlusLargeQueLesPanes(t *testing.T) {
	cfg := LoadConfig()
	for name, tc := range map[string]struct{ top, bot []string }{
		"want > panes":             {[]string{"%1", "%2", "%3"}, []string{"%4", "%5", "%6"}},
		"ids absents partout":      {[]string{"%9", "%8"}, []string{"%7", "%6", "%1"}},
		"classement vide":          {nil, nil},
		"doublons top/bot":         {[]string{"%1", "%2"}, []string{"%2", "%3", "%1"}},
		"tout absent":              {[]string{"%x"}, []string{"%y"}},
		"top seul trop long":       {[]string{"%1", "%2", "%3", "%4", "%5", "%6", "%7"}, nil},
		"bot seul, top inconnu":    {[]string{"%unknown"}, []string{"%2", "%3"}},
		"panes disparus (reste 1)": {[]string{"%1"}, []string{"%2"}},
	} {
		for _, panes := range [][]WinPane{panes5(), panes5()[:1], nil} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s (%d panes) : panic %v", name, len(panes), r)
					}
				}()
				plan := PlanArrange(cfg, "@1", panes, 200, 50, tc.top, tc.bot, "17")
				// aucun swap ne vise un pane inexistant (un id inconnu n'est pas échangé avec la position 0)
				exist := map[string]bool{}
				for _, p := range panes {
					exist[p.ID] = true
				}
				for _, c := range plan.Cmds {
					if len(c) >= 6 && c[0] == "swap-pane" && (!exist[c[3]] || !exist[c[5]]) {
						t.Errorf("%s : swap vers un pane inexistant : %v", name, c)
					}
				}
			}()
		}
	}
	// cas nominal inchangé : classement complet et ordonné
	plan := PlanArrange(cfg, "@1", panes5(), 200, 50, []string{"%2", "%1"}, []string{"%3", "%4", "%5"}, "17")
	if len(plan.Cmds) == 0 || plan.Cmds[len(plan.Cmds)-1][0] == "" {
		t.Errorf("plan nominal vide : %v", plan.Cmds)
	}
	swaps := 0
	for _, c := range plan.Cmds {
		if c[0] == "swap-pane" {
			swaps++
		}
	}
	if swaps != 1 { // %2 passe en tête : un seul échange
		t.Errorf("swaps nominaux = %d, attendu 1 : %v", swaps, plan.Cmds)
	}
}

func TestRecoverLogConsigneLePanic(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	func() {
		defer RecoverLog("test")
		var s []int
		_ = s[5] // panic
	}()
	if out := buf.String(); !strings.Contains(out, "PANIC récupéré (test)") || !strings.Contains(out, "index out of range") {
		t.Errorf("panic non consigné : %q", out)
	}
}

func TestRunProtectedRelanceApresPanic(t *testing.T) {
	old := restartPause
	restartPause = time.Millisecond
	defer func() { restartPause = old }()
	n := 0
	ok := RunProtected("test", 5, func() {
		n++
		if n < 3 {
			panic("boum")
		}
	})
	if !ok || n != 3 {
		t.Errorf("ok=%v essais=%d, attendu relance jusqu'au 3e essai", ok, n)
	}
	n = 0
	if RunProtected("test", 2, func() { n++; panic("toujours") }) || n != 3 {
		t.Errorf("panic permanent : %d essais, attendu 3 (1 + 2 relances) puis abandon", n)
	}
}
