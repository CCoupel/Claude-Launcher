package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Deux rafraîchissements concurrents (F5 via Dispatch("refresh"), tour périodique) lisent et écrivent
// InitSynced et le fichier init-project.md : sans verrou, course de données (go test -race) et
// fichier temporaire partagé écrasé. Reproduit par plusieurs ensureInitSynced / adoptTemplate en parallèle.
func TestInitSyncConcurrent(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	t.Setenv("CL_SCRIPT", filepath.Join(dir, "claude-launcher.sh"))
	if err := os.WriteFile(TplCommitPath(), []byte("abc123"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("init-project contenu"))
	}))
	defer srv.Close()
	old := SetRawBase(srv.URL)
	oldSynced := InitSynced
	InitSynced = false
	defer func() { SetRawBase(old); InitSynced = oldSynced }()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); ensureInitSynced() }()
		go func() { defer wg.Done(); adoptTemplate("v9.9.9", "abc123") }()
	}
	wg.Wait()
	if b, _ := os.ReadFile(InitProjectPath()); string(b) != "init-project contenu" {
		t.Errorf("init-project.md = %q", b)
	}
	if !initSyncedGet() {
		t.Error("InitSynced non posé après une synchronisation réussie")
	}
}
