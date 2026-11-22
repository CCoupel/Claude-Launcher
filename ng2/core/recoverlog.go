package core

import (
	"log"
	"runtime/debug"
	"strings"
	"time"
)

// RecoverLog : à appeler par `defer core.RecoverLog("…")` dans une goroutine de surveillance — un panic y est consigné
// (lieu, message, début de la pile) au lieu de faire tomber tout le processus (hub, exe, pod).
func RecoverLog(where string) {
	if r := recover(); r != nil {
		log.Printf("PANIC récupéré (%s) : %v\n%s", where, r, shortStack())
	}
}

// RunProtected : exécute fn ; un panic est consigné et fn est relancée (au plus maxRestarts fois, 2 s entre deux), puis
// l'on rend la main. Rend vrai si fn s'est terminée normalement. Pour un surveillant de longue durée (layout-watch).
func RunProtected(where string, maxRestarts int, fn func()) bool {
	for try := 0; ; try++ {
		panicked := func() (p bool) {
			defer func() {
				if r := recover(); r != nil {
					p = true
					log.Printf("PANIC récupéré (%s, essai %d) : %v\n%s", where, try+1, r, shortStack())
				}
			}()
			fn()
			return false
		}()
		if !panicked {
			return true
		}
		if try >= maxRestarts {
			return false
		}
		time.Sleep(restartPause)
	}
}

var restartPause = 2 * time.Second

// shortStack : début de la pile du panic en cours.
func shortStack() string {
	st := strings.Split(string(debug.Stack()), "\n")
	if len(st) > 14 {
		st = st[:14]
	}
	return strings.Join(st, "\n")
}
