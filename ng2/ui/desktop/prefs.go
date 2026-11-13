package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"claude-launcher-ng2/core"
)

// ── Préférences d'affichage (#123, #124) ──────────────────────────────────
//
// Catégories repliées, largeur du navigateur, hauteur de l'arbre, cartes
// repliées : gardées dans desktop-ui.json du cache du launcher (pas dans le
// stockage du navigateur, propre à chaque WebView2 / navigateur et vidé avec
// lui). Contenu libre, décidé par l'interface (objet JSON).

var prefsMu sync.Mutex

func prefsPath() string { return filepath.Join(core.CacheDir(), "desktop-ui.json") }

// loadPrefs : préférences enregistrées (objet vide sinon).
func loadPrefs() map[string]any {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	m := map[string]any{}
	if b, err := os.ReadFile(prefsPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// savePrefs : fusionne patch dans les préférences (clé à null : supprimée).
func savePrefs(patch map[string]any) map[string]any {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	m := map[string]any{}
	if b, err := os.ReadFile(prefsPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	for k, v := range patch {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	writeJSON(prefsPath(), m)
	return m
}
