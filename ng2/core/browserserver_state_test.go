//go:build !windows

package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Dossiers d'état du launcher (cache, coffre, configuration, configuration de claude) : jamais servis ni listés, même
// quand ils sont sous une racine servie (en cluster, CL_CACHE_DIR est sous la racine partagée), liens compris.
func TestEtatDuLauncherJamaisServi(t *testing.T) {
	root := t.TempDir()
	t.Setenv(CacheDirVar, filepath.Join(root, "cache", "cluster"))
	t.Setenv(SecretsDirVar, filepath.Join(root, "coffre"))
	t.Setenv("CL_CONFIG_DIR", filepath.Join(root, "reglages"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claudecfg"))
	for rel, c := range map[string]string{
		"cache/cluster/update-check.json":     "{}",
		"cache/cluster/browser-token":         "jeton",
		"cache/cluster/secrets-journal.jsonl": "journal",
		"cache/cluster/projects.json":         "[]",
		"coffre/srv/id":                       "cle",
		"reglages/config.conf":                `GITHUB_TOKEN="ghp_x"`,
		"claudecfg/credentials.json":          "id",
		"PROJETS/p/ok.html":                   "ok",
		"PROJETS/p/_work/mockup/m.html":       "maquette",
	} {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// un lien d'apparence anodine vers le cache
	if err := os.Symlink(filepath.Join(root, "cache", "cluster"), filepath.Join(root, "PROJETS", "p", "lien-cache")); err != nil {
		t.Skip("liens symboliques indisponibles : ", err)
	}
	srv := httptest.NewServer(fileServer{roots: func() []string { return []string{root} }})
	defer srv.Close()
	for _, rel := range []string{
		"cache/cluster/update-check.json", "cache/cluster/browser-token", "cache/cluster/secrets-journal.jsonl", "cache/cluster/projects.json",
		"coffre/srv/id", "reglages/config.conf", "claudecfg/credentials.json", "PROJETS/p/lien-cache/browser-token", "PROJETS/p/lien-cache/update-check.json",
	} {
		if code, body := fetch(t, srv, filepath.Join(root, rel)); code != http.StatusNotFound {
			t.Errorf("%s servi : %d %.30q (404 attendu)", rel, code, body)
		}
	}
	for rel, want := range map[string]string{"PROJETS/p/ok.html": "ok", "PROJETS/p/_work/mockup/m.html": "maquette"} {
		if code, body := fetch(t, srv, filepath.Join(root, rel)); code != 200 || !strings.Contains(body, want) {
			t.Errorf("%s : %d %.30q (servi attendu)", rel, code, body)
		}
	}
	// listes : les dossiers d'état n'apparaissent pas à la racine ; le lien vers le cache est masqué dans le projet
	_, top := fetch(t, srv, root+"/")
	// (« cache » lui-même, simple parent du dossier d'état, peut être listé : son contenu l'est-il ?)
	if _, b := fetch(t, srv, root+"/cache/"); strings.Contains(b, "cluster") {
		t.Errorf("dossier d'état listé dans son parent : %.200q", b)
	}
	for _, hidden := range []string{"coffre", "reglages", "claudecfg"} {
		if strings.Contains(top, ">"+hidden+"/<") {
			t.Errorf("%s listé à la racine", hidden)
		}
	}
	if !strings.Contains(top, "PROJETS") {
		t.Errorf("liste de la racine : %.200q", top)
	}
	if _, b := fetch(t, srv, filepath.Join(root, "PROJETS", "p")+"/"); strings.Contains(b, "lien-cache") || !strings.Contains(b, "ok.html") {
		t.Errorf("liste du projet : %.200q", b)
	}
}
