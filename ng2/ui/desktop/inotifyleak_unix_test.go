//go:build linux

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inotifyFDs : nombre de descripteurs inotify ouverts par ce processus de test.
func inotifyFDs() int {
	ents, _ := os.ReadDir("/proc/self/fd")
	n := 0
	for _, e := range ents {
		if l, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name())); err == nil && strings.Contains(l, "inotify") {
			n++
		}
	}
	return n
}

// Une App démarrée puis arrêtée ne laisse aucune veille inotify : le quota de l'utilisateur (max_user_instances,
// 128 ou 256) est partagé par tous les processus ; une campagne de tests (-count=10) en détenait 239 et faisait
// échouer d'autres tests « too many open files ». Rouge tant que App.Stop n'arrête pas la veille de l'arborescence
// (core.WatchTree n'a pas d'arrêt : sa goroutine et son watcher fsnotify vivent jusqu'à la fin du processus).
func TestAppStopReleasesInotifyWatchers(t *testing.T) {
	sandbox(t, "", "alpha")
	cycle := func() {
		var r recorder
		a := newApp(t, r.emit)
		a.Start()
		time.Sleep(150 * time.Millisecond) // la veille est posée par une goroutine de Start
		a.Stop()
	}
	cycle() // amorçage éventuel de ressources partagées
	time.Sleep(300 * time.Millisecond)
	before := inotifyFDs()
	const cycles = 6
	for i := 0; i < cycles; i++ {
		cycle()
	}
	time.Sleep(500 * time.Millisecond) // arrêt asynchrone des veilles
	if grown := inotifyFDs() - before; grown > 1 {
		t.Errorf("%d instance(s) inotify de plus après %d démarrages/arrêts d'App (avant : %d) : App.Stop ne ferme pas la veille de l'arborescence (core.WatchTree)", grown, cycles, before)
	}
}
