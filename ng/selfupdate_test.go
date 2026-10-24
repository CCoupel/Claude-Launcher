package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeRelease sert /v9.9.9/<nom> ; sums remplace SHA256SUMS si non vide.
func fakeRelease(t *testing.T, files map[string]string, sums string) *httptest.Server {
	t.Helper()
	if sums == "" {
		for name, body := range files {
			h := sha256.Sum256([]byte(body))
			sums += fmt.Sprintf("%s  %s\n", hex.EncodeToString(h[:]), name)
		}
	}
	files["SHA256SUMS"] = sums
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyUpdate(t *testing.T) {
	dir := t.TempDir()
	bin, script := filepath.Join(dir, "claude-launcher-ng"), filepath.Join(dir, "claude-launcher.sh")
	writeFile(t, bin, "ancien binaire", 0o755)
	writeFile(t, script, "ancien script", 0o700)
	srv := fakeRelease(t, map[string]string{ngAsset(): "nouveau binaire", "claude-launcher.sh": "nouveau script"}, "")

	binDone, scriptDone, err := applyUpdate(srv.URL+"/v9.9.9", bin, script)
	if err != nil || !binDone || !scriptDone {
		t.Fatalf("applyUpdate = %v %v %v", binDone, scriptDone, err)
	}
	if got := readFile(t, bin); got != "nouveau binaire" {
		t.Errorf("binaire = %q", got)
	}
	if got := readFile(t, script); got != "nouveau script" {
		t.Errorf("script = %q", got)
	}
	if st, _ := os.Stat(script); st.Mode().Perm() != 0o700 {
		t.Errorf("droits du script perdus : %v", st.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) != 0 {
		t.Errorf("fichiers temporaires restés : %v", left)
	}
}

func TestApplyUpdateRefusesBadSum(t *testing.T) {
	dir := t.TempDir()
	bin, script := filepath.Join(dir, "claude-launcher-ng"), filepath.Join(dir, "claude-launcher.sh")
	writeFile(t, bin, "ancien binaire", 0o755)
	writeFile(t, script, "ancien script", 0o755)
	// somme fausse pour le binaire, script absent de SHA256SUMS
	sums := fmt.Sprintf("%064d  %s\n", 0, ngAsset())
	srv := fakeRelease(t, map[string]string{ngAsset(): "binaire altéré", "claude-launcher.sh": "nouveau script"}, sums)

	binDone, scriptDone, err := applyUpdate(srv.URL+"/v9.9.9", bin, script)
	if err == nil || binDone || scriptDone {
		t.Fatalf("applyUpdate = %v %v %v, attendu un refus", binDone, scriptDone, err)
	}
	if readFile(t, bin) != "ancien binaire" || readFile(t, script) != "ancien script" {
		t.Error("fichiers modifiés malgré le refus")
	}
}

func TestApplyUpdateOffline(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude-launcher-ng")
	writeFile(t, bin, "ancien binaire", 0o755)
	srv := fakeRelease(t, map[string]string{}, "")
	srv.Close() // serveur injoignable

	if binDone, _, err := applyUpdate(srv.URL+"/v9.9.9", bin, ""); err == nil || binDone {
		t.Fatalf("applyUpdate hors ligne = %v %v", binDone, err)
	}
	if readFile(t, bin) != "ancien binaire" {
		t.Error("binaire modifié hors ligne")
	}
}

func TestReleaseVersionOnly(t *testing.T) {
	for v, want := range map[string]bool{"v3.0.2": true, "dev": false, "v3.0.1+quit": false, "": false, "v3.0": false} {
		if got := releaseVerRe.MatchString(v); got != want {
			t.Errorf("%q : %v, attendu %v", v, got, want)
		}
	}
}
