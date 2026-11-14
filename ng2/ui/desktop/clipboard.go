package desktop

import (
	"fmt"
	"time"
)

// ── Presse-papiers et collage dans les terminaux (#155, LOT-4) ────────────
//
// Le presse-papiers du poste est lu et écrit par le runtime Wails, côté Go (pas
// d'invite d'autorisation de WebView2) : wails_on.go branche les deux crochets
// au démarrage de la fenêtre. Sans eux (mode --web, binaire sans Wails) les
// deux méthodes répondent « indisponible » et le frontend prend le presse-papiers
// de son navigateur. Un client web n'atteint JAMAIS le presse-papiers de la
// machine qui héberge le serveur : en mode web les crochets ne sont pas
// consultés, même s'ils existaient.

var (
	clipboardSetHook func(text string) error // nil : pas de presse-papiers Go
	clipboardGetHook func() (string, error)  // idem
)

const (
	maxClipboardWrite = 8 << 20 // texte copié, au plus
	maxPaste          = 4 << 20 // taille maximale d'un collage (lu ou saisi)
)

// clipboardUnavailable : réponse « pas de presse-papiers Go » (le frontend prend
// navigator.clipboard).
func clipboardUnavailable() map[string]any {
	return map[string]any{"ok": false, "unavailable": true,
		"msg": "presse-papiers du poste indisponible ici : le navigateur sera utilisé (contexte sécurisé : localhost)"}
}

func (a *App) clipboardUsable() bool {
	return !a.web && clipboardSetHook != nil && clipboardGetHook != nil
}

// clipboardWrite : copie text dans le presse-papiers du poste.
func (a *App) clipboardWrite(text string) map[string]any {
	if !a.clipboardUsable() {
		return clipboardUnavailable()
	}
	if len(text) > maxClipboardWrite {
		return map[string]any{"ok": false, "msg": fmt.Sprintf("texte trop volumineux pour le presse-papiers (%d Mo au plus)", maxClipboardWrite>>20)}
	}
	if err := clipboardSetHook(text); err != nil {
		return map[string]any{"ok": false, "msg": "copie impossible : " + err.Error()}
	}
	return map[string]any{"ok": true}
}

// clipboardRead : texte du presse-papiers du poste (vide s'il n'en contient pas).
func (a *App) clipboardRead() map[string]any {
	if !a.clipboardUsable() {
		return clipboardUnavailable()
	}
	text, err := clipboardGetHook()
	if err != nil {
		return map[string]any{"ok": false, "msg": "lecture du presse-papiers impossible : " + err.Error()}
	}
	if len(text) > maxPaste {
		return map[string]any{"ok": false, "msg": fmt.Sprintf("presse-papiers trop volumineux pour un collage (%d Mo au plus)", maxPaste>>20)}
	}
	return map[string]any{"ok": true, "text": text}
}

// inputRefusal : réponse de termInput quand la saisie n'est pas partie. En lecture
// seule le motif est aussi dit en pied de volet — au plus une fois par seconde par
// onglet : une saisie ou un collage n'est plus perdu en silence.
func (a *App) inputRefusal(t *projTerm, reason string) map[string]any {
	var msg string
	switch reason {
	case "readonly":
		who := t.ownerID()
		if who == "" {
			who = "un autre launcher"
		}
		msg = "lecture seule : " + who + " a la main sur " + t.name + " — saisie et collage ignorés (Entrée sur le projet pour reprendre la main) ; copier reste possible"
	default:
		msg = fmt.Sprintf("collage trop volumineux (%d Mo au plus) : rien n'a été envoyé", maxPaste>>20)
	}
	t.mu.Lock()
	say := reason != t.refusedWhy || time.Since(t.refusedAt) > time.Second // un motif nouveau se dit tout de suite
	if say {
		t.refusedAt, t.refusedWhy = time.Now(), reason
	}
	t.mu.Unlock()
	if say {
		a.setMsgSoon("⚠ " + msg)
	}
	return map[string]any{"ok": false, "reason": reason, "msg": msg}
}

// setMsgSoon : message de pied de volet sans jamais attendre a.mu : posé tout de
// suite (ordre conservé) quand le verrou est libre, sinon en tâche de fond.
func (a *App) setMsgSoon(m string) {
	if a.mu.TryLock() {
		a.msg = m
		a.mu.Unlock()
		a.changed()
		return
	}
	go a.setMsg(m)
}
