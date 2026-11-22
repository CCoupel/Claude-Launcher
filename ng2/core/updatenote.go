package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Avertissement d'une mise à jour réussie (#218 : faite sans sauvegarde de l'ancien binaire), transmis au navigateur
// de la coque qui est relancé juste après : écrit dans le cache, repris et supprimé au démarrage suivant.

func updateNotePath() string { return filepath.Join(CacheDir(), "update-note") }

// SaveUpdateNote : mémorise note pour le prochain démarrage du navigateur.
func SaveUpdateNote(note string) {
	_ = os.MkdirAll(CacheDir(), 0o755)
	_ = os.WriteFile(updateNotePath(), []byte(strings.TrimSpace(note)+"\n"), 0o644)
}

// TakeUpdateNote : note mémorisée (vidée à la lecture) ; "" s'il n'y en a pas.
func TakeUpdateNote() string {
	b, err := os.ReadFile(updateNotePath())
	if err != nil {
		return ""
	}
	_ = os.Remove(updateNotePath())
	return strings.TrimSpace(string(b))
}
