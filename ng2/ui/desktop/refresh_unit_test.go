package desktop

import (
	"testing"
	"time"
)

// #158 : rafraîchissement d'un projet — évènement final toujours émis avec les
// six étapes dans l'ordre, un seul rafraîchissement par projet, fermeture forcée
// sans objet pour un projet qui ne tourne pas.
func TestRefreshProjectEmitsFinalEvent(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)

	if res, err := a.Dispatch("refreshProject", []byte(`{"path":"/hors/arbre"}`)); err == nil {
		t.Errorf("projet inconnu accepté : %v", res)
	}
	res, err := a.Dispatch("refreshProject", []byte(`{"path":"`+ps[0]+`"}`))
	if err != nil || res.(map[string]any)["ok"] != true {
		t.Fatalf("refreshProject : %v %v", res, err)
	}
	ev := r.wait(t, 30*time.Second, "évènement refresh final", func(m map[string]any) bool {
		return m["_name"] == "refresh" && m["done"] == true
	})
	steps, _ := ev["steps"].([]any)
	want := []string{"ctr", "session", "git", "github", "tpl", "usage"}
	if len(steps) != len(want) {
		t.Fatalf("étapes : %v", steps)
	}
	for i, s := range steps {
		m := s.(map[string]any)
		if m["id"] != want[i] || (m["state"] != "ok" && m["state"] != "err") {
			t.Errorf("étape %d : %v", i, m)
		}
	}
	if _, busy := a.refreshing.Load(ps[0]); busy {
		time.Sleep(200 * time.Millisecond)
	}
}

func TestRefreshProjectOnlyOnceAtATime(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	a.refreshing.Store(ps[0], true) // un rafraîchissement est en cours
	res, err := a.Dispatch("refreshProject", []byte(`{"path":"`+ps[0]+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(map[string]any); m["ok"] != false || m["msg"] == "" {
		t.Errorf("second rafraîchissement : %v", m)
	}
	if !a.Snapshot().rowOf(ps[0]).Refreshing {
		t.Error("ligne sans refreshing pendant l'opération")
	}
}

func TestForceCloseNothingToForce(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	res, err := a.Dispatch("forceClose", []byte(`{"path":"`+ps[0]+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(map[string]any); m["msg"] == "" || m["question"] != nil {
		t.Errorf("projet fermé : %v", m)
	}
	res, _ = a.Dispatch("forceCloseConfirm", []byte(`{"path":"`+ps[0]+`"}`)) // sans question en attente
	if m := res.(map[string]any); m["ok"] != false {
		t.Errorf("confirmation sans objet : %v", m)
	}
}

func (s Snapshot) rowOf(path string) Row {
	for _, r := range s.Rows {
		if r.Path == path {
			return r
		}
	}
	return Row{}
}
