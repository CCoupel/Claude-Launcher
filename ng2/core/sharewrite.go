package core

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Écriture bornée de config.conf sur un partage réseau (#220) : un appel système bloqué (partage injoignable) ne
// s'interrompt pas ; l'appelant, lui, rend la main au bout du délai, sans tenir de verrou.

// Bornes et défaut de SHARE_WRITE_TIMEOUT (secondes).
const (
	ShareWriteTimeoutDefault = 10
	ShareWriteTimeoutMin     = 2
	ShareWriteTimeoutMax     = 120
)

// ClampShareWriteTimeout : valeur de SHARE_WRITE_TIMEOUT ramenée dans 2–120 (0 ou absente : défaut).
func ClampShareWriteTimeout(n int) int {
	switch {
	case n <= 0:
		return ShareWriteTimeoutDefault
	case n < ShareWriteTimeoutMin:
		return ShareWriteTimeoutMin
	case n > ShareWriteTimeoutMax:
		return ShareWriteTimeoutMax
	}
	return n
}

// ShareTimeout : l'écriture n'a pas abouti dans le délai (ou une écriture abandonnée est encore bloquée).
type ShareTimeout struct {
	After time.Duration
	Busy  bool // vrai : refus immédiat, une écriture abandonnée précédente est encore en cours
}

func (e *ShareTimeout) Error() string {
	return fmt.Sprintf("partage injoignable : config.conf non écrit après %d s", int(e.After.Round(time.Second)/time.Second))
}

// IsShareTimeout : l'erreur est un dépassement de délai d'écriture sur le partage.
func IsShareTimeout(err error) bool {
	var t *ShareTimeout
	return errors.As(err, &t)
}

// shareTimeoutSec : SHARE_WRITE_TIMEOUT de la dernière configuration lue (LoadConfig) ; défaut tant qu'aucune ne l'a été.
var shareTimeoutSec atomic.Int64

// ShareWriteTimeoutNow : délai d'écriture courant (dernière configuration lue).
func ShareWriteTimeoutNow() time.Duration {
	return time.Duration(ClampShareWriteTimeout(int(shareTimeoutSec.Load()))) * time.Second
}

var (
	// ShareWriteDelay : point d'injection des tests (nanosecondes) — délai posé dans la goroutine d'écriture, avant fn
	// (partage lent ou bloquant) ; nul en production.
	ShareWriteDelay atomic.Int64
	shareMu         sync.Mutex
	shareBusy       bool // une écriture de config.conf (rendue ou abandonnée) est en cours : au plus une goroutine
	// ShareWriteHook : point d'injection des tests (partage bloquant) — appelé dans la goroutine d'écriture, avant fn,
	// avec le chemin de config.conf ; nul en production. À poser avant l'appel et à ne remettre qu'écriture terminée.
	ShareWriteHook func(path string)
)

// WithinShare : exécute fn (écriture de config.conf) en au plus d. Au-delà : rend *ShareTimeout tout de suite ; fn
// continue en tâche de fond (un appel système bloqué ne s'interrompt pas) et son résultat est perdu — le fichier,
// s'il est écrit plus tard, fait foi. Une seule écriture à la fois : tant que l'écriture abandonnée est bloquée,
// les suivantes échouent tout de suite (même erreur), sans nouvelle goroutine. L'appelant ne tient aucun verrou
// au-delà du délai.
func WithinShare(d time.Duration, fn func() error) error {
	shareMu.Lock()
	if shareBusy {
		shareMu.Unlock()
		return &ShareTimeout{After: d, Busy: true}
	}
	shareBusy = true
	hook := ShareWriteHook
	shareMu.Unlock()
	res := make(chan error, 1)
	go func() {
		if hook != nil {
			hook(ConfigPath())
		}
		if dl := time.Duration(ShareWriteDelay.Load()); dl > 0 {
			time.Sleep(dl)
		}
		err := fn()
		shareMu.Lock()
		shareBusy = false
		shareMu.Unlock()
		res <- err
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case err := <-res:
		return err
	case <-t.C:
		return &ShareTimeout{After: d}
	}
}

// ShareWriteTimeout : délai d'écriture de la configuration cfg.
func (c Config) ShareWriteTimeout() time.Duration {
	return time.Duration(ClampShareWriteTimeout(c.ShareWriteTimeoutSec)) * time.Second
}
