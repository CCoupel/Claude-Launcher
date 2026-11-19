package desktop

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// #131 : deux clics rapides, réponse lente : les cartes complètes du projet
// quitté ne sont jamais envoyées ; seules celles du projet choisi arrivent.
func TestCardsOfPreviousProjectDropped(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta")
	var r recorder
	a := newApp(t, r.emit)
	old := cardsSlow.Get()
	cardsSlow.Set(500 * time.Millisecond)
	t.Cleanup(func() { cardsSlow.Set(old) })
	for i := 0; i < 3; i++ { // clics alternés rapides
		for _, p := range ps {
			if _, err := a.Dispatch("select", json.RawMessage(`{"path":`+strconv.Quote(p)+`}`)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	r.wait(t, 5*time.Second, "cartes du dernier projet", func(e map[string]any) bool {
		return e["_name"] == "cards" && e["path"] == ps[1]
	})
	cardsBg.Wait()
	for _, e := range r.all() {
		if e["_name"] == "cards" && e["path"] != ps[1] {
			t.Errorf("cartes d'un projet qui n'est plus sélectionné : %v", e["path"])
		}
	}
}
