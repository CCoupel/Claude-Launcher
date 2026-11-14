package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #136 — contracts/desktop-dispatch.md, sans tmux : forme JSON d'une ligne de
// la carte Team, teamToggle sur un projet fermé ou sans argument, carte d'un
// projet fermé (teammates du registre non démarrés, rien de caché).

func TestTeamRowJSONContract(t *testing.T) {
	b, _ := json.Marshal(TeamRow{Name: "dev-backend", Tag: "v5.9.2", Model: "sonnet", Effort: "medium",
		Claude: "2.1.87", Pane: "%7", Started: true, Hidden: true, IdleLeft: 12})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"name", "tag", "model", "effort", "started", "active", "hidden", "pinned", "idleLeft", "claude", "pane"} {
		if _, ok := m[k]; !ok {
			t.Errorf("champ %q absent du JSON d'une ligne : %s", k, b)
		}
	}
	if _, ok := m["count"]; ok {
		t.Error("`count` doit être retiré (plus de regroupement par type)")
	}
	if m["hidden"] != true || m["pinned"] != false || m["idleLeft"] != 12.0 {
		t.Errorf("valeurs : %s", b)
	}
	// non démarré : pas de pane ni de version de claude
	b, _ = json.Marshal(TeamRow{Name: "qa"})
	m = nil
	_ = json.Unmarshal(b, &m)
	if _, ok := m["pane"]; ok {
		t.Errorf("pane présent pour un teammate non démarré : %s", b)
	}
	if m["started"] != false {
		t.Errorf("started : %s", b)
	}
}

func TestTeamToggleClosedProject(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	for _, arg := range []string{
		`{"path":` + strconvQ(ps[0]) + `,"pane":"%7"}`, // projet non ouvert
		`{"path":"/nulle/part","pane":"%7"}`,
		`{}`,
	} {
		res, err := a.Dispatch("teamToggle", json.RawMessage(arg))
		if err != nil {
			t.Fatalf("%s : %v", arg, err)
		}
		m, _ := res.(map[string]any)
		if m["ok"] != false || strings.TrimSpace(asString(m["msg"])) == "" {
			t.Errorf("%s → %v : refus avec message attendu", arg, res)
		}
	}
}

func TestReadTeamClosedProject(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	cfg := filepath.Join(ps[0], ".claude")
	_ = os.MkdirAll(cfg, 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "project-config.json"),
		[]byte(`{"agents":{"registry":[{"name":"dev-a","tag":"v1"},{"name":"qa","tag":"v1"}]}}`), 0o644)
	var r recorder
	a := newApp(t, r.emit)
	rows := a.readTeam(ps[0])
	if len(rows) != 2 || rows[0].Name != "dev-a" || rows[1].Name != "qa" {
		t.Fatalf("lignes : %+v", rows)
	}
	for _, row := range rows {
		if row.Started || row.Hidden || row.Pinned || row.Active || row.Pane != "" || row.IdleLeft != 0 {
			t.Errorf("projet fermé : %+v", row)
		}
	}
}

func strconvQ(s string) string { b, _ := json.Marshal(s); return string(b) }

func asString(v any) string { s, _ := v.(string); return s }
