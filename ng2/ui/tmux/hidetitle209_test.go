package tmux

import (
	"testing"

	"claude-launcher-ng2/core"
)

// #209 : titre de la carte TEAM de la coque Linux — état effectif du masquage, « · projet » quand le projet a son propre choix.
func TestHideTitle209(t *testing.T) {
	for _, c := range []struct {
		h    core.HideIdleInfo
		want string
	}{
		{core.HideIdleInfo{}, ""}, // non renseigné
		{core.HideIdleInfo{Enabled: true, Delay: 30, Mode: "default", Global: true}, "masquage 30 s"},
		{core.HideIdleInfo{Enabled: false, Delay: 30, Mode: "default"}, "no hide"},
		{core.HideIdleInfo{Enabled: true, Delay: 30, Mode: "auto"}, "masquage 30 s · projet"},
		{core.HideIdleInfo{Enabled: false, Delay: 30, Mode: "none", Global: true}, "no hide · projet"},
		{core.HideIdleInfo{Enabled: false, Delay: 0, Mode: "auto"}, "no hide · projet"},
	} {
		if got := hideTitle(c.h); got != c.want {
			t.Errorf("%+v : %q, attendu %q", c.h, got, c.want)
		}
	}
}
