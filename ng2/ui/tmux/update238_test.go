package tmux

// #238 (v6.8.1) : la coque utilise la même fonction de cycle que la fenêtre native (core.UpdateCheckEvery /
// NextCheckDelay), plus de valeurs en dur (10 min / 2 min). Garde de source : le délai rendu par tea.Cmd n'est pas
// observable sans tmux, les valeurs elles-mêmes sont testées dans core (updatecheck238_test.go).

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCoqueUtiliseLeCycleCommun238(t *testing.T) {
	b, err := os.ReadFile("nav.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"core.UpdateCheckEvery(", "core.NextCheckDelay(", "core.CheckUpdatesEvery("} {
		if !strings.Contains(src, want) {
			t.Errorf("nav.go : %s absent — la coque doit partager le cycle de la fenêtre native", want)
		}
	}
	// aucune durée en dur dans la chaîne de vérification des versions (UpdTickCmd / CheckUpdates*)
	hard := regexp.MustCompile(`UpdTickCmd\([^)]*(10|2|5)\s*\*\s*time\.(Minute|Second)`)
	if m := hard.FindString(src); m != "" {
		t.Errorf("nav.go : délai en dur dans %q", m)
	}
}
