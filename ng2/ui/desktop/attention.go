package desktop

import (
	"path/filepath"
	"time"

	"claude-launcher-ng2/core"
)

// Teamleader en attente de réponse (#223, contracts/attention.md § 5-6) : relevé des dossiers d'état toutes les 2 s,
// sur les projets ouverts ici et ceux dont la session vit ailleurs (autre launcher, pod) — jamais tout l'arbre. Une
// lecture de dossier par projet, par le partage : aucun wsl.exe / kubectl exec.

// attentionEvery : période du relevé (l'attente est affichée ≤ 5 s après le hook).
const attentionEvery = 2 * time.Second

// startAttention : lance (une fois) la boucle de relevé ; elle s'arrête avec l'App (a.quit).
func (a *App) startAttention() {
	a.attnOnce.Do(func() { go a.every(attentionEvery, a.attentionTick) })
}

// attentionTick : un relevé ; émet « attention » (nouvelle attente, id nouveau) et « attentionEnd » (disparue).
func (a *App) attentionTick() {
	type target struct{ path, leader string }
	var targets []target
	a.mu.Lock()
	seen := map[string]bool{}
	for p, t := range a.terms {
		seen[p] = true
		targets = append(targets, target{p, t.leaderPane()})
	}
	for p, l := range a.ctr.Live {
		if !seen[p] && l.Claude { // session vivante ailleurs : fichier du pane le plus récent
			seen[p] = true
			targets = append(targets, target{p, ""})
		}
	}
	a.mu.Unlock()

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
	}
}
