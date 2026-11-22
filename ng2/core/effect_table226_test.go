package core

import (
	"strings"
	"testing"
)

// Couverture : chaque réglage connu du modèle de config.conf a un effet (pas de trou) pour les deux interfaces, et la table ne
// contient aucune clé inconnue du modèle.
func TestTableDesEffetsCouvreLeModele226(t *testing.T) {
	model := map[string]bool{}
	for _, s := range confSettings {
		model[s.Key] = true
		for _, iface := range []string{IfaceDesktop, IfaceShell} {
			eff, how, ok := OptionEffect(s.Key, iface)
			if !ok || eff == "" || strings.TrimSpace(how.Why) == "" {
				t.Errorf("%s (%s) : effet manquant (%q %+v)", s.Key, iface, eff, how)
			}
		}
	}
	for _, k := range OptionKeys() {
		if !model[k] {
			t.Errorf("clé %s de la table absente du modèle", k)
		}
	}
	if _, _, ok := OptionEffect("INCONNUE", IfaceDesktop); ok {
		t.Error("clé inconnue : pas de pastille attendue")
	}
	if e, _, _ := OptionEffect("COLOR_PALETTE", IfaceShell); e != EffectProject {
		t.Errorf("palette dans la coque = %s, attendu project", e)
	}
}

func TestChangedOptionsValeurEffective226(t *testing.T) {
	old := "CLAUDE_DISABLE_MOUSE=1\nTEAM_HIDE_IDLE=30\n"
	if c := ChangedOptions(old, old+"# commentaire\n", IfaceDesktop); len(c) != 0 {
		t.Errorf("commentaire : %v", c)
	}
	if c := ChangedOptions(old, "TEAM_HIDE_IDLE=30\nCLAUDE_DISABLE_MOUSE=1\nUSAGE_REFRESH_SEC=120\n", IfaceDesktop); len(c) != 0 {
		t.Errorf("ordre / valeur égale au défaut : %v", c)
	}
	c := ChangedOptions(old, "CLAUDE_DISABLE_MOUSE=0\nTEAM_HIDE_IDLE=30\nGITHUB_DIR=/x\n", IfaceDesktop)
	if len(c) != 2 || !NeedsRelaunch(c) {
		t.Errorf("changements = %v (souris + GITHUB_DIR, relance attendue)", c)
	}
	// le jeton se compare sur sa valeur, sans jamais la rendre
	a, b := "GITHUB_TOKEN=ghp_FAKE_TEST_aaaaaaaa\n", "GITHUB_TOKEN=ghp_FAKE_TEST_bbbbbbbb\n"
	c = ChangedOptions(a, b, IfaceDesktop)
	if len(c) != 1 || c[0].Key != "GITHUB_TOKEN" || c[0].Effect != EffectClaude {
		t.Errorf("jeton : %v", c)
	}
	if n := ApplyNote(c, IfaceShell); strings.Contains(n, "FAKE_TEST") || !strings.Contains(n, "F5") {
		t.Errorf("note : %q", n)
	}
}

// Garde-fou #227 : toute entrée de type « secret » du modèle est révélable ET lue par RevealSecret. Une 2ᵉ clé secrète ajoutée
// au modèle sans être branchée à RevealSecret fait échouer ce test (l'œil n'afficherait jamais rien, ou on ne saurait pas quoi lire).
func TestToutSecretDuModeleEstRevelable227(t *testing.T) {
	n := 0
	for _, s := range confSettings {
		if s.Type != "secret" {
			continue
		}
		n++
		if !RevealableKey(ConfConf, s.Key) {
			t.Errorf("%s : secret du modèle mais pas révélable", s.Key)
		}
		conf, _ := confBench225(t)
		editConf(t, conf, s.Key+"=ghp_FAKE_TEST_garde227\n", 0)
		if v, err := RevealSecret(ConfConf, s.Key); err != nil || v == "" {
			t.Errorf("%s : RevealSecret ne lit pas ce secret (%q, %v) — brancher la clé dans reveal.go", s.Key, v, err)
		}
	}
	if n == 0 {
		t.Fatal("aucun secret dans le modèle : garde-fou sans objet")
	}
	for _, s := range confSettings {
		if s.Type != "secret" && RevealableKey(ConfConf, s.Key) {
			t.Errorf("%s : révélable sans être de type secret", s.Key)
		}
	}
}
