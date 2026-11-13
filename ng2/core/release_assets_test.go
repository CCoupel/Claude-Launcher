package core

import (
	"os"
	"strings"
	"testing"
)

// La mise à jour automatique cherche dans la release le fichier de son
// système (ngAssetFor) : release.yml doit publier exactement ces noms, et
// les inclure dans SHA256SUMS (#112).
func TestReleaseAssetsMatchUpdater(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Skip("release.yml introuvable : ", err)
	}
	y := string(b)
	exe := ngAssetFor("windows", "amd64")
	for _, want := range []string{
		"-o ../dist/" + exe + " .",               // build Windows
		"sha256sum claude-launcher-ng2-* " + exe, // dans SHA256SUMS
		"dist/" + exe,                            // publié
		"-tags desktop,production",               // interface native Wails
		"-H windowsgui",
	} {
		if !strings.Contains(y, want) {
			t.Errorf("release.yml ne contient pas %q", want)
		}
	}
	// Linux et macOS : noms inchangés (claude-launcher-ng2-<os>-<arch>)
	if got := ngAssetFor("linux", "amd64"); !strings.Contains(y, "-o ../dist/claude-launcher-ng2-$os-$arch") || got != "claude-launcher-ng2-linux-amd64" {
		t.Errorf("artefact Linux : %s", got)
	}
}
