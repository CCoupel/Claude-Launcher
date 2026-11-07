package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Groupe de serveurs (#61) ──────────────────────────────────────────────
//
// Un backend docker, podman ou ssh peut viser plusieurs serveurs (hosts:). Le
// projet est affecté au groupe ; son serveur est choisi par le launcher :
//   - aucun registre : les conteneurs sont la seule source (labels cl.launcher,
//     cl.project, cl.backend) ; le serveur d'un conteneur est celui où on le
//     trouve ;
//   - affichage : le scan régulier du navigateur (10 s) interroge chaque
//     serveur en parallèle ;
//   - justesse : à l'ouverture, tous les serveurs sont réinterrogés sous un
//     verrou ; un projet garde son serveur tant que son conteneur existe, sinon
//     il va sur celui qui porte le moins de conteneurs NG2 en marche.
// Chaque serveur est un backend « membre » (at) : même nom, host fixé ; tout ce
// qui agit sur un conteneur passe par le membre de son serveur (Engine.of).

func (b BackendConf) pool() bool { return len(b.Hosts) > 0 }

// servers : serveurs du backend (un seul hors groupe).
func (b BackendConf) servers() []string {
	if b.pool() {
		return b.Hosts
	}
	return []string{b.Host}
}

// at : membre du groupe pour le serveur h (le backend lui-même hors groupe).
func (b BackendConf) at(h string) BackendConf {
	if !b.pool() || h == "" {
		return b
	}
	c := b
	c.Host, c.Server, c.Hosts = h, h, nil
	return c
}

// of : moteur du serveur où se trouve le conteneur f.
func (e Engine) of(f Found) Engine { return Engine{e.b.at(f.Server)} }

// listPool : conteneurs NG2 de tous les serveurs, interrogés en parallèle ;
// down : serveurs qui n'ont pas répondu, avec leur erreur.
func (e Engine) listPool() (fs []Found, down map[string]error) {
	down = map[string]error{}
	if !e.b.pool() {
		l, err := e.listOne()
		if err != nil {
			down[e.b.Host] = err
		}
		return l, down
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range e.b.Hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			l, err := Engine{e.b.at(h)}.listOne()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				down[h] = err
				return
			}
			for i := range l {
				l[i].Server = h
			}
			fs = append(fs, l...)
		}(h)
	}
	wg.Wait()
	sort.Slice(fs, func(i, j int) bool { return fs[i].Server+fs[i].Name < fs[j].Server+fs[j].Name })
	return fs, down
}

// downText : « srv1, srv2 » (serveurs injoignables, triés).
func downText(down map[string]error) string {
	var l []string
	for h := range down {
		l = append(l, h)
	}
	sort.Strings(l)
	return strings.Join(l, ", ")
}

// busiest : nombre de conteneurs NG2 en marche par serveur joignable.
func loadOf(b BackendConf, fs []Found, down map[string]error) map[string]int {
	n := map[string]int{}
	for _, h := range b.servers() {
		if _, ko := down[h]; !ko {
			n[h] = 0
		}
	}
	for _, f := range fs {
		if _, ok := n[f.Server]; ok && f.Running {
			n[f.Server]++
		}
	}
	return n
}

// leastLoaded : serveur joignable le moins chargé (ordre de hosts en cas d'égalité).
func leastLoaded(b BackendConf, load map[string]int) string {
	best := ""
	for _, h := range b.servers() {
		if n, ok := load[h]; ok && (best == "" || n < load[best]) {
			best = h
		}
	}
	return best
}

// ── Verrou de création ────────────────────────────────────────────────────
//
// Fichier créé en exclusif dans le projet le temps de choisir le serveur et de
// créer le conteneur ; deux launchers qui ouvrent le même projet au même instant
// ne le créent pas sur deux serveurs. Au-delà de lockStale, il est tenu pour
// abandonné : sa perte est sans conséquence (il ne protège que la création).

var lockStale = time.Minute

func lockPath(proj string) string { return filepath.Join(proj, ".claude-launcher", ".lock") }

// lockProject : prend le verrou (attente jusqu'à wait) ; renvoie de quoi le rendre.
func lockProject(proj string, wait time.Duration) (func(), error) {
	p := lockPath(proj)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%s %s\n", launcherID(), time.Now().Format(time.RFC3339))
			f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > lockStale {
			_ = os.Remove(p) // abandonné
			continue
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(p)
			return nil, fmt.Errorf("projet en cours de création par un autre launcher (%s) : réessayer dans un instant", strings.TrimSpace(string(b)))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ── Choix du serveur à l'ouverture ────────────────────────────────────────

// asker : question posée dans la fenêtre du projet (réponse lue sur in).
type asker struct {
	out io.Writer
	in  *bufio.Reader
}

func (a asker) ask(q string) string {
	fmt.Fprint(a.out, q)
	l, err := a.in.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(l))
}

// place : moteur du serveur du projet dans le groupe (le backend lui-même hors
// groupe). Le verrou rendu (unlock) est à libérer une fois le conteneur créé.
func (e Engine) place(proj string, a asker) (Engine, func(), error) {
	noop := func() {}
	if !e.b.pool() {
		return e, noop, nil
	}
	find := func() ([]Found, []Found, map[string]error) {
		fs, down := e.listPool()
		var mine []Found
		for _, f := range fs {
			if f.Project == proj {
				mine = append(mine, f)
			}
		}
		return fs, mine, down
	}
	_, mine, _ := find()
	if len(mine) == 0 {
		// personne ne l'a créé : sous verrou, on réinterroge avant de choisir
		unlock, err := lockProject(proj, 30*time.Second)
		if err != nil {
			return e, noop, err
		}
		fs, mine2, down2 := find()
		if len(mine2) == 0 {
			load := loadOf(e.b, fs, down2)
			best := leastLoaded(e.b, load)
			if best == "" {
				unlock()
				return e, noop, fmt.Errorf("groupe %s : aucun serveur ne répond (%s)", e.b.Name, downText(down2))
			}
			if len(down2) > 0 {
				r := a.ask(fmt.Sprintf("⚠ %s injoignable(s) : le projet y est peut-être. Démarrer quand même sur %s ? [o/N] ", downText(down2), best))
				if r != "o" && r != "y" {
					unlock()
					return e, noop, fmt.Errorf("ouverture annulée : %s injoignable(s)", downText(down2))
				}
			}
			fmt.Fprintf(a.out, "  serveur choisi : %s (%d conteneur(s) NG2 en marche)\n", best, load[best])
			return Engine{e.b.at(best)}, unlock, nil
		}
		unlock()
		mine = mine2
	}
	if len(mine) > 1 {
		return e.resolveDup(mine, a)
	}
	fmt.Fprintf(a.out, "  serveur : %s\n", mine[0].Server)
	return e.of(mine[0]), noop, nil
}

// resolveDup : le même projet sur plusieurs serveurs — demander lequel garder ;
// les autres conteneurs sont supprimés (avec leurs volumes anonymes).
func (e Engine) resolveDup(mine []Found, a asker) (Engine, func(), error) {
	noop := func() {}
	fmt.Fprintf(a.out, "\n⚠ conteneur en double pour ce projet :\n")
	for i, f := range mine {
		st := "arrêté"
		if f.Running {
			st = "en marche"
		}
		fmt.Fprintf(a.out, "  %d  %s  (%s)\n", i+1, f.Server, st)
	}
	r := a.ask(fmt.Sprintf("garder lequel ? [1-%d, autre : annuler] ", len(mine)))
	k, err := strconv.Atoi(r)
	if err != nil || k < 1 || k > len(mine) {
		return e, noop, fmt.Errorf("doublon non résolu : ouverture annulée")
	}
	keep := mine[k-1]
	for i, f := range mine {
		if i != k-1 {
			if out, err := e.of(f).cmd("rm", "-f", "-v", f.Name).CombinedOutput(); err != nil {
				return e, noop, fmt.Errorf("suppression du doublon sur %s : %v (%s)", f.Server, err, strings.TrimSpace(string(out)))
			}
			fmt.Fprintf(a.out, "  doublon supprimé sur %s\n", f.Server)
		}
	}
	return e.of(keep), noop, nil
}
