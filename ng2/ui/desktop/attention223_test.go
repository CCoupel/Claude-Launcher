package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #223 (LOT-3a v6.7.0) — le teamleader en attente est signalé sur l'interface native en moins de 5 s, retiré dès la
// réponse, sans doublon ; seul le teamleader compte. Contrat : contracts/attention.md §§ 4-6. Un faux claude joue les hooks :
// les VRAIES commandes posées par core.SyncAttentionHook dans le settings.json isolé du test sont exécutées avec la charge
// utile et l'environnement que claude leur donnerait (CL_ATTENTION_DIR du projet, TMUX_PANE du pane). Projet factice,
// tmux isolé (openFakeProject), jamais de vrai projet ni de settings.json réel.

const (
	attSignalWithin = 5 * time.Second // contrat § 5
	attPayloadQ     = `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Valider le plan ?","header":"Plan","options":[{"label":"Oui"}]}]}}`
	attPayloadPerm  = `{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude a besoin de votre permission pour utiliser Bash"}`
)

type attFixture struct {
	f       fakeProj
	state   string // <projet>/.claude-launcher/attention
	leader  string // %N
	mate    string // %M (teammate)
	cmdWait map[string]string
}

func attentionProject(t *testing.T) *attFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hooks sh et tmux : Linux seulement")
	}
	f, _ := fakeTeamProject(t)
	ids := strings.Fields(f.tmux("list-panes", "-t", "="+hostSession(f.path)+":", "-F", "#{pane_id}"))
	if len(ids) < 2 {
		t.Fatalf("panes = %v", ids)
	}
	// commandes réelles des hooks (settings.json isolé du test)
	dir := core.ClaudeDir()
	if err := core.SyncAttentionHook(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	cmds := map[string]string{}
	for ev, groups := range doc.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, "# cl-attention") {
					cmds[ev] = h.Command
				}
			}
		}
	}
	for _, ev := range []string{"PreToolUse", "Notification", "PostToolUse"} {
		if cmds[ev] == "" {
			t.Fatalf("hook %s du launcher absent de settings.json", ev)
		}
	}
	return &attFixture{f: f, state: filepath.Join(f.path, ".claude-launcher", "attention"), leader: ids[0], mate: ids[1], cmdWait: cmds}
}

// hook : joue le hook de l'évènement comme claude, dans le pane donné.
func (x *attFixture) hook(t *testing.T, event, payload, pane string) {
	t.Helper()
	c := exec.Command("sh", "-c", x.cmdWait[event])
	c.Env = []string{"PATH=" + os.Getenv("PATH"), "CL_ATTENTION_DIR=" + x.state, "TMUX_PANE=" + pane}
	c.Stdin = strings.NewReader(payload)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("hook %s : %v %s", event, err, out)
	}
}

// view : lignes et onglets de l'instantané, en JSON (champs « attention » du contrat § 6).
func (x *attFixture) view(t *testing.T) (row, tab map[string]any) {
	t.Helper()
	b, _ := json.Marshal(x.f.a.Snapshot())
	var s struct {
		Rows []map[string]any `json:"rows"`
		Tabs []map[string]any `json:"tabs"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.Rows {
		if r["path"] == x.f.path {
			row = r
		}
	}
	for _, tb := range s.Tabs {
		if tb["path"] == x.f.path {
			tab = tb
		}
	}
	return
}

func attOf(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	a, _ := m["attention"].(map[string]any)
	return a
}

func (x *attFixture) events(name string) (out []map[string]any) {
	for _, e := range x.f.r.all() {
		if e["_name"] == name && e["path"] == x.f.path {
			out = append(out, e)
		}
	}
	return
}

func (x *attFixture) waitAttention(t *testing.T, what string, present bool) {
	t.Helper()
	waitUntil(t, what, attSignalWithin, func() bool {
		row, tab := x.view(t)
		return (attOf(row) != nil) == present && (attOf(tab) != nil) == present
	})
}

// Question du teamleader : ligne de l'arbre ET onglet signalés en ≤ 5 s, évènement `attention` unique, puis retrait dès la
// réponse (PostToolUse) avec `attentionEnd` ; aucune notification en double ni après la fin (critical).
func TestAttentionQuestionThenAnswered223(t *testing.T) {
	x := attentionProject(t)
	t0 := time.Now()
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	x.waitAttention(t, "attente signalée (arbre et onglet)", true)
	if d := time.Since(t0); d > attSignalWithin {
		t.Errorf("signalée après %v, attendu ≤ %v", d, attSignalWithin)
	}
	row, tab := x.view(t)
	for what, a := range map[string]map[string]any{"ligne": attOf(row), "onglet": attOf(tab)} {
		if a["kind"] != "question" || a["text"] != "Valider le plan ?" || a["id"] == nil || a["id"] == "" || a["since"] == nil {
			t.Errorf("%s : attention = %v, attendu {kind:question, text, since, id}", what, a)
		}
	}
	id := attOf(row)["id"]
	ev := x.f.r.wait(t, attSignalWithin, "évènement attention", func(e map[string]any) bool { return e["_name"] == "attention" && e["path"] == x.f.path })
	if ev["kind"] != "question" || ev["text"] != "Valider le plan ?" || ev["id"] != id || ev["name"] == nil {
		t.Errorf("évènement = %v, attendu {path, name, kind, text, id} cohérent avec l'instantané", ev)
	}
	// plusieurs tours de la boucle de 2 s : pas de doublon pour le même id
	time.Sleep(4500 * time.Millisecond)
	n := 0
	for _, e := range x.events("attention") {
		if e["id"] == id {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d évènements attention pour l'id %v, attendu 1", n, id)
	}
	// réponse de l'utilisateur : l'outil se termine (PostToolUse) → badge retiré
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	x.waitAttention(t, "attente retirée après la réponse", false)
	end := x.f.r.wait(t, attSignalWithin, "attentionEnd", func(e map[string]any) bool { return e["_name"] == "attentionEnd" && e["path"] == x.f.path })
	if end["id"] != id {
		t.Errorf("attentionEnd id = %v, attendu %v", end["id"], id)
	}
	time.Sleep(2500 * time.Millisecond)
	if len(x.events("attentionEnd")) != 1 {
		t.Errorf("%d attentionEnd, attendu 1", len(x.events("attentionEnd")))
	}
	for _, e := range x.events("attention") {
		if e["id"] != id {
			t.Errorf("nouvelle attente après la réponse : %v", e)
		}
	}
}

// Demande de permission (Notification) : kind permission avec le message.
func TestAttentionPermission223(t *testing.T) {
	x := attentionProject(t)
	x.hook(t, "Notification", attPayloadPerm, x.leader)
	x.waitAttention(t, "permission signalée", true)
	row, _ := x.view(t)
	a := attOf(row)
	if a["kind"] != "permission" || !strings.Contains(a["text"].(string), "permission pour utiliser Bash") {
		t.Errorf("attention = %v", a)
	}
	x.hook(t, "UserPromptSubmit", `{"hook_event_name":"UserPromptSubmit"}`, x.leader) // fin d'attente par une autre voie
	x.waitAttention(t, "retirée à la réponse", false)
}

// Seul le teamleader est signalé : l'état écrit par un teammate est ignoré.
func TestAttentionTeammateIgnored223(t *testing.T) {
	x := attentionProject(t)
	x.hook(t, "PreToolUse", attPayloadQ, x.mate)
	time.Sleep(4500 * time.Millisecond) // plus de deux tours de la boucle
	row, tab := x.view(t)
	if attOf(row) != nil || attOf(tab) != nil {
		t.Errorf("attente d'un teammate signalée : ligne %v, onglet %v", attOf(row), attOf(tab))
	}
	if n := len(x.events("attention")); n != 0 {
		t.Errorf("%d évènement(s) attention pour un teammate", n)
	}
}

// État de plus de 24 h : ignoré et supprimé ; l'attente d'un projet qui n'est pas ouvert n'est pas lue.
func TestAttentionStaleAndNotOpen223(t *testing.T) {
	x := attentionProject(t)
	_ = os.MkdirAll(x.state, 0o755)
	stale := filepath.Join(x.state, strings.TrimPrefix(x.leader, "%")+".json")
	_ = os.WriteFile(stale, []byte(attPayloadQ), 0o644)
	old := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(stale, old, old)
	// la boucle de 2 s supprime le fichier : attente sur la condition, pas sur une pause fixe (stabilité sous charge)
	waitUntil(t, "état périmé supprimé", 12*time.Second, func() bool { _, err := os.Stat(stale); return err != nil })
	if row, _ := x.view(t); attOf(row) != nil {
		t.Errorf("état de 25 h signalé : %v", attOf(row))
	}
}

func TestAttentionClosedProjectNotRead223(t *testing.T) {
	ps := sandbox(t, "", "alpha", "beta")
	var r recorder
	a := newApp(t, r.emit)
	d := filepath.Join(ps[1], ".claude-launcher", "attention")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "5.json"), []byte(attPayloadQ), 0o644)
	time.Sleep(4500 * time.Millisecond)
	b, _ := json.Marshal(a.Snapshot())
	if strings.Contains(string(b), `"attention"`) {
		t.Errorf("projet fermé et sans session signalé en attente : %s", b)
	}
	for _, e := range r.all() {
		if e["_name"] == "attention" {
			t.Errorf("évènement attention pour un projet fermé : %v", e)
		}
	}
}

// Sans CL_ATTENTION_DIR (session hors launcher) le hook ne fait rien : aucun signal.
func TestAttentionNoSignalWithoutDir223(t *testing.T) {
	x := attentionProject(t)
	c := exec.Command("sh", "-c", x.cmdWait["PreToolUse"])
	c.Env = []string{"PATH=" + os.Getenv("PATH"), "TMUX_PANE=" + x.leader}
	c.Stdin = strings.NewReader(attPayloadQ)
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4500 * time.Millisecond)
	if row, _ := x.view(t); attOf(row) != nil {
		t.Errorf("signal sans CL_ATTENTION_DIR : %v", attOf(row))
	}
}
