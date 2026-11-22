package desktop

import (
	"log"
	"path/filepath"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/internal/tun"
)

// Teamleader en attente de réponse (#223, contracts/attention.md § 5-6) : relevé des dossiers d'état toutes les secondes,
// sur les projets ouverts ici et ceux dont la session vit ailleurs (autre launcher, pod) — jamais tout l'arbre. Une
// lecture de dossier par projet, par le partage : aucun wsl.exe / kubectl exec.

// attentionEvery : période du relevé (l'attente est affichée ≤ 5 s après le hook) : 1 s, pour laisser de la marge à une
// machine chargée (une lecture de dossier par projet ouvert).
const attentionEvery = time.Second

// startAttention : lance (une fois) la boucle de relevé ; elle s'arrête avec l'App (a.quit).
func (a *App) startAttention() {
	a.attnOnce.Do(func() { go a.every(attentionEvery, a.attentionTick) })
}

// attentionTick : un relevé ; émet « attention » (nouvelle attente, id nouveau) et « attentionEnd » (disparue).
func (a *App) attentionTick() {
	type target struct {
		path   string
		term   *projTerm
		leader string
	}
	var targets []target
	a.mu.Lock() // seulement la copie des cibles : aucun verrou de terminal ni E/S sous a.mu
	seen := map[string]bool{}
	for p, t := range a.terms {
		seen[p] = true
		targets = append(targets, target{path: p, term: t})
	}
	for p, l := range a.ctr.Live {
		if !seen[p] && l.Claude { // session vivante ailleurs : fichier du pane le plus récent
			seen[p] = true
			targets = append(targets, target{path: p})
		}
	}
	a.mu.Unlock()
	for i := range targets {
		if t := targets[i].term; t != nil {
			targets[i].leader = t.leaderPane()
		}
	}

	now := map[string]core.Attention{}
	for _, t := range targets {
		if at, ok := core.ReadAttentionOf(t.path, t.leader); ok {
			now[t.path] = at
		}
	}

	type ev struct {
		name string
		data map[string]any
	}
	var evs []ev
	a.mu.Lock()
	for p, old := range a.attn {
		if cur, ok := now[p]; !ok || cur.ID != old.ID {
			evs = append(evs, ev{"attentionEnd", map[string]any{"path": p, "id": old.ID}})
		}
	}
	for p, cur := range now {
		if old, ok := a.attn[p]; !ok || old.ID != cur.ID {
			evs = append(evs, ev{"attention", map[string]any{"path": p, "name": filepath.Base(p), "kind": cur.Kind, "text": cur.Text, "id": cur.ID}})
		}
	}
	changed := len(evs) > 0
	a.attn = now
	a.mu.Unlock()
	if !changed {
		return
	}
	a.changed() // l'instantané porte Row.attention / TabInfo.attention
	for _, e := range evs {
		a.emit(e.name, e.data)
		switch e.name {
		case "attention":
			a.notifyAttention(e.data)
		case "attentionEnd":
			a.notifyEnd(e.data)
		}
	}
}

// ── Notifications de la fenêtre native (#223, contracts/attention.md § 7) ─────

var (
	// notifySendHook : notification système (Wails SendNotification) ; nil : indisponible (build sans Wails, mode web,
	// IsNotificationAvailable faux) — le badge seul signale l'attente. Remplacé dans les tests.
	notifySendHook = tun.Of[func(id, title, body string) error](nil)
	// notifyRemoveHook : retire la notification d'un id (RemoveNotification).
	notifyRemoveHook = tun.Of[func(id string)](nil)
)

// notifyAttention : une notification par id (la boucle n'émet « attention » qu'une fois par id), aucune si l'option
// ATTENTION_NOTIFY vaut 0 ou si les notifications sont indisponibles.
func (a *App) notifyAttention(d map[string]any) {
	send := notifySendHook.Get()
	if send == nil || !a.conf().AttentionNotify {
		return
	}
	id, _ := d["id"].(string)
	path, _ := d["path"].(string)
	name, _ := d["name"].(string)
	text, _ := d["text"].(string)
	kind, _ := d["kind"].(string)
	if id == "" {
		return
	}
	a.notifMu.Lock()
	if a.notified == nil {
		a.notified = map[string]string{}
	}
	if _, dup := a.notified[id]; dup {
		a.notifMu.Unlock()
		return
	}
	a.notified[id] = path
	a.notifMu.Unlock()
	title := "❓ " + name + " : question du teamleader"
	if kind == "permission" {
		title = "❓ " + name + " : permission demandée"
	}
	if err := send(id, title, text); err != nil {
		log.Printf("notification d'attente (%s) : %v", name, err)
	}
}

// notifyEnd : retire la notification de l'attente terminée (aucune notification après la fin).
func (a *App) notifyEnd(d map[string]any) {
	id, _ := d["id"].(string)
	a.notifMu.Lock()
	path, had := a.notified[id]
	delete(a.notified, id)
	if had { // le chemin reste connu après la fin : un clic tardif sélectionne quand même l'onglet
		if a.notifPast == nil {
			a.notifPast = map[string]string{}
		}
		if len(a.notifPast) >= 64 {
			clear(a.notifPast)
		}
		a.notifPast[id] = path
	}
	a.notifMu.Unlock()
	if rm := notifyRemoveHook.Get(); had && rm != nil {
		rm(id)
	}
}

// notificationClicked : clic sur la notification id — le frontend sélectionne l'onglet du projet (évènement
// « attentionFocus » {path}) ; la mise au premier plan de la fenêtre est faite par l'appelant (Wails).
func (a *App) notificationClicked(id string) {
	a.notifMu.Lock()
	path, ok := a.notified[id]
	if !ok {
		path, ok = a.notifPast[id] // attente déjà finie : l'onglet est sélectionné quand même
	}
	a.notifMu.Unlock()
	if ok && a.emitNative != nil { // fenêtre native seule : jamais vers le navigateur miroir
		a.emitNative("attentionFocus", map[string]any{"path": path, "id": id})
	}
}
