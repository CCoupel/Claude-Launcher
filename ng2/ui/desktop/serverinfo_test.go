package desktop

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

// `init` et l'instantané portent server{version, boot} ; boot est stable pendant la vie du processus.
func TestInitPorteLeServeur(t *testing.T) {
	var r recorder
	a := newApp(t, r.emit)
	old := core.NgVersion
	core.NgVersion = "v6.4.0"
	defer func() { core.NgVersion = old }()
	res, err := a.Dispatch("init", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var m struct {
		Server ServerInfo `json:"server"`
		State  struct {
			Server ServerInfo `json:"server"`
		} `json:"state"`
	}
	_ = json.Unmarshal(b, &m)
	if m.Server.Version != "v6.4.0" || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(m.Server.Boot) {
		t.Errorf("init.server = %+v", m.Server)
	}
	if m.State.Server != m.Server {
		t.Errorf("l'instantané doit porter le même serveur : %+v / %+v", m.State.Server, m.Server)
	}
	res2, _ := a.Dispatch("init", nil)
	b2, _ := json.Marshal(res2)
	if !strings.Contains(string(b2), m.Server.Boot) {
		t.Error("boot instable entre deux init du même processus")
	}
}

// En cluster, l'annonce d'une release n'est pas cliquable et renvoie à helm upgrade.
func TestAnnonceNotifyEnCluster(t *testing.T) {
	t.Setenv("CL_UPDATE_MODE", "notify")
	t.Setenv("CL_IN_CLUSTER", "1")
	var r recorder
	a := newApp(t, r.emit)
	a.mu.Lock()
	a.upd.newLauncher = "v9.9.9"
	a.mu.Unlock()
	var got *Alert
	for _, al := range a.Snapshot().Alerts {
		if al.Kind == "launcher" {
			al := al
			got = &al
		}
	}
	if got == nil || got.Click || !got.Notify || !strings.Contains(got.Text, "mise à jour par helm upgrade") {
		t.Errorf("annonce : %+v", got)
	}
}
