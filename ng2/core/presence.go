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
// processNonce : identifiant de CE processus (tiré une fois au démarrage), écrit dans le témoin. Un pod de StatefulSet
// recréé a le même nom de machine et le même pid (1) que son prédécesseur : le pid seul ne distingue pas deux
// processus successifs, l'identifiant oui.
var processNonce = newLockToken()

func AcquirePresence() (release func(), err error) {
	if os.Getenv(platform.HubNameVar) == "" {
		return func() {}, nil
	}
	p := presencePath()
	token := newLockToken()
	// contenu : <nom réel de la machine> <pid> <date> <identifiant de cette prise> <identifiant du processus>
	me := fmt.Sprintf("%s %d %s %s %s", platform.RealHostName(), os.Getpid(), time.Now().Format(time.RFC3339), token, processNonce)
	for try, reclaims := 0, 0; ; try++ {
		created, cerr := createExclusive(p, []byte(me+"\n"), 0o644)
		if cerr != nil {
			if lockBusyErr(cerr) && try < 20 { // Windows : suppression du témoin en cours chez un lecteur : réessayer
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return nil, cerr
		}
		if created {
			break
		}
		b, _ := os.ReadFile(p)
		st, serr := os.Stat(p)
		dead := presenceDead(string(b))
		if serr == nil && !dead && time.Since(st.ModTime()) < presenceStale {
			return nil, fmt.Errorf("launcher « %s » déjà actif (%s) sur %s : un second launcher de même nom est refusé — choisir un autre CL_HUB_NAME",
				hubName(), strings.TrimSpace(string(b)), filepath.Dir(p))
		}
		if reclaims++; reclaims > 4 {
			return nil, errors.New("témoin de présence illisible : " + p)
		}
		if dead { // rédacteur disparu (même machine : pid mort, ou prédécesseur de même pid) : repris même s'il est frais
			_, _ = reclaimStaleIf(p, presenceStale, string(b))
		} else { // périmé : repris, un seul gagnant
			_, _ = reclaimStale(p, presenceStale)
		}
	}
	stop := make(chan struct{})
	beat := presenceBeat // lu ici : le goroutine ne touche jamais la variable du paquet (tests qui la changent)
	go func() {
		t := time.NewTicker(beat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				switch b, err := os.ReadFile(p); {
				case err == nil && strings.Contains(string(b), token):
					now := time.Now()
					_ = os.Chtimes(p, now, now)
				case os.IsNotExist(err): // supprimé (nettoyage du dossier d'état) : recréé
					_, _ = createExclusive(p, []byte(me+"\n"), 0o644)
				case err == nil: // repris par un autre launcher du même nom : signalé, plus de battement
					fmt.Fprintf(os.Stderr, "⚠ launcher « %s » : sa place a été prise par un autre (%s)\n", hubName(), strings.TrimSpace(string(b)))
					return
				}
			}
		}
	}()
	var once sync.Once
	rel := func() {
		once.Do(func() {
			close(stop)
			if lockOwnedBy(p, token) { // jamais le témoin d'un autre
				_ = os.Remove(p)
			}
		})
	}
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

// presenceDead : le témoin vient d'un processus de cette machine qui n'existe plus — son pid n'existe plus, ou c'est le
// pid de CE processus mais avec un autre identifiant de processus (ou sans : ancien format) : un prédécesseur du même
// pid (pod recréé sous le même nom, pid 1 dans les deux cas). Le même processus (même identifiant) n'est jamais mort.
func presenceDead(content string) bool {
	f := strings.Fields(content)
	if len(f) < 2 || f[0] != platform.RealHostName() {
		return false
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil || pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return len(f) < 5 || f[4] != processNonce
	}
	return !platform.ProcessAlive(pid)
}
