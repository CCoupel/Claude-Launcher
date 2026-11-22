package core

import "testing"

// #223 (LOT-3b v6.7.0) — option ATTENTION_NOTIFY : booléen, rubrique « Teams et panes », libellé « Notifications d'attente »,
// effet immédiat, activée par défaut (décision GATE 2) ; désactivable.
func TestAttentionNotifyOption223(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir()) // jamais le config.conf réel
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := settingOf(ReadConfModel(""), "ATTENTION_NOTIFY")
	if s.Key != "ATTENTION_NOTIFY" {
		t.Fatal("option ATTENTION_NOTIFY absente du modèle")
	}
	if s.Type != "bool" || s.Group != "teams" || s.Label != "Notifications d'attente" {
		t.Errorf("option = type %q rubrique %q libellé %q", s.Type, s.Group, s.Label)
	}
	if s.Value != true || s.Default != true {
		t.Errorf("défaut = %v (valeur %v), attendu activé", s.Default, s.Value)
	}
	if eff, _, ok := OptionEffect("ATTENTION_NOTIFY", IfaceDesktop); !ok || eff != EffectImmediate {
		t.Errorf("effet = %q (%v), attendu immediate", eff, ok)
	}
	if !LoadConfig().AttentionNotify {
		t.Error("LoadConfig sans config.conf : AttentionNotify doit valoir vrai")
	}
	m := ReadConfModel("ATTENTION_NOTIFY=0\n")
	if settingOf(m, "ATTENTION_NOTIFY").Value != false {
		t.Errorf("ATTENTION_NOTIFY=0 relu %v", settingOf(m, "ATTENTION_NOTIFY").Value)
	}
}
