package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/platform"
)

// ── Garde contre deux launchers de même nom (#176) ────────────────────────
//
// Deux launchers qui partagent un dossier d'état et portent le même CL_HUB_NAME auraient la même identité : chacun
// croirait piloter les sessions de l'autre. Un témoin de présence « hub-<nom>.presence » est tenu dans le dossier
// d'état (CL_CACHE_DIR) ; un second launcher du même nom refuse de démarrer. Le témoin est renouvelé toutes les
// presenceBeat ; au-delà de presenceStale, ou si son processus est mort sur la même machine, il est repris.
// Sans CL_HUB_NAME, rien n'est contrôlé (comportement de la v6.2.0).

var (
	presenceBeat  = 3 * time.Second
	presenceStale = 10 * time.Second
)

func presencePath() string { return filepath.Join(CacheDir(), "hub-"+HubNameSafe()+".presence") }

// HubNameSafe : nom du launcher utilisable dans un nom de fichier.
func HubNameSafe() string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, hubName())
}

// AcquirePresence : prend la place du launcher de ce nom (CL_HUB_NAME posé seulement). Rend la fonction qui la libère,
// ou une erreur nommant le launcher déjà actif.
func AcquirePresence() (release func(), err error) {
	if os.Getenv(platform.HubNameVar) == "" {
		return func() {}, nil
	}
	p := presencePath()
	me := fmt.Sprintf("%s %d %s", platform.RealHostName(), os.Getpid(), time.Now().Format(time.RFC3339))
	for try := 0; ; try++ {
		created, cerr := createExclusive(p, []byte(me+"\n"), 0o644)
		if cerr != nil {
			return nil, cerr
		}
		if created {
			break
		}
		b, _ := os.ReadFile(p)
		st, serr := os.Stat(p)
		if serr == nil && !presenceDead(string(b)) && time.Since(st.ModTime()) < presenceStale {
			return nil, fmt.Errorf("launcher « %s » déjà actif (%s) sur %s : un second launcher de même nom est refusé — choisir un autre CL_HUB_NAME",
				hubName(), strings.TrimSpace(string(b)), filepath.Dir(p))
		}
		if try > 3 {
			return nil, errors.New("témoin de présence illisible : " + p)
		}
		_ = os.Remove(p) // abandonné : repris
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(presenceBeat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				now := time.Now()
				_ = os.Chtimes(p, now, now)
			}
		}
	}()
	var once sync.Once
	rel := func() { once.Do(func() { close(stop); _ = os.Remove(p) }) }
	presenceMu.Lock()
	presenceRelease = rel
	presenceMu.Unlock()
	return rel, nil
}

var (
	presenceMu      sync.Mutex
	presenceRelease func()
)

// ReleasePresence : rend la place avant de relancer le binaire (mise à jour) : le nouveau processus ne doit pas se
// voir refuser par son prédécesseur.
func ReleasePresence() {
	presenceMu.Lock()
	r := presenceRelease
	presenceMu.Unlock()
	if r != nil {
		r()
	}
}

// presenceDead : le témoin vient d'un processus de cette machine qui n'existe plus.
func presenceDead(content string) bool {
	f := strings.Fields(content)
	if len(f) < 2 || f[0] != platform.RealHostName() {
		return false
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil || pid <= 0 {
		return false
	}
	return !platform.ProcessAlive(pid)
}
