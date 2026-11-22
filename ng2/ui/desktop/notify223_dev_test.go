package desktop

import (
	"encoding/json"
	"os"
	"sync"
	"testing"

	"claude-launcher-ng2/core"
)

// #223 : une notification par id, aucune avec ATTENTION_NOTIFY=0, retirée à la fin, clic → évènement attentionFocus.
func TestNotifyAttention223(t *testing.T) {
	var mu sync.Mutex
	var sent, removed []string
	defer notifySendHook.Set(notifySendHook.Set(func(id, title, body string) error {
		mu.Lock()
		sent = append(sent, id)
		mu.Unlock()
		return nil
	}))
	defer notifyRemoveHook.Set(notifyRemoveHook.Set(func(id string) {
		mu.Lock()
		removed = append(removed, id)
		mu.Unlock()
	}))
	_ = sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	ev := map[string]any{"path": "/p/alpha", "name": "alpha", "kind": "question", "text": "Valider ?", "id": "4-1"}
	if !a.conf().AttentionNotify {
		t.Fatal("ATTENTION_NOTIFY doit valoir 1 par défaut")
	}
	a.notifyAttention(ev)
	a.notifyAttention(ev) // même id : pas de doublon
	if len(sent) != 1 {
		t.Fatalf("notifications = %v, attendu 1", sent)
	}
	a.notificationClicked("4-1")
	found := false
	for _, e := range r.all() {
		if e["_name"] == "attentionFocus" && e["path"] == "/p/alpha" {
			found = true
		}
	}
	if !found {
		t.Error("clic : évènement attentionFocus attendu")
	}
	a.notifyEnd(map[string]any{"path": "/p/alpha", "id": "4-1"})
	a.notifyEnd(map[string]any{"path": "/p/alpha", "id": "4-1"})
	if len(removed) != 1 {
		t.Errorf("retraits = %v, attendu 1", removed)
	}
	// option à 0 : aucune notification
	if err := os.WriteFile(core.ConfigPath(), []byte("ATTENTION_NOTIFY=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.configReload(core.ConfConf)
	ev["id"] = "4-2"
	a.notifyAttention(ev)
	if len(sent) != 1 {
		t.Errorf("option à 0 : %v notifié(s)", sent)
	}
}

// #223 : snapshot.attentionNotify toujours présent, suit config.conf à chaud.
func TestSnapshotAttentionNotify223(t *testing.T) {
	_ = sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	get := func() (bool, bool) {
		b, _ := json.Marshal(a.Snapshot())
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		v, ok := m["attentionNotify"].(bool)
		return v, ok
	}
	if v, ok := get(); !ok || !v {
		t.Fatalf("défaut : %v présent=%v, attendu true", v, ok)
	}
	_ = os.WriteFile(core.ConfigPath(), []byte("ATTENTION_NOTIFY=0\n"), 0o644)
	a.configReload(core.ConfConf)
	if v, ok := get(); !ok || v {
		t.Errorf("option à 0 : %v présent=%v, attendu false présent", v, ok)
	}
}

// attentionFocus : émetteur natif SEUL (pas le miroir navigateur) ; clic après la fin de l'attente : onglet sélectionné quand même.
func TestAttentionFocusNativeOnly223(t *testing.T) {
	defer notifySendHook.Set(notifySendHook.Set(func(string, string, string) error { return nil }))
	_ = sandbox(t, "", "alpha")
	var native, mirror recorder
	a := newApp(t, native.emit)
	me := Emitter(mirror.emit)
	a.mirror.Store(&me)
	a.notifyAttention(map[string]any{"path": "/p/alpha", "name": "alpha", "kind": "question", "text": "?", "id": "7-1"})
	a.notifyEnd(map[string]any{"path": "/p/alpha", "id": "7-1"}) // attente finie
	a.notificationClicked("7-1")
	n := 0
	for _, e := range native.all() {
		if e["_name"] == "attentionFocus" && e["path"] == "/p/alpha" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("attentionFocus natif = %d, attendu 1 (attente finie : onglet sélectionné quand même)", n)
	}
	for _, e := range mirror.all() {
		if e["_name"] == "attentionFocus" {
			t.Errorf("attentionFocus envoyé au navigateur miroir : %v", e)
		}
	}
}
