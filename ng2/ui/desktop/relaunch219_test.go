package desktop

// #219 : Dispatch « launcherInfo » et « relaunch » (fenêtre native et --web).
// Réutilise le crochet existant restartApp (jamais de vraie relance dans un test).

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func hookRestart219(t *testing.T) *int32 {
	var n int32
	old := restartApp.Get()
	restartApp.Set(func() { atomic.AddInt32(&n, 1) })
	t.Cleanup(func() { restartApp.Set(old) })
	return &n
}

func TestLauncherInfoForme219(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "launcherInfo", map[string]any{})
	if _, ok := v["running"].(string); !ok {
		t.Errorf("running absent ou non texte : %v", v)
	}
	if _, ok := v["installed"].(string); !ok {
		t.Errorf("installed absent ou non texte : %v", v)
	}
	if _, ok := v["binChanged"].(bool); !ok {
		t.Errorf("binChanged absent ou non booléen : %v", v)
	}
	if m, _ := v["mode"].(string); m != "desktop" && m != "web" {
		t.Errorf("mode = %v, attendu desktop ou web hors coque et pod", v["mode"])
	}
	for _, k := range []string{"stale", "backup"} { // propres à la coque / sans retour arrière dans l'interface (contrat ed8b845)
		if _, has := v[k]; has {
			t.Errorf("launcherInfo ne doit pas porter %q : %v", k, v)
		}
	}
}

func TestRelaunchRefuseSansConfirm219(t *testing.T) {
	sandbox(t, "", "alpha")
	n := hookRestart219(t)
	var r recorder
	a := newApp(t, r.emit)
	for _, args := range []map[string]any{{}, {"confirm": false}} {
		v := dispatchJSON(t, a, "relaunch", args)
		if v["ok"] != false || v["msg"] == nil || v["msg"] == "" {
			t.Errorf("relaunch %v = %v : refus avec message attendu", args, v)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(n) != 0 {
		t.Error("relance déclenchée sans confirmation")
	}
}

func TestRelaunchAvecConfirmRelanceUneFois219(t *testing.T) {
	sandbox(t, "", "alpha")
	n := hookRestart219(t)
	var r recorder
	a := newApp(t, r.emit)
	v := dispatchJSON(t, a, "relaunch", map[string]any{"confirm": true})
	if v["ok"] != true {
		t.Fatalf("relaunch confirmé = %v", v)
	}
	for i := 0; i < 50 && atomic.LoadInt32(n) == 0; i++ {
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(n); got != 1 {
		t.Errorf("restartApp appelé %d fois, attendu 1", got)
	}
}

func TestRelaunchWebSansConfirmEtSansSessionRefuse219(t *testing.T) {
	sandbox(t, "", "alpha")
	n := hookRestart219(t)
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok219", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	// sans cookie : refusé comme tout appel
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", `{"method":"relaunch","args":{"confirm":true}}`, "application/json", ""); resp.StatusCode == 200 {
		t.Error("relaunch accepté sans cookie")
	}
	// avec cookie mais sans confirm : refus applicatif
	resp, body := doWith(t, srv.URL, "POST", "/api/call", "", "tok219", `{"method":"relaunch","args":{}}`, "application/json", "")
	if resp.StatusCode != 200 || !contains219(body, `"ok":false`) {
		t.Errorf("relaunch sans confirm : %d %s", resp.StatusCode, body)
	}
	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(n) != 0 {
		t.Error("relance déclenchée sans confirmation (web)")
	}
}

func contains219(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
