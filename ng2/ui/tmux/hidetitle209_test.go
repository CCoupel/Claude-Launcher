package tmux

import (
	"testing"

	"claude-launcher-ng2/core"
)

// #209 : titre de la carte TEAM de la coque Linux — état effectif du masquage, « ◆ » quand le projet a son propre choix.
func TestHideTitle209(t *testing.T) {
	for _, c := range []struct {
		h    core.HideIdleInfo
		want string
	}{
		{core.HideIdleInfo{}, ""}, // non renseigné
		{core.HideIdleInfo{Enabled: true, Delay: 30, Mode: "default", Global: true}, "default (masqués 30 s)"},
		{core.HideIdleInfo{Enabled: false, Delay: 30, Mode: "default"}, "default (désactivé)"},
		{core.HideIdleInfo{Enabled: false, Delay: 0, Mode: "default", Global: true}, "default (désactivé)"}, // délai 0 : jamais masqué
		{core.HideIdleInfo{Enabled: true, Delay: 30, Mode: "auto"}, "auto hide ◆"},
		{core.HideIdleInfo{Enabled: false, Delay: 30, Mode: "none", Global: true}, "no hide ◆"},
		{core.HideIdleInfo{Enabled: false, Delay: 0, Mode: "auto"}, "auto hide ◆"},
	} {
		if got := hideTitle(c.h); got != c.want {
			t.Errorf("%+v : %q, attendu %q", c.h, got, c.want)
		}
	}
}
