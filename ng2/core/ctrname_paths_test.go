package core

import "testing"

// Le nom d'un projet se calcule sur les deux derniers éléments du chemin, quel que soit le séparateur : un projet Windows
// sans correspondance de chemins (C:\Users\…\TEST\alpha) ne doit pas donner le chemin entier tronqué, identique pour
// tous les projets (CI Windows).
func TestCtrNameCheminWindows(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\runneradmin\AppData\Local\Temp\TestX\001\PROJETS\TEST\alpha`: "test-alpha",
		`C:\Users\runneradmin\AppData\Local\Temp\TestX\001\PROJETS\TEST\beta`:  "test-beta",
		`D:\GITHUB\QUALIF\factice-1`:                                           "qualif-factice-1",
		`\\nas\MFS\ROOT\PROJETS\QUALIF\factice-1`:                              "qualif-factice-1",
		"/share/MFS/ROOT/PROJETS/QUALIF/factice-1":                             "qualif-factice-1",
	} {
		if got := CtrName(in); got != want {
			t.Errorf("CtrName(%q) = %q, attendu %q", in, got, want)
		}
	}
}
