package core

import (
	"claude-launcher-ng2/internal/tun"
	"errors"
	"log"
	"time"

	"github.com/fsnotify/fsnotify"
)

// fsCloseLimit : au plus combien de temps un arrêt attend la fermeture d'un surveillant fsnotify (le launcher a 20 s pour
// ses ouvertures dans un délai de grâce de 30 s : un Close qui ne rend jamais la main ne doit pas bloquer l'arrêt).
// Remplacé dans les tests.
var fsCloseLimit = 5 * time.Second

// closeFsWatcher : ferme w EN VIDANT ses canaux, au plus fsCloseLimit. Sous Windows, Close attend la goroutine de lecture
// du surveillant, qui reste bloquée sur l'envoi d'un évènement quand plus personne ne lit Events : interblocage à
// l'arrêt (CI Windows : Stop figé 9 minutes). Tant que Close n'a pas rendu la main, évènements et erreurs sont lus et
// jetés. Rend faux (et journalise) si Close n'a pas fini dans le délai : la goroutine de fermeture est alors abandonnée.
func closeFsWatcher(w *fsnotify.Watcher, what string) bool {
	if w == nil {
		return true
	}
	return drainClose(w.Close, w.Events, w.Errors, fsCloseLimit, what)
}

// drainClose : closeFn dans une goroutine, events et errs vidés jusqu'à son retour, au plus limit.
func drainClose(closeFn func() error, events <-chan fsnotify.Event, errs <-chan error, limit time.Duration, what string) bool {
	done := make(chan struct{})
	go func() {
		_ = closeFn()
		close(done)
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		select {
		case <-events:
		case <-errs:
		case <-done:
			return true
		case <-timer.C:
			log.Printf("surveillant de fichiers (%s) : fermeture sans réponse après %s, abandonnée (l'arrêt continue)", what, limit)
			return false
		}
	}
}

// fsCallGrace : délai laissé à un appel au surveillant (Add, Remove) déjà en cours quand l'arrêt est demandé.
var fsCallGrace = tun.Of(500 * time.Millisecond)

// fsCall : f (w.Add, w.Remove) dans une goroutine, abandonnable à l'arrêt (quit fermé). Sous Windows, fsnotify répond à Add
// et Remove par la goroutine de lecture : si Close passe avant qu'elle ne traite la demande, l'appel attend une réponse qui
// ne vient JAMAIS (Add / Close en course, vu en CI : adder figé 9 minutes sur backend_windows.go « <-in.reply », Stop derrière
// wg.Wait). Après quit, l'appel a fsCallGrace pour finir, puis il est abandonné (sa goroutine reste, sans conséquence).
func fsCall(f func() error, quit <-chan struct{}) error {
	res := make(chan error, 1)
	go func() { res <- f() }()
	select {
	case err := <-res:
		return err
	case <-quit:
	}
	select {
	case err := <-res:
		return err
	case <-time.After(fsCallGrace.Get()):
		return errFsAbandoned
	}
}

var errFsAbandoned = errors.New("appel au surveillant abandonné à l'arrêt")
