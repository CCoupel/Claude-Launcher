package testguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// La garde voit une écriture, une modification et une suppression dans le dossier surveillé.
func TestTakeDetectsChanges(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, ".template-version")
	a := take(d)
	if err := os.WriteFile(p, []byte("v5.9.2"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := take(d)
	if a[".template-version"] == b[".template-version"] {
		t.Error("création non détectée")
	}
	_ = os.WriteFile(p, []byte("v5.9.3"), 0o644)
	if c := take(d); c[".template-version"] == b[".template-version"] {
		t.Error("modification non détectée")
	}
	_ = os.Remove(p)
	if c := take(d); c[".template-version"] != a[".template-version"] {
		t.Error("suppression : retour à l'état initial attendu")
	}
}

// Un jeton factice apparu dans un settings*.json réel est vu ; un fichier qui le portait déjà avant la suite non ; un
// dossier temporaire sans marqueur non plus.
func TestNewLeaksDetecteUnJetonFactice(t *testing.T) {
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, "claude-launcher"), 0o755)
	settings := filepath.Join(d, "settings.json")
	envf := filepath.Join(d, "claude-launcher", "settings-env.json")
	_ = os.WriteFile(settings, []byte(`{"env":{"GH_TOKEN":"ghp_vraiement_valide"}}`), 0o600)
	before := markerFiles([]string{d})
	if len(before) != 0 {
		t.Fatalf("marqueur vu dans un fichier sain : %v", before)
	}
	if l := newLeaks([]string{d}, before); len(l) != 0 {
		t.Errorf("fuite signalée sans marqueur : %v", l)
	}
	_ = os.WriteFile(settings, []byte(`{"env":{"GH_TOKEN":"ghp_`+FakeTokenMarker+`_215"}}`), 0o600)
	if l := newLeaks([]string{d}, before); len(l) != 1 || l[0] != settings {
		t.Errorf("fuite non détectée dans settings.json : %v", l)
	}
	_ = os.WriteFile(envf, []byte(`["GH_TOKEN"] `+FakeTokenMarker), 0o600)
	if l := newLeaks([]string{d}, before); len(l) != 2 {
		t.Errorf("fuite non détectée dans settings-env.json : %v", l)
	}
	// déjà présent avant la suite : pas imputable aux tests
	if l := newLeaks([]string{d}, markerFiles([]string{d})); len(l) != 0 {
		t.Errorf("fichier déjà marqué avant la suite signalé : %v", l)
	}
	// le jeton factice de la suite n'est jamais un jeton GitHub valide (ghp_ + 36 alphanumériques)
	fake := "ghp_" + FakeTokenMarker + "_215"
	valid := true
	if !strings.HasPrefix(fake, "ghp_") || len(fake)-4 != 36 {
		valid = false
	}
	for _, r := range fake[4:] {
		if r == '_' {
			valid = false
		}
	}
	if valid {
		t.Errorf("%q a le format d'un vrai jeton GitHub", fake)
	}
}
