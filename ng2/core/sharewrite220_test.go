package core

import (
	"fmt"
	"testing"
)

// #220 (LOT-1 v6.7.0) — option SHARE_WRITE_TIMEOUT : contrat contracts/desktop-dispatch.md § « v6.7.0 »
// (entier en secondes, défaut 10, bornes 2–120, rubrique « Projets », effet immédiat).
func TestShareWriteTimeoutOption220(t *testing.T) {
	m := ReadConfModel("")
	s := settingOf(m, "SHARE_WRITE_TIMEOUT")
	if s.Key != "SHARE_WRITE_TIMEOUT" {
		t.Fatal("option SHARE_WRITE_TIMEOUT absente du modèle de config.conf")
	}
	if s.Type != "int" {
		t.Errorf("type = %q, attendu int", s.Type)
	}
	if fmt.Sprint(s.Value) != "10" || fmt.Sprint(s.Default) != "10" {
		t.Errorf("défaut = %v (valeur %v), attendu 10", s.Default, s.Value)
	}
	if s.Min == nil || *s.Min != 2 || s.Max == nil || *s.Max != 120 {
		t.Errorf("bornes = %v–%v, attendu 2–120", s.Min, s.Max)
	}
	if s.Label != "Délai d'écriture sur le partage (s)" {
		t.Errorf("libellé = %q", s.Label)
	}
	if eff, _, ok := OptionEffect("SHARE_WRITE_TIMEOUT", IfaceDesktop); !ok || eff != EffectImmediate {
		t.Errorf("effet = %q (connu : %v), attendu immediate", eff, ok)
	}
}

// Hors bornes : refusé à l'enregistrement, comme les autres entiers bornés.
func TestShareWriteTimeoutBounds220(t *testing.T) {
	for _, v := range []int{1, 121} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{cset("SHARE_WRITE_TIMEOUT", v)}); err == nil {
			t.Errorf("SHARE_WRITE_TIMEOUT=%d accepté, attendu refus (bornes 2–120)", v)
		}
	}
	for _, v := range []int{2, 10, 120} {
		if _, err := ApplyConfConfChanges("", []ConfigChange{cset("SHARE_WRITE_TIMEOUT", v)}); err != nil {
			t.Errorf("SHARE_WRITE_TIMEOUT=%d refusé : %v", v, err)
		}
	}
}
