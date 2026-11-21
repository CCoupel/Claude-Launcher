package core

import (
	"errors"
	"os"
)

// ── Révélation d'un élément caché (#227) ──────────────────────────────────
//
// Les valeurs secrètes ne quittent jamais le poste dans le modèle (configModel), un instantané, un évènement, un message ni un
// journal. La seule voie est secretReveal : liste FERMÉE de clés, une valeur à la fois, à la demande de l'utilisateur.

// ErrNotRevealable : la clé n'est pas un secret révélable (liste fermée).
var ErrNotRevealable = errors.New("cette clé n'est pas un élément caché révélable")

// RevealableKey : la clé est un réglage de type « secret » du modèle de config.conf (liste fermée).
func RevealableKey(kind ConfigKind, key string) bool {
	if kind != ConfConf {
		return false
	}
	s, ok := confSettingOf(key)
	return ok && s.Type == "secret"
}

// RevealSecret : valeur du secret key de config.conf. Refus : clé hors liste fermée, ou non définie.
func RevealSecret(kind ConfigKind, key string) (string, error) {
	if !RevealableKey(kind, key) {
		return "", ErrNotRevealable
	}
	b, _ := os.ReadFile(ConfigPath())
	switch key {
	case "GITHUB_TOKEN":
		if v := ConfTokenValue(string(b)); v != "" {
			return v, nil
		}
	}
	return "", errors.New("aucune valeur définie")
}

// JournalReveal : consigne « révélé : clé, interface, heure » dans le journal des secrets — jamais la valeur, ni sa longueur ni
// son empreinte. Une erreur d'écriture du journal refuse la révélation (pas de révélation sans trace).
func JournalReveal(key, iface string) error {
	return journalSecret("", key, "révélé", "", iface)
}
