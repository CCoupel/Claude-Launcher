package desktop

import (
	"encoding/json"
	"fmt"

	"claude-launcher-ng2/core"
)

// teamAgentSet : réglage du modèle et de l'effort d'un teammate depuis la carte TEAM (#250 ; contrat
// contracts/desktop-dispatch.md « v6.9.0 »). remote : requête web hors boucle locale — refusée ici, côté serveur,
// avant toute lecture. La carte TEAM du projet est renvoyée tout de suite après l'écriture.
func (a *App) teamAgentSet(remote bool, raw json.RawMessage) (any, error) {
	if remote {
		return map[string]any{"ok": false, "remote": true, "msg": "réglage réservé à la fenêtre native"}, nil
	}
	var in struct {
		Path, Agent   string
		Model, Effort json.RawMessage
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("teamAgentSet : arguments invalides : %v", err)
	}
	if err := a.checkProjectArg(true, in.Path); err != nil {
		return nil, err
	}
	model, err := optString(in.Model)
	if err != nil {
		return map[string]any{"ok": false, "msg": "modèle invalide"}, nil
	}
	effort, err := optString(in.Effort)
	if err != nil {
		return map[string]any{"ok": false, "msg": "effort invalide"}, nil
	}
	res, serr := core.SetAgentOverride(in.Path, in.Agent, model, effort)
	if t := a.term(in.Path); t != nil {
		t.resetRegistry()
	}
	if serr != nil { // refus ou échec d'écriture : message (timeout / partial portés par le résultat)
		if res.Partial != "" {
			a.pushTeam(in.Path)
		}
		return res, nil
	}
	for _, r := range a.readTeam(in.Path) { // le teammate tourne : il garde son réglage jusqu'à son prochain spawn
		if r.Agent == in.Agent && r.Started {
			res.NextSpawn = true
		}
	}
	a.pushTeam(in.Path)
	return res, nil
}

// optString : champ JSON absent → nil ; chaîne → sa valeur ; autre (null, nombre…) → erreur.
func optString(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
