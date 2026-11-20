package platform

import (
	"errors"
	"os"
	"time"
)

// ReplaceRetries / ReplaceFirstDelay : rename retenté sur Windows quand le fichier cible est tenu par un autre
// programme (antivirus, éditeur, indexeur) : 5 essais, délai doublé de 50 ms à 800 ms. #217
const (
	ReplaceRetries    = 5
	ReplaceFirstDelay = 50 * time.Millisecond
)

// ErrReplaceBusy : la cible reste verrouillée après tous les essais.
var ErrReplaceBusy = errors.New("fichier verrouillé par un autre programme")

// ReplaceWithRetry : logique pure du remplacement (injectable en test) — rename(), retenté tant que busy(err) et
// qu'il reste des essais ; sleep(d) attend entre deux essais. Dernière erreur occupée : jointe à ErrReplaceBusy.
func ReplaceWithRetry(rename func() error, busy func(error) bool, sleep func(time.Duration)) error {
	delay := ReplaceFirstDelay
	var err error
	for i := 0; i < ReplaceRetries; i++ {
		if err = rename(); err == nil || !busy(err) {
			return err
		}
		if i < ReplaceRetries-1 {
			sleep(delay)
			delay *= 2
		}
	}
	return errors.Join(ErrReplaceBusy, err)
}

// ReplaceFile : renomme src sur dst (écrase dst, atomique) ; Windows : retry borné sur ACCESS_DENIED /
// SHARING_VIOLATION. dst reste intact en cas d'échec.
func ReplaceFile(src, dst string) error {
	return ReplaceWithRetry(func() error { return os.Rename(src, dst) }, replaceBusy, time.Sleep)
}
