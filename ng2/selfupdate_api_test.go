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
	"time"
)

// Lien direct en 504 constant (vu en v5.3.0) : la mise à jour passe par l'API.
func TestReleaseGetAPIFallback(t *testing.T) {
	bin := []byte("nouveau binaire")
	h := sha256.Sum256(bin)
	sums := hex.EncodeToString(h[:]) + "  " + ngAsset() + "\n"
	direct := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rel/v9.9.9/SHA256SUMS", "/rel/v9.9.9/" + ngAsset():
			direct++
			w.WriteHeader(504)
		case "/repos/" + launcherRepo + "/releases/tags/v9.9.9":
			fmt.Fprintf(w, `{"assets":[{"id":1,"name":"SHA256SUMS"},{"id":2,"name":%q}]}`, ngAsset())
		case "/repos/" + launcherRepo + "/releases/assets/1":
			fmt.Fprint(w, sums)
		case "/repos/" + launcherRepo + "/releases/assets/2":
			if r.Header.Get("Accept") != "application/octet-stream" {
				w.WriteHeader(415)
				return
			}
			w.Write(bin)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	oldAPI, oldRel, oldWait := apiBase, releaseBase, releaseRetryWait
	apiBase, releaseBase, releaseRetryWait = srv.URL, srv.URL+"/rel", time.Millisecond
	defer func() { apiBase, releaseBase, releaseRetryWait = oldAPI, oldRel, oldWait }()

	if !releaseReady(releaseBase + "/v9.9.9") {
		t.Fatal("release non vue prête malgré l'API")
	}
	dst := filepath.Join(t.TempDir(), "ng2")
	_ = os.WriteFile(dst, []byte("ancien"), 0o755)
	done, _, err := applyUpdate(releaseBase+"/v9.9.9", dst, "")
	got, _ := os.ReadFile(dst)
	if err != nil || !done || string(got) != string(bin) {
		t.Fatalf("mise à jour par l'API : %v %v %q", err, done, got)
	}
	if direct < 4 {
		t.Errorf("lien direct essayé %d fois, attendu 2 par fichier", direct)
	}
	// 404 : pas de repli (fichier absent, pas une panne)
	if _, err := releaseGet(releaseBase+"/v9.9.9/absent", time.Second); err == nil || err.Error() != "HTTP 404" {
		t.Errorf("404 : %v", err)
	}
}
