package desktop

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #223 (LOT-3b v6.7.0) — notifications de la fenêtre native. Contrat : contracts/attention.md § 7 : une notification par id,
// aucune avec ATTENTION_NOTIFY=0 (badge seul), retirée à la fin de l'attente, aucune après, défaut activé. Le système de
// notification (Wails) est remplacé par les points d'injection notifySendHook / notifyRemoveHook : jamais de vraie
// notification.

type notifRec struct {
	mu      sync.Mutex
	sent    []notifSent
	removed []string
}

type notifSent struct{ id, title, body string }

func (n *notifRec) sends() []notifSent {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notifSent(nil), n.sent...)
}

func (n *notifRec) removes() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.removed...)
}

func recordNotifications(t *testing.T) *notifRec {
	t.Helper()
	n := &notifRec{}
	oldS := notifySendHook.Set(func(id, title, body string) error {
		n.mu.Lock()
		n.sent = append(n.sent, notifSent{id, title, body})
		n.mu.Unlock()
		return nil
	})
	oldR := notifyRemoveHook.Set(func(id string) {
		n.mu.Lock()
		n.removed = append(n.removed, id)
		n.mu.Unlock()
	})
	t.Cleanup(func() { notifySendHook.Set(oldS); notifyRemoveHook.Set(oldR) })
	return n
}

// Défaut : option activée (décision GATE 2).
func TestAttentionNotifyDefaultOn223(t *testing.T) {
	sandbox(t, "", "alpha")
	if !core.LoadConfig().AttentionNotify {
		t.Error("ATTENTION_NOTIFY absent de config.conf : attendu activé (1) par défaut")
	}
}

// Une notification par attente (id) avec le projet et la question ; retirée à la réponse ; rien après.
func TestNotificationOncePerIDAndRemovedOnEnd223(t *testing.T) {
	n := recordNotifications(t)
	x := attentionProject(t)
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	waitUntil(t, "notification envoyée", attSignalWithin, func() bool { return len(n.sends()) >= 1 })
	time.Sleep(4500 * time.Millisecond) // plusieurs tours de la boucle : jamais de doublon
	s := n.sends()
	if len(s) != 1 {
		t.Fatalf("%d notifications, attendu 1 par id : %+v", len(s), s)
	}
	row, _ := x.view(t)
	if id := attOf(row)["id"]; s[0].id != id {
		t.Errorf("id de la notification = %q, attendu l'id de l'attente %v", s[0].id, id)
	}
	if !strings.Contains(s[0].title, "alpha") || !strings.Contains(s[0].body, "Valider le plan ?") {
		t.Errorf("notification = %+v, attendu le projet dans le titre et la question dans le corps", s[0])
	}
	// réponse : notification retirée, plus rien ensuite
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	waitUntil(t, "notification retirée", attSignalWithin, func() bool { return len(n.removes()) >= 1 })
	if r := n.removes(); len(r) != 1 || r[0] != s[0].id {
		t.Errorf("retraits = %v, attendu [%s]", r, s[0].id)
	}
	time.Sleep(2500 * time.Millisecond)
	if len(n.sends()) != 1 || len(n.removes()) != 1 {
		t.Errorf("notification après la fin : envois %d, retraits %d", len(n.sends()), len(n.removes()))
	}
}

// Une nouvelle attente = un nouvel id = une nouvelle notification (permission après la question).
func TestNotificationPerNewWait223(t *testing.T) {
	n := recordNotifications(t)
	x := attentionProject(t)
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	waitUntil(t, "première notification", attSignalWithin, func() bool { return len(n.sends()) == 1 })
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	waitUntil(t, "retrait", attSignalWithin, func() bool { return len(n.removes()) == 1 })
	time.Sleep(1100 * time.Millisecond) // id = pane + date en ms : une attente distincte
	x.hook(t, "Notification", attPayloadPerm, x.leader)
	waitUntil(t, "seconde notification", attSignalWithin, func() bool { return len(n.sends()) == 2 })
	s := n.sends()
	if s[0].id == s[1].id || !strings.Contains(s[1].body, "permission pour utiliser Bash") {
		t.Errorf("notifications = %+v", s)
	}
}

// ATTENTION_NOTIFY=0 : le badge reste affiché, aucune notification.
func TestNoNotificationWhenOptionOff223(t *testing.T) {
	n := recordNotifications(t)
	x := attentionProject(t)
	if err := os.WriteFile(core.ConfigPath(), []byte("ATTENTION_NOTIFY=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x.f.a.configReload(core.ConfConf)
	if x.f.a.conf().AttentionNotify {
		t.Fatal("précondition : ATTENTION_NOTIFY=0 non pris en compte")
	}
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	x.waitAttention(t, "badge affiché malgré l'option à 0", true)
	time.Sleep(2500 * time.Millisecond)
	if s := n.sends(); len(s) != 0 {
		t.Errorf("%d notification(s) avec ATTENTION_NOTIFY=0 : %+v", len(s), s)
	}
}

// Changement à chaud : l'option remise à 1 s'applique à l'attente suivante.
func TestNotificationOptionAppliesHot223(t *testing.T) {
	n := recordNotifications(t)
	x := attentionProject(t)
	_ = os.WriteFile(core.ConfigPath(), []byte("ATTENTION_NOTIFY=0\n"), 0o644)
	x.f.a.configReload(core.ConfConf)
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	x.waitAttention(t, "badge", true)
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	x.waitAttention(t, "retrait", false)
	_ = os.WriteFile(core.ConfigPath(), []byte("ATTENTION_NOTIFY=1\n"), 0o644)
	x.f.a.configReload(core.ConfConf)
	time.Sleep(1100 * time.Millisecond)
	x.hook(t, "Notification", attPayloadPerm, x.leader)
	waitUntil(t, "notification après réactivation", attSignalWithin, func() bool { return len(n.sends()) == 1 })
}

// Clic sur la notification : l'interface sélectionne l'onglet du projet (évènement attentionFocus), aussi après la fin de l'attente.
func TestNotificationClickSelectsTab223(t *testing.T) {
	n := recordNotifications(t)
	x := attentionProject(t)
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	waitUntil(t, "notification", attSignalWithin, func() bool { return len(n.sends()) == 1 })
	id := n.sends()[0].id
	x.f.a.notificationClicked(id)
	ev := x.f.r.wait(t, 2*time.Second, "attentionFocus", func(e map[string]any) bool { return e["_name"] == "attentionFocus" })
	if ev["path"] != x.f.path || ev["id"] != id {
		t.Errorf("attentionFocus = %v, attendu le projet %s et l'id %s", ev, x.f.path, id)
	}
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	waitUntil(t, "retrait", attSignalWithin, func() bool { return len(n.removes()) == 1 })
	before := len(x.f.r.all())
	x.f.a.notificationClicked(id) // attente déjà finie : le clic tardif sélectionne QUAND MÊME l'onglet (contrat § 7)
	late := x.f.r.wait(t, 2*time.Second, "attentionFocus après la fin", func(e map[string]any) bool { return e["_name"] == "attentionFocus" && x.f.r.count() > before })
	if late["path"] != x.f.path || late["id"] != id {
		t.Errorf("clic tardif : %v, attendu le projet %s et l'id %s", late, x.f.path, id)
	}
	n1 := 0
	for _, e := range x.f.r.since(before) {
		if e["_name"] == "attentionFocus" {
			n1++
		}
	}
	if n1 != 1 {
		t.Errorf("%d attentionFocus pour un clic tardif, attendu 1", n1)
	}
	// id jamais vu : aucun projet à sélectionner, aucun évènement
	before = x.f.r.count()
	x.f.a.notificationClicked("999-0")
	time.Sleep(200 * time.Millisecond)
	for _, e := range x.f.r.since(before) {
		if e["_name"] == "attentionFocus" {
			t.Errorf("clic sur un id inconnu : %v", e)
		}
	}
}

// Notifications indisponibles (build sans Wails, mode web) : badge seul, aucune erreur.
func TestNotificationUnavailableBadgeOnly223(t *testing.T) {
	oldS, oldR := notifySendHook.Set(nil), notifyRemoveHook.Set(nil)
	defer func() { notifySendHook.Set(oldS); notifyRemoveHook.Set(oldR) }()
	x := attentionProject(t)
	x.hook(t, "PreToolUse", attPayloadQ, x.leader)
	x.waitAttention(t, "badge sans notifications", true)
	x.hook(t, "PostToolUse", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, x.leader)
	x.waitAttention(t, "retrait", false)
}
