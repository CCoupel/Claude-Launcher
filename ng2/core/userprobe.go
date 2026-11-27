package core

import (
	"sync"
	"time"
)

// Sondage des backends d'UTILISATEURS (LOT-3.2 E3, contrat §3 « Limites », §10, S31) : UN appel en vol par backend, attente croissante de 10 s à
// 5 min quand il est injoignable, au plus 16 sondages simultanés, aucune goroutine qui fuit.

const (
	probeMin       = 10 * time.Second
	probeMax       = 5 * time.Minute
	probeMaxFlight = 16
)

// NextProbeDelay : attente avant le prochain sondage après `failures` échecs consécutifs — 10 s, puis ×2 jusqu'à 5 min.
func NextProbeDelay(failures int) time.Duration {
	if failures <= 0 {
		return probeMin
	}
	d := probeMin
	for i := 0; i < failures && d < probeMax; i++ {
		d *= 2
	}
	if d > probeMax {
		d = probeMax
	}
	return d
}

type probeState struct {
	inflight bool
	failures int
	next     time.Time
}

// Prober : état des sondages (une entrée par backend). L'horloge est injectée.
type Prober struct {
	now func() time.Time
	mu  sync.Mutex
	st  map[string]*probeState
	wg  sync.WaitGroup
	sem chan struct{}
}

// NewProber : sondeur à horloge injectée.
func NewProber(now func() time.Time) *Prober {
	return &Prober{now: now, st: map[string]*probeState{}, sem: make(chan struct{}, probeMaxFlight)}
}

// Due : l'attente du backend est écoulée (jamais sondé : dû).
func (p *Prober) Due(backend string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.st[backend]
	return s == nil || !p.now().Before(s.next)
}

// Run : lance probe pour le backend si aucun appel n'est en vol et si l'attente est écoulée ; faux sinon. Un échec allonge l'attente
// (10 s, 20 s, … 5 min), un succès la remet à 10 s. Au plus 16 sondages simultanés (les autres attendent leur tour sans bloquer l'appelant).
func (p *Prober) Run(backend string, probe func() error) bool {
	p.mu.Lock()
	s := p.st[backend]
	if s == nil {
		s = &probeState{}
		p.st[backend] = s
	}
	if s.inflight || p.now().Before(s.next) {
		p.mu.Unlock()
		return false
	}
	s.inflight = true
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.sem <- struct{}{}
		err := probe()
		<-p.sem
		p.mu.Lock()
		if err != nil {
			s.failures++
		} else {
			s.failures = 0
		}
		s.next = p.now().Add(NextProbeDelay(max(s.failures-1, 0))) // 1er échec : 10 s, 2e : 20 s…
		s.inflight = false
		p.mu.Unlock()
	}()
	return true
}

// Wait : attend la fin des sondages lancés (tests, arrêt).
func (p *Prober) Wait() { p.wg.Wait() }
