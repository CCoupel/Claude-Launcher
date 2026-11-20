package platform

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// ReplaceRetries / ReplaceFirstDelay : rename retenté quand la cible est tenue par un autre programme
// (antivirus, éditeur, indexeur) : 5 essais, délai doublé de 50 ms à 800 ms. #217
const (
	ReplaceRetries    = 5 // 5 essais : attentes 50, 100, 200, 400 ms
	ReplaceFirstDelay = 50 * time.Millisecond
)

// ErrReplaceBusy : la cible reste verrouillée après tous les essais.
var ErrReplaceBusy = errors.New("fichier verrouillé par un autre programme")

// IsReplaceRetryable : ERROR_ACCESS_DENIED (5) ou ERROR_SHARING_VIOLATION (32), même enveloppés (*os.LinkError).
// Logique pure, sans tag d'OS : testable partout.
func IsReplaceRetryable(err error) bool {
	var en syscall.Errno
	return errors.As(err, &en) && (en == 5 || en == 32)
}

// ReplaceWithRetry : rename(src, dst), retenté tant que IsReplaceRetryable ; sleep(d) attend entre deux essais.
// Échec final : ErrReplaceBusy jointe à la dernière cause.
func ReplaceWithRetry(src, dst string, rename func(a, b string) error, sleep func(time.Duration)) error {
	delay := ReplaceFirstDelay
	var err error
	for i := 0; i < ReplaceRetries; i++ {
		if err = rename(src, dst); err == nil || !IsReplaceRetryable(err) {
			return err
		}
		if i < ReplaceRetries-1 {
			sleep(delay)
			delay *= 2
		}
	}
	return fmt.Errorf("%w : %w", ErrReplaceBusy, err)
}

// ReplaceFile : renomme src sur dst (écrase dst, atomique). Retry borné seulement sous Windows (errno 5/32 ont un
// autre sens ailleurs) ; dst reste intact en cas d'échec.
func ReplaceFile(src, dst string) error {
	if !replaceRetries {
		return os.Rename(src, dst)
	}
	return ReplaceWithRetry(src, dst, os.Rename, time.Sleep)
}
