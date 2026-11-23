package desktop

import (
	"strings"
	"testing"
)

// #235 : la date de renouvellement de la consommation est colorée par resetLevel
// (red / yellow→orange / green) et l'alerte « ⚠ épuisé ~HH:MM » est affichée en rouge.
func TestUsageResetLevelAndAlert235(t *testing.T) {
	js, css := asset(t, "app.js"), asset(t, "style.css")
	for _, want := range []string{"r.resetLevel", "r.alert", "rst-red", "rst-orange", "rst-green", "rst-alert"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js : %q absent", want)
		}
	}
	for _, want := range []string{"#usage .rst-red { color: var(--red)", "#usage .rst-orange { color: var(--orange)", "#usage .rst-green { color: var(--green)", "#usage .rst-alert { color: var(--red)"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css : %q absent (couleurs via variables du thème)", want)
		}
	}
}
