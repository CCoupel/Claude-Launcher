package tun

import (
	"sync"
	"testing"
	"time"
)

func TestGetSet(t *testing.T) {
	v := Of(5 * time.Second)
	if v.Get() != 5*time.Second {
		t.Fatalf("Get = %v", v.Get())
	}
	if old := v.Set(time.Second); old != 5*time.Second || v.Get() != time.Second {
		t.Errorf("Set : ancienne %v, nouvelle %v", old, v.Get())
	}
	// restauration en une ligne
	defer v.Set(v.Set(time.Minute))
	if v.Get() != time.Minute {
		t.Errorf("valeur posée = %v", v.Get())
	}
}

func TestFonctionEtNil(t *testing.T) {
	h := Of[func() int](nil)
	if f := h.Get(); f != nil {
		t.Fatal("crochet nil attendu")
	}
	h.Set(func() int { return 7 })
	if f := h.Get(); f == nil || f() != 7 {
		t.Error("crochet posé non rendu")
	}
}

// Lectures et remplacements concurrents : aucune course sous -race, toujours une valeur entière (jamais déchirée).
func TestAccesConcurrent(t *testing.T) {
	v := Of([2]int{1, 1})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				v.Set([2]int{i, i})
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if a := v.Get(); a[0] != a[1] {
					t.Errorf("valeur déchirée : %v", a)
					return
				}
			}
		}()
	}
	wg.Wait()
}
