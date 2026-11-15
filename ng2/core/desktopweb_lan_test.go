package core

import (
	"strings"
	"testing"
)

// Décision utilisateur (audit sécurité LOT-4) : une écoute hors de la boucle locale exige
// `allow_lan: true` dans desktop_web ; sans lui, refusée avec un message qui dit quoi faire.
func TestDesktopWebLANNeedsExplicitAllow(t *testing.T) {
	for _, l := range []string{"0.0.0.0:8770", "192.168.1.5:8770", "[::]:8770"} {
		if _, err := resolveDesktopWebFor(dwFile(t, "defaults: { desktop_web: { listen: \""+l+"\" } }"), "PC", "linux"); err == nil || !strings.Contains(err.Error(), "allow_lan") {
			t.Errorf("%s sans allow_lan : %v, attendu un refus qui cite allow_lan", l, err)
		}
	}
	c, err := resolveDesktopWebFor(dwFile(t, "defaults: { desktop_web: { listen: \"0.0.0.0:8770\", allow_lan: true } }"), "PC", "linux")
	if err != nil || c.Off || c.Loopback || c.Warn == "" {
		t.Errorf("avec allow_lan : %+v %v, attendu actif, hors boucle locale, avec avertissement", c, err)
	}
	// la boucle locale n'a jamais besoin de allow_lan
	if c, err := resolveDesktopWebFor(dwFile(t, "defaults: { desktop_web: { listen: \"127.0.0.1:8770\" } }"), "PC", "linux"); err != nil || c.Off {
		t.Errorf("boucle locale : %+v %v", c, err)
	}
	// allow_lan se fusionne comme listen (machine > défauts)
	y := "defaults: { desktop_web: { listen: \"0.0.0.0:8770\", allow_lan: true } }\nhubs:\n  PC:\n    desktop_web: { allow_lan: false }\n"
	if _, err := resolveDesktopWebFor(dwFile(t, y), "PC", "linux"); err == nil {
		t.Error("allow_lan: false de la machine ignoré")
	}
}

func TestDesktopWebLANModelAndValidation(t *testing.T) {
	y := "hubs:\n  PC:\n    desktop_web: { listen: \"0.0.0.0:8770\", allow_lan: true }\n"
	m, err := ReadHubModel([]byte(y), "PC", "linux")
	if err != nil || !m.Settings.DesktopWeb.AllowLAN {
		t.Errorf("modèle : %+v %v, attendu allow_lan", m.Settings.DesktopWeb, err)
	}
	bad := "hubs:\n  PC:\n    desktop_web: { listen: \"0.0.0.0:8770\" }\n"
	refused := false
	for _, p := range validateBackends([]byte(bad), "linux", "PC") {
		refused = refused || p.Severity == SevError && strings.Contains(p.Message, "allow_lan")
	}
	if !refused {
		t.Error("la validation de backends.yaml n'a pas refusé l'écoute LAN sans allow_lan")
	}
}

// Refus au client web de toute modification du réglage : l'empreinte des réglages desktop_web.
func TestDesktopWebChanged(t *testing.T) {
	base := "defaults: { image: x }\nhubs:\n  PC:\n    desktop_web: { listen: \"8770\" }\n    windows: { desktop_web: { listen: \"8771\" } }\n"
	same := "# commentaire\ndefaults: { image: y }\nhubs:\n  PC:\n    desktop_web: { listen: \"8770\" }\n    windows: { desktop_web: { listen: \"8771\" } }\n"
	cases := []struct {
		name, to string
		want     bool
	}{
		{"autre réglage", same, false},
		{"port changé", strings.Replace(base, `"8770"`, `"8780"`, 1), true},
		{"allow_lan ajouté", strings.Replace(base, `{ listen: "8770" }`, `{ listen: "8770", allow_lan: true }`, 1), true},
		{"bloc supprimé", strings.Replace(base, "    desktop_web: { listen: \"8770\" }\n", "", 1), true},
		{"surcharge système changée", strings.Replace(base, `"8771"`, `"8799"`, 1), true},
		{"défauts ajoutés", "defaults: { desktop_web: { listen: off } }\n" + strings.TrimPrefix(base, "defaults: { image: x }\n"), true},
	}
	for _, c := range cases {
		if got := DesktopWebChanged([]byte(base), []byte(c.to)); got != c.want {
			t.Errorf("%s : %v, attendu %v", c.name, got, c.want)
		}
	}
}
