package core

import (
	"claude-launcher-ng2/internal/tun"
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
	presenceBeat  = tun.Of(3 * time.Second)
	presenceStale = tun.Of(10 * time.Second) // presenceSuccessorStale : un témoin de même machine et de même pid mais d'un autre processus (pod recréé sous le même
	// nom) n'est repris qu'après ce silence de battement ; un prédécesseur encore vivant (nœud isolé, pod forcé) bat
	// toutes les presenceBeat et n'est donc jamais écrasé. presenceWait : durée pendant laquelle le démarrage attend,
	// dans le processus, que ce silence soit établi avant de refuser.
	presenceSuccessorStale = tun.Of(6 * time.Second)
	presenceWait           = tun.Of(15 * time.Second)
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
	waitUntil := time.Now().Add(presenceWait.Get())
	for try, reclaims := 0, 0; ; try++ {
		if try > 400 { // 100 s d'attente au plus, jamais de boucle sans fin
			return nil, errors.New("témoin de présence inaccessible : " + p)
		}
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
		dead, successor := presenceDead(string(b)), presenceSuccessor(string(b))
		age := time.Duration(0)
		if serr == nil {
			age = time.Since(st.ModTime())
		}
		switch {
		case serr != nil: // disparu entre-temps : nouvelle tentative de création
			continue
		case dead || age >= presenceStale.Get(): // rédacteur disparu (pid mort) ou périmé : repris
			reclaims++
			if dead {
				_, _ = reclaimStaleIf(p, presenceStale.Get(), string(b))
			} else {
				_, _ = reclaimStale(p, presenceStale.Get())
			}
		case successor && age >= presenceSuccessorStale.Get(): // prédécesseur de même pid, sans battement depuis assez longtemps
			reclaims++
			// revérifié sous le verrou de reprise : un battement arrivé depuis le constat de silence n'est pas écrasé
			_, _ = reclaimStaleIfMin(p, presenceStale.Get(), string(b), presenceSuccessorStale.Get())
		case successor && time.Now().Before(waitUntil):
			// peut-être un prédécesseur qui vient d'être arrêté : attendre ICI le silence de son battement (jamais de
			// sortie en code 1 : le pod ne redémarre pas pour rien), puis le reprendre
			time.Sleep(250 * time.Millisecond)
			continue
		default:
			return nil, fmt.Errorf("launcher « %s » déjà actif (%s) sur %s : un second launcher de même nom est refusé — choisir un autre CL_HUB_NAME",
				hubName(), strings.TrimSpace(string(b)), filepath.Dir(p))
		}
		if reclaims > 4 {
			return nil, errors.New("témoin de présence illisible : " + p)
		}
	}
	stop := make(chan struct{})
	beat := presenceBeat.Get() // lu ici : le goroutine ne touche jamais la variable du paquet (tests qui la changent)
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
			// jamais le témoin d'un autre : l'identifiant de la prise est le 4e champ du contenu (pas un préfixe)
			if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), token) {
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

// presenceDead : le témoin vient d'un processus de cette machine dont le pid n'existe plus (sûr : repris tout de suite).
func presenceDead(content string) bool {
	f := strings.Fields(content)
	if len(f) < 2 || f[0] != platform.RealHostName() {
		return false
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return false
	}
	return !platform.ProcessAlive(pid)
}

// presenceSuccessor : le témoin porte le pid de CE processus mais l'identifiant d'un autre (ou aucun : ancien format) —
// le rédacteur est probablement un prédécesseur du même pid (pod recréé sous le même nom, pid 1 dans les deux cas). Le
// pid ne dit pas s'il est encore vivant (nœud isolé, pod forcé) : seul le silence de son battement le prouve, d'où
// presenceSuccessorStale. Le même processus (même identifiant) n'est jamais un prédécesseur.
func presenceSuccessor(content string) bool {
	f := strings.Fields(content)
	if len(f) < 2 || f[0] != platform.RealHostName() {
		return false
	}
	pid, err := strconv.Atoi(f[1])
	return err == nil && pid == os.Getpid() && (len(f) < 5 || f[4] != processNonce)
}
