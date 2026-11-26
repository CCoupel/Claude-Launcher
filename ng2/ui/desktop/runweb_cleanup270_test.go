package desktop

import (
	"claude-launcher-ng2/core"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// #270 — RunWeb crée sa propre App et ne la rend pas : sans elle, le test ne pouvait pas attendre la fin réelle de l'arrêt (« détachement
// des terminaux toujours en cours à l'échéance ») ni des tâches de fond, qui écrivaient encore dans xdg_cache_home pendant l'effacement
// du dossier temporaire (Windows : « The directory is not empty »). Le crochet runWebApp donne l'App au test.
type runWebCap struct {
	mu   sync.Mutex
	apps []*App
}

// captureRunWebApp : à appeler AVANT de lancer RunWeb (après sandbox). Fin de test : chaque App est arrêtée (idempotent) et ses tâches
// de fond attendues (15 s) ; enregistré après le nettoyage du bac à sable, il s'exécute avant lui.
func captureRunWebApp(t *testing.T) *runWebCap {
	t.Helper()
	c := &runWebCap{}
	old := runWebApp.Set(func(a *App) {
		c.mu.Lock()
		c.apps = append(c.apps, a)
		c.mu.Unlock()
	})
	t.Cleanup(func() {
		c.finish(t)
		runWebApp.Set(old)
	})
	return c
}

// finish : Stop puis attente des tâches de fond de toutes les App créées par RunWeb ; idempotent.
func (c *runWebCap) finish(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	apps := append([]*App(nil), c.apps...)
	c.mu.Unlock()
	for _, a := range apps {
		a.Stop()
		if !a.waitBackground(15 * time.Second) {
			t.Errorf("tâches de fond de l'App de RunWeb encore actives 15 s après Stop : un écrivain tardif ferait échouer l'effacement du TempDir")
		}
	}
}

// quietTree270 : aucun fichier créé ni modifié sous dirs pendant d (un écrivain tardif y serait vu). Rend la description des écarts.
func quietTree270(d time.Duration, dirs ...string) []string {
	snap := func() map[string]string {
		m := map[string]string{}
		for _, dir := range dirs {
			_ = filepath.Walk(dir, func(p string, st os.FileInfo, err error) error {
				if st != nil {
					m[p] = fmt.Sprint(st.Size(), st.ModTime().UnixNano(), st.IsDir())
				}
				return nil
			})
		}
		return m
	}
	before := snap()
	time.Sleep(d)
	after := snap()
	var diff []string
	for p, v := range after {
		if before[p] != v {
			diff = append(diff, p)
		}
	}
	return diff
}

// assertCaptured270 : le crochet a bien reçu l'App de RunWeb (sinon le test n'attend rien) et, une fois Stop et les tâches de fond
// attendues, plus aucun écrivain ne touche aux dossiers de cache et de config claude du bac à sable.
func (c *runWebCap) assertSettled270(t *testing.T) {
	t.Helper()
	c.finish(t)
	c.mu.Lock()
	n := len(c.apps)
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d App reçue(s) par le crochet runWebApp, attendu 1 : le test n'attend rien", n)
	}
	// l'écrivain identifié de l'App de RunWeb : la goroutine du serveur de fichiers (browser-server.status, réécrit toutes les 10 s dans le
	// CacheDir COURANT tant qu'elle vivait) ; le reste du cache peut recevoir des écritures d'autres tests, hors de cette vérification
	if late := quietTree270(700*time.Millisecond, filepath.Join(core.CacheDir(), "browser-server.status")); len(late) > 0 {
		t.Errorf("écrivain tardif après l'arrêt de RunWeb : %v (l'effacement du TempDir échouerait sous Windows)", late)
	}
}
