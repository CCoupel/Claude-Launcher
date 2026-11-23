package desktop

// Test de spécification #238 (v6.8.1) — une nouvelle version apparaît sur la page web ouverte sans F5 : la boucle de
// vérification (updateLoop, même fonction que la coque) pousse l'annonce par l'évènement SSE `state` (champ alerts),
// sans appel `refresh`. Faux serveur GitHub (core.ApiBase) : aucun appel réel ; poste factice (sandbox + CL_SCRIPT).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

func TestNouvelleVersionPousseeSansF5238(t *testing.T) {
	root := t.TempDir()
	sandbox(t, root, "alpha")
	t.Setenv("CL_SCRIPT", filepath.Join(root, "claude-launcher.sh"))
	t.Setenv("CL_CACHE_DIR", filepath.Join(root, "ngcache"))
	for _, k := range []string{"CL_FAKE_LATEST_TPL", "CL_FAKE_LATEST_LAUNCHER", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("GH_TOKEN", "tok") // cycle avec jeton
	oldV := core.SetNgVersion("v6.8.0")
	defer core.SetNgVersion(oldV)
	defer core.SetInitSynced(core.SetInitSynced(true)) // pas de synchronisation d'init-project.md (réseau)
	// template déjà adopté : aucun téléchargement ni adoption
	for p, v := range map[string]string{core.TplVersionPath(): "v6.8.0", core.TplCommitPath(): "tplsha"} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var hits atomic.Int32
	var tag atomic.Value // dernier tag du launcher publié sur le faux GitHub
	tag.Store("v6.8.0")
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.Contains(r.URL.Path, "/releases/"):
			http.NotFound(w, r) // release pas encore prête : annonce « en préparation »
		case strings.Contains(r.URL.Path, "Claude-Launcher"):
			fmt.Fprintf(w, `[{"name":%q,"commit":{"sha":"lsha"}}]`, tag.Load().(string))
		default:
			fmt.Fprint(w, `[{"name":"v6.8.0","commit":{"sha":"tplsha"}}]`)
		}
	}))
	defer gh.Close()
	defer core.ApiBase.Set(core.ApiBase.Set(gh.URL))

	// page web : flux SSE ouvert AVANT l'apparition de la version
	w := &webBridge{token: "tokU238tokU238tok", subs: map[chan []byte]bool{}, strictHost: true}
	a := newApp(t, w.emit)
	w.app = a
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	c := scBootstrap(t, srv.URL, "tokU238tokU238tok")
	resp := openSSE(t, srv.URL, c)
	defer resp.Body.Close()

	tag.Store("v6.9.0") // nouvelle version publiée avant la première vérification
	go a.emitter()
	a.bg(a.updateLoop)

	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			l := sc.Text()
			if !strings.HasPrefix(l, "data: ") {
				continue
			}
			var ev struct {
				Name string `json:"name"`
				Data struct {
					Alerts []Alert `json:"alerts"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(l[6:]), &ev) != nil || ev.Name != "state" {
				continue
			}
			for _, al := range ev.Data.Alerts {
				if al.Kind == "launcher" && strings.Contains(al.Text, "v6.9.0") {
					found <- al.Text
					return
				}
			}
		}
	}()
	select {
	case txt := <-found:
		if !strings.Contains(txt, "NG v6.9.0") {
			t.Errorf("annonce = %q", txt)
		}
	case <-time.After(30 * time.Second): // 1re vérification 6 s après le démarrage
		t.Fatalf("aucun évènement state avec l'annonce v6.9.0 en 30 s (appels GitHub : %d) : la page ne la verrait qu'avec F5", hits.Load())
	}
	if hits.Load() == 0 {
		t.Error("le faux serveur GitHub n'a pas été interrogé")
	}
}
