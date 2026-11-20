package desktop

// #210 (LOT-5) : le rafraîchissement des cartes n'est jamais perdu pendant une relecture lente
// (.claude/workflow/plan-v6.5.0-lot5.md, « Tests Requis »). Point d'injection : cardsRead (app.go), lu par la goroutine de
// relecture à la place de a.cardsFull. L'emitter n'est pas démarré par newApp : la fixture le démarre. Attentes de conditions,
// jamais de durée fixe pour un résultat positif ; seule l'ABSENCE d'une relecture de trop laisse un court temps de stabilisation.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fix210 struct {
	a        *App
	gate     chan struct{} // la 1re relecture bloque tant qu'il n'est pas fermé
	reads    atomic.Int32  // relectures démarrées
	active   atomic.Int32
	maxAct   atomic.Int32
	etat     atomic.Value // état « sur disque » lu par la relecture
	mu       sync.Mutex
	emitted  []Cards
	states   atomic.Int32
	emitDone chan struct{} // fermé quand emitter() a rendu la main
}

func newFix210(t *testing.T) *fix210 {
	t.Helper()
	f := &fix210{gate: make(chan struct{}), emitDone: make(chan struct{})}
	f.etat.Store("A")
	a := newApp(t, func(string, any) {})
	f.a = a
	a.emit = func(name string, data any) {
		switch name {
		case "cards":
			f.mu.Lock()
			f.emitted = append(f.emitted, data.(Cards))
			f.mu.Unlock()
		case "state":
			f.states.Add(1)
		}
	}
	a.mu.Lock()
	a.sel = "p1"
	a.mu.Unlock()
	old := cardsRead.Set(func(_ *App, path string) Cards {
		n := f.reads.Add(1)
		cur := f.active.Add(1)
		for m := f.maxAct.Load(); cur > m && !f.maxAct.CompareAndSwap(m, cur); m = f.maxAct.Load() {
		}
		if n == 1 {
			<-f.gate // relecture lente (projet par 9P)
		}
		defer f.active.Add(-1)
		return Cards{Path: path, Branch: f.etat.Load().(string)}
	})
	t.Cleanup(func() { cardsRead.Set(old) })
	go func() { a.emitter(); close(f.emitDone) }()
	t.Cleanup(func() { a.Stop(); f.release() })
	return f
}

func (f *fix210) release() {
	select {
	case <-f.gate:
	default:
		close(f.gate)
	}
}

func (f *fix210) cards() []Cards {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cards(nil), f.emitted...)
}

func wait210(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition non atteinte en 10 s : %s", what)
}

// premier signal : la relecture démarre et reste bloquée ; rend la main quand l'emitter a consommé le signal.
func (f *fix210) startSlow(t *testing.T) {
	t.Helper()
	f.a.cardsChanged()
	wait210(t, "1re relecture démarrée", func() bool { return f.reads.Load() == 1 })
}

// signals : n signaux pendant la relecture ; attend que l'emitter les ait tous consommés (canal vidé).
func (f *fix210) signals(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		f.a.cardsChanged()
		wait210(t, "signal consommé par l'emitter", func() bool { return len(f.a.cardDirty) == 0 })
	}
	// barrière : l'emitter est mono-goroutine ; un « state » émis prouve que l'itération du dernier signal est terminée
	// (consommé du canal ne veut pas dire pris en compte : requestCards peut ne pas avoir encore tourné)
	before := f.states.Load()
	f.a.changed()
	wait210(t, "signaux pris en compte (barrière state)", func() bool { return f.states.Load() > before })
}

// settle : plus aucune relecture en cours ; courte stabilisation pour constater l'ABSENCE d'une relecture de trop.
func (f *fix210) settle(t *testing.T, wantReads int32) {
	t.Helper()
	wait210(t, fmt.Sprintf("%d relectures terminées", wantReads), func() bool { return f.reads.Load() >= wantReads && f.active.Load() == 0 })
	time.Sleep(60 * time.Millisecond)
	if n := f.reads.Load(); n != wantReads {
		t.Errorf("relectures : %d, attendu exactement %d", n, wantReads)
	}
}

// Un puis cinq signaux pendant une relecture lente : exactement UNE relecture de plus (fusion), après la fin de la première,
// et la dernière carte émise reflète l'état modifié pendant la première.
func TestCartesSignalPendantRelectureLente210(t *testing.T) {
	for _, n := range []int{1, 5} {
		t.Run(fmt.Sprintf("%d_signaux", n), func(t *testing.T) {
			f := newFix210(t)
			f.startSlow(t)
			f.etat.Store("B") // l'état change pendant la relecture en cours
			f.signals(t, n)
			if f.reads.Load() != 1 {
				t.Fatalf("une 2e relecture a démarré avant la fin de la 1re : %d", f.reads.Load())
			}
			f.release()
			f.settle(t, 2)
			cs := f.cards()
			if len(cs) == 0 || cs[len(cs)-1].Branch != "B" {
				t.Errorf("la dernière carte émise doit refléter l'état postérieur au signal : %+v", cs)
			}
		})
	}
}

// jamais deux relectures simultanées (sous -race), même avec des rafales de signaux.
func TestCartesJamaisDeuxRelecturesSimultanees210(t *testing.T) {
	f := newFix210(t)
	f.startSlow(t)
	f.signals(t, 8)
	f.release()
	f.settle(t, 2)
	for i := 0; i < 20; i++ {
		f.a.cardsChanged()
	}
	wait210(t, "rafale traitée", func() bool { return len(f.a.cardDirty) == 0 && f.active.Load() == 0 })
	if m := f.maxAct.Load(); m != 1 {
		t.Errorf("relectures simultanées : max %d, attendu 1", m)
	}
}

// la sélection change pendant la relecture : la relecture rejouée porte sur la nouvelle sélection ; la carte de l'ancien
// projet n'est jamais émise.
func TestCartesSelectionChangeePendantRelecture210(t *testing.T) {
	f := newFix210(t)
	f.startSlow(t)
	f.a.mu.Lock()
	f.a.sel = "p2"
	f.a.mu.Unlock()
	f.signals(t, 1)
	f.release()
	f.settle(t, 2)
	cs := f.cards()
	for _, c := range cs {
		if c.Path == "p1" {
			t.Errorf("carte de l'ancien projet émise : %+v", c)
		}
	}
	if len(cs) == 0 || cs[len(cs)-1].Path != "p2" {
		t.Errorf("la relecture rejouée doit porter sur p2 : %+v", cs)
	}
}

// quit fermé pendant une relecture bloquée avec une relecture en attente : l'arrêt n'est pas bloqué, l'emitter sort,
// aucune relecture n'est lancée ensuite.
func TestCartesArretPendantRelecture210(t *testing.T) {
	f := newFix210(t)
	f.startSlow(t)
	f.signals(t, 1) // relecture en attente
	stopped := make(chan struct{})
	go func() { f.a.Stop(); close(stopped) }()
	select {
	case <-f.emitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("l'emitter ne sort pas alors que quit est fermé (relecture bloquée)")
	}
	f.release() // la relecture bloquée finit APRÈS l'arrêt
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop bloqué par la relecture")
	}
	wait210(t, "relecture terminée", func() bool { return f.active.Load() == 0 })
	time.Sleep(60 * time.Millisecond)
	if n := f.reads.Load(); n != 1 {
		t.Errorf("relecture lancée après quit : %d lectures, attendu 1", n)
	}
}

// #130 : un changed() pendant une relecture lente émet « state » sans attendre la fin de la relecture.
func TestCartesRelectureLenteNeRetardePasLArbre210(t *testing.T) {
	f := newFix210(t)
	f.startSlow(t)
	before := f.states.Load()
	f.a.changed()
	wait210(t, "state émis pendant la relecture bloquée", func() bool { return f.states.Load() > before })
	if f.active.Load() != 1 {
		t.Error("la relecture lente devait encore être en cours")
	}
}
