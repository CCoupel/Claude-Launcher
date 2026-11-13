package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// artefact de release par système : Linux/macOS inchangés, .exe sous Windows (#112)
func TestNgAssetFor(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "claude-launcher-ng2-linux-amd64",
		{"linux", "arm64"}:   "claude-launcher-ng2-linux-arm64",
		{"darwin", "arm64"}:  "claude-launcher-ng2-darwin-arm64",
		{"windows", "amd64"}: "claude-launcher-ng2.exe",
		{"windows", "arm64"}: "claude-launcher-ng2-windows-arm64.exe",
	}
	for k, want := range cases {
		if got := ngAssetFor(k[0], k[1]); got != want {
			t.Errorf("ngAssetFor(%s, %s) = %q, attendu %q", k[0], k[1], got, want)
		}
	}
	if NgAsset() != ngAssetFor(runtime.GOOS, runtime.GOARCH) {
		t.Error("NgAsset : système courant")
	}
}

// InstallBinary : le binaire est remplacé (Windows : l'ancien est mis de côté en .old)
func TestInstallBinary(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "claude-launcher-ng2")
	_ = os.WriteFile(dst, []byte("ancien"), 0o755)
	if err := InstallBinary(dst, []byte("nouveau")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "nouveau" {
		t.Errorf("contenu : %q", b)
	}
	_, err := os.Stat(dst + ".old")
	if runtime.GOOS == "windows" && err != nil {
		t.Error("Windows : .old attendu")
	}
	if runtime.GOOS != "windows" && err == nil {
		t.Error("hors Windows : pas de .old")
	}
}

// serveur web : l'URL porte le chemin vu des sessions (Linux), le fichier est
// lu à son chemin local (correspondance de Windows)
func TestFileServerMapsURLToLocal(t *testing.T) {
	local := t.TempDir()
	_ = os.MkdirAll(filepath.Join(local, "ROOT", "p"), 0o755)
	_ = os.WriteFile(filepath.Join(local, "ROOT", "p", "a.md"), []byte("# bonjour"), 0o644)
	_ = os.WriteFile(filepath.Join(t.TempDir(), "secret"), []byte("x"), 0o644)
	m, _ := NewPathMapper([]PathMap{{Local: local, Remote: "/share/MFS"}})
	s := fileServer{hosts: map[string]bool{"localhost": true},
		roots: func() []string { return []string{"/share/MFS/ROOT"} },
		local: func(p string) string { l, _ := m.ToLocal(p); return l }}
	get := func(u string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, "http://localhost"+u, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if c, b := get("/share/MFS/ROOT/p/a.md"); c != 200 || !strings.Contains(b, "bonjour") {
		t.Errorf("fichier mappé : %d %q", c, b)
	}
	if c, _ := get("/share/MFS/ROOT/../secret"); c != 404 {
		t.Errorf("hors racine : %d", c)
	}
	if runtime.GOOS != "windows" { // (un chemin C:\… n'est pas un chemin d'URL)
		if c, _ := get(local + "/ROOT/p/a.md"); c != 404 {
			t.Errorf("chemin local dans l'URL : %d", c)
		}
	}
	if !underLocal(`\\wsl.localhost\Ubuntu\share\MFS\ROOT\p\a.md`, `\\WSL.localhost\Ubuntu\share\MFS\ROOT`) ||
		underLocal(`\\wsl.localhost\Ubuntu\share\MFS\ROOTX\a`, `\\wsl.localhost\Ubuntu\share\MFS\ROOT`) {
		t.Error("underLocal : forme Windows")
	}
}
