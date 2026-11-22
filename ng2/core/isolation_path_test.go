package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathUnder(t *testing.T) {
	for _, c := range []struct {
		tmp, d string
		want   bool
	}{
		{"/t", "/t", true}, {"/t", "/t/a/b", true}, {"/t", "/tt/a", false}, {"/t", "/", false}, {"/t/a", "/t", false},
	} {
		if got := pathUnder(filepath.FromSlash(c.tmp), filepath.FromSlash(c.d)); got != c.want {
			t.Errorf("pathUnder(%q, %q) = %v", c.tmp, c.d, got)
		}
	}
}

// canonPath : un dossier du dossier temporaire est reconnu quelle que soit la forme du chemin (relatif/lien/non nettoyé).
func TestUnderTempFormesDuChemin(t *testing.T) {
	TestIsolation = true
	defer func() { TestIsolation = false }()
	d := t.TempDir()
	for _, p := range []string{filepath.Join(d, "x", "settings.json"), filepath.Join(d, "a", "..", "settings.json")} {
		if !underTemp(p) {
			t.Errorf("%s refusé", p)
		}
	}
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	if underTemp(filepath.Join(root, "hors-temp-inexistant", "settings.json")) {
		t.Error("chemin hors dossier temporaire accepté")
	}
}
