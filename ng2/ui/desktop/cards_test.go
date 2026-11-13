package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #130 : au clic, les cartes partent du cache en moins de 300 ms, même si le
// reste (état jamais calculé, team) est lent ; il est complété ensuite.
func TestCardsFromCacheFirst(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta")
	writeStateForTest(t, core.ComputeState(ps[0])) // état préparé (gestionnaire, hub)
	var r recorder
	a := newApp(t, r.emit)
	old := cardsSlow
	cardsSlow = 2 * time.Second // partie lente simulée
	t.Cleanup(func() { cardsSlow = old })

	t0 := time.Now()
	res, err := a.Dispatch("select", json.RawMessage(`{"path":`+strconv.Quote(ps[0])+`}`))
	took := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	c := res.(Cards)
	if took > 300*time.Millisecond || c.Path != ps[0] || !c.Loaded || len(c.Legend) == 0 {
		t.Fatalf("cartes depuis le cache en %s : %+v", took, c)
	}
	// la team, elle, arrive ensuite (« pending »), étiquetée par son projet
	if len(c.Pending) == 0 || c.Pending[0] != "team" {
		t.Errorf("à compléter : %v", c.Pending)
	}
	ev := r.wait(t, 5*time.Second, "cartes complètes", func(e map[string]any) bool { return e["_name"] == "cards" })
	if ev["path"] != ps[0] || ev["pending"] != nil {
		t.Errorf("complétées : %v", ev)
	}
}

// Durées (t.Log) des cartes au clic : avant (#120 : tout calculé avant
// l'envoi) et après (cache d'abord). CL_PERF_ROOT : projets sur un disque
// réseau (ex. /share/MFS/ROOT/CLAUDE_PROJETS) pour des mesures représentatives.
func TestCardsTiming(t *testing.T) {
	root := ""
	if r := os.Getenv("CL_PERF_ROOT"); r != "" {
		root = filepath.Join(r, ".cl-e2e-cards-"+strconv.Itoa(os.Getpid()))
		t.Cleanup(func() { os.RemoveAll(root) })
	}
	ps := sandbox(t, root, "alpha", "beta")
	for i := 0; i < 300; i++ { // un projet un peu fourni
		_ = os.WriteFile(filepath.Join(ps[0], "f"+strconv.Itoa(i)+".txt"), []byte("x"), 0o644)
	}
	var r recorder
	a := newApp(t, r.emit)
	t0 := time.Now()
	_ = a.cardsFull(ps[0]) // avant : tout calculé avant l'envoi (état, team)
	before := time.Since(t0)
	writeStateForTest(t, core.ComputeState(ps[1])) // état préparé en tâche de fond
	t0 = time.Now()
	c := a.selectCards(ps[1])
	after := time.Since(t0)
	t.Logf("cartes au clic : avant %s (sans cache, tout calculé) ; après %s (depuis le cache, %v à compléter)",
		before.Round(time.Millisecond), after.Round(time.Millisecond), c.Pending)
	if after > 300*time.Millisecond {
		t.Errorf("cartes depuis le cache en %s (objectif 300 ms)", after)
	}
}
