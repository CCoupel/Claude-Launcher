//go:build !windows

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #130 : rien de ce qui peut passer par 9P ne bloque l'affichage. Le cache du
// hub Linux est rendu « bloquant » (tube nommé : sa lecture ne revient jamais) ;
// le démarrage, l'arbre, l'init de l'interface et un clic doivent quand même
// répondre tout de suite (le cache du hub n'est lu qu'en tâche de fond).
func TestNothingSlowBeforeDisplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tubes nommés : Linux / macOS")
	}
	ps := sandbox(t, "", "alpha", "beta")
	hub := t.TempDir()
	_ = os.MkdirAll(filepath.Join(hub, "state"), 0o755)
	for _, p := range ps { // chaque état du hub : un tube qui bloque à la lecture
		h := filepath.Base(core.StatePath(core.RemotePath(p)))
		if err := syscall.Mkfifo(filepath.Join(hub, "state", h), 0o644); err != nil {
			t.Skip("mkfifo : ", err)
		}
	}
	_ = os.WriteFile(core.BackendsPath(), []byte("hubs:\n  "+hostForTest()+":\n    hub_cache: "+strconv.Quote(hub)+"\n"), 0o644)

	done := make(chan time.Duration, 1)
	// App créée dans la goroutine (qui peut rester bloquée) : arrêtée en fin
	// de test si elle existe (#141)
	apps := make(chan *App, 1)
	t.Cleanup(func() {
		select {
		case a := <-apps:
			stopBg(a)
		default:
		}
	})
	go func() {
		t0 := time.Now()
		var r recorder
		a := NewApp(r.emit)
		apps <- a
		if a.hubCache != hub {
			t.Errorf("hub_cache : %q", a.hubCache)
		}
		_, _ = a.Dispatch("init", nil)
		_, _ = a.Dispatch("select", json.RawMessage(`{"path":`+strconv.Quote(ps[1])+`}`))
		done <- time.Since(t0)
	}()
	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("démarrage + init + clic : %s", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bloqué sur une lecture du cache du hub avant l'affichage")
	}
	// les tubes sont débloqués pour que le nettoyage n'attende rien
	for _, p := range ps {
		f, err := os.OpenFile(filepath.Join(hub, "state", filepath.Base(core.StatePath(core.RemotePath(p)))), os.O_RDWR, 0)
		if err == nil {
			f.Close()
		}
	}
}
