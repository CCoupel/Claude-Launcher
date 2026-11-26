package core

import (
	"strings"
	"testing"
)

// #253 cycle 1 : jamais de racine de volume montée entière ; racines en double ambiguës refusées.
func TestK8s253RefuseRacineDeVolume(t *testing.T) {
	b := t253K8s(map[string]string{t253Root: "claude-projets"})
	for name, c := range map[string]struct{ proj, cfg string }{
		"CLAUDE_CONFIG_DIR = racine": {t253Proj, t253Root},
		"projet = racine":            {t253Root, t253Cfg},
		"racine avec « / » final":    {t253Proj, t253Root + "/"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := t253Manifest(t, b, c.proj, c.cfg, EnvSpec{})
			if err == nil || !strings.Contains(err.Error(), "stockage partagé") || !strings.Contains(err.Error(), "racine") {
				t.Errorf("racine montée entière acceptée ou erreur floue : %v", err)
			}
		})
	}
}

func TestK8s253RacinesIdentiquesPVCDifferents(t *testing.T) {
	b := t253K8s(map[string]string{t253Root: "pvc-a", t253Root + "/": "pvc-b"})
	_, err := t253Manifest(t, b, t253Proj, t253Cfg, EnvSpec{})
	if err == nil || !strings.Contains(err.Error(), "deux PVC") {
		t.Errorf("racines identiques avec PVC différents : %v", err)
	}
	// même PVC deux fois : sans ambiguïté
	b = t253K8s(map[string]string{t253Root: "pvc-a", t253Root + "/": "pvc-a"})
	if _, err := t253Manifest(t, b, t253Proj, t253Cfg, EnvSpec{}); err != nil {
		t.Errorf("même PVC sous deux écritures : %v", err)
	}
}
