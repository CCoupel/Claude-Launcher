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

	"gopkg.in/yaml.v3"
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

func (b BackendConf) IsPool() bool { return len(b.Hosts) > 0 }

// servers : serveurs du backend (un seul hors groupe).
func (b BackendConf) Servers() []string {
	if b.IsPool() {
		return b.Hosts
	}
	return []string{b.Host}
}

// at : membre du groupe pour le serveur h (le backend lui-même hors groupe).
func (b BackendConf) At(h string) BackendConf {
	if !b.IsPool() || h == "" {
		return b
	}
	c := b
	c.Host, c.Server, c.Hosts = h, h, nil
	return c
}

// of : moteur du serveur où se trouve le conteneur f.
func (e Engine) Of(f Found) Engine { return Engine{e.B.At(f.Server)} }

// listPool : conteneurs NG2 de tous les serveurs, interrogés en parallèle ;
// down : serveurs qui n'ont pas répondu, avec leur erreur.
func (e Engine) ListPool() (fs []Found, down map[string]error) {
	down = map[string]error{}
	if !e.B.IsPool() {
		l, err := e.listOne()
		if err != nil {
			down[e.B.Host] = err
		}
		return l, down
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range e.B.Hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			l, err := Engine{e.B.At(h)}.listOne()
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
func DownText(down map[string]error) string {
	var l []string
	for h := range down {
		l = append(l, h)
	}
	sort.Strings(l)
	return strings.Join(l, ", ")
}

// busiest : nombre de conteneurs NG2 en marche par serveur joignable.
func LoadOf(b BackendConf, fs []Found, down map[string]error) map[string]int {
	n := map[string]int{}
	for _, h := range b.Servers() {
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

// leastLoaded : serveur joignable, hors drain, le moins chargé (ordre de hosts
// en cas d'égalité).
func leastLoaded(b BackendConf, load map[string]int) string {
	best := ""
	for _, h := range b.Servers() {
		if n, ok := load[h]; ok && !b.Drain[h] && (best == "" || n < load[best]) {
			best = h
		}
	}
	return best
}

// ── Pools de machines nommés (#64) ───────────────────────────────────────
//
//	pools:
//	  dev: { hosts: [srv1, srv2, srv3], drain: [srv3] }
//	backends:
//	  docker-dev: { type: docker, pool: dev, host: "tcp://{}:2375", … }
//	  ssh-dev:    { type: ssh,    pool: dev, host: "cyril@{}", … }
//
// Un backend qui cite un pool devient un groupe de serveurs (hosts) : chaque
// machine donne une adresse par le modèle host ({} = la machine). drain : plus
// de nouveaux projets (la machine reste interrogée). Les backends d'un même
// pool partagent la charge de chaque machine (Mates).

type PoolConf struct {
	Hosts []string `yaml:"hosts"`
	Drain []string `yaml:"drain"`
}

// UnmarshalYAML : un pool est toujours un objet (une liste simple obligerait à
// réécrire la ligne pour mettre une machine en drain) ; clés connues seulement.
func (p *PoolConf) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("ligne %d : un pool s'écrit { hosts: [srv1, srv2], drain: [] }, pas une liste", n.Line)
	}
	for i := 0; i < len(n.Content); i += 2 {
		if k := n.Content[i].Value; k != "hosts" && k != "drain" {
			return fmt.Errorf("ligne %d : clé de pool %q inconnue (hosts, drain)", n.Content[i].Line, k)
		}
	}
	type raw PoolConf
	return n.Decode((*raw)(p))
}

// checkPools : pools non vides, sans doublon, drain inclus dans hosts.
func checkPools(ps map[string]PoolConf) error {
	for n, p := range ps {
		if len(p.Hosts) == 0 {
			return fmt.Errorf("backends.yaml : pool %q sans machine (hosts)", n)
		}
		seen := map[string]bool{}
		for _, h := range p.Hosts {
			if strings.TrimSpace(h) == "" || seen[h] {
				return fmt.Errorf("backends.yaml : pool %q : machine vide ou en double dans « hosts »", n)
			}
			seen[h] = true
		}
		dr := map[string]bool{}
		for _, h := range p.Drain {
			if !seen[h] {
				return fmt.Errorf("backends.yaml : pool %q : %q en drain mais absent de « hosts »", n, h)
			}
			if dr[h] {
				return fmt.Errorf("backends.yaml : pool %q : %q en double dans « drain »", n, h)
			}
			dr[h] = true
		}
	}
	return nil
}

// usePool : remplit hosts (adresses), machines et drain depuis le pool cité.
func (c *BackendConf) usePool(ps map[string]PoolConf) error {
	if c.Type != "podman" && c.Type != "docker" && c.Type != "ssh" {
		return fmt.Errorf("« pool » réservé à podman, docker et ssh")
	}
	if len(c.Hosts) > 0 {
		return fmt.Errorf("« pool » ou « hosts », pas les deux")
	}
	p, ok := ps[c.Pool]
	if !ok {
		var l []string
		for n := range ps {
			l = append(l, n)
		}
		sort.Strings(l)
		return fmt.Errorf("pool %q inconnu (pools déclarés : %s)", c.Pool, strings.Join(l, ", "))
	}
	if c.Host != "" && !strings.Contains(c.Host, "{}") {
		return fmt.Errorf("avec « pool », « host » est un modèle d'adresse où {} est la machine (ex. tcp://{}:2375) ; %q n'en contient pas", c.Host)
	}
	addr := func(m string) string {
		if c.Host == "" {
			return m
		}
		return strings.ReplaceAll(c.Host, "{}", m)
	}
	c.Machines, c.Drain = map[string]string{}, map[string]bool{}
	for _, m := range p.Hosts {
		a := addr(m)
		c.Hosts = append(c.Hosts, a)
		c.Machines[a] = m
	}
	for _, m := range p.Drain {
		c.Drain[addr(m)] = true
	}
	c.Host = ""
	return nil
}

// linkMates : chaque backend d'un pool connaît les autres backends du pool.
func linkMates(bs []BackendConf) {
	byPool := map[string][]BackendConf{}
	for _, b := range bs {
		if b.Pool != "" {
			byPool[b.Pool] = append(byPool[b.Pool], b) // copies sans Mates
		}
	}
	for i := range bs {
		for _, m := range byPool[bs[i].Pool] {
			if bs[i].Pool != "" && m.Name != bs[i].Name {
				bs[i].Mates = append(bs[i].Mates, m)
			}
		}
	}
}

// machine : machine du pool derrière le serveur h (h lui-même hors pool).
func (b BackendConf) machine(h string) string {
	if m, ok := b.Machines[h]; ok {
		return m
	}
	return h
}

// addMates : ajoute à load (par serveur de b) les conteneurs NG2 en marche des
// autres backends du pool, machine par machine (interrogés en parallèle).
func AddMates(b BackendConf, load map[string]int) {
	if len(b.Mates) == 0 {
		return
	}
	own := map[string]string{} // machine → serveur de b
	for _, h := range b.Hosts {
		own[b.machine(h)] = h
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, m := range b.Mates {
		wg.Add(1)
		go func(m BackendConf) {
			defer wg.Done()
			fs, _ := Engine{m}.ListPool()
			mu.Lock()
			defer mu.Unlock()
			for _, f := range fs {
				if h, ok := own[m.machine(f.Server)]; ok && f.Running {
					if _, up := load[h]; up {
						load[h]++
					}
				}
			}
		}(m)
	}
	wg.Wait()
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
			fmt.Fprintf(f, "%s %s\n", LauncherID(), time.Now().Format(time.RFC3339))
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
type Asker struct {
	Out io.Writer
	In  *bufio.Reader
}

func (a Asker) ask(q string) string {
	fmt.Fprint(a.Out, q)
	l, err := a.In.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(l))
}

// place : moteur du serveur du projet dans le groupe (le backend lui-même hors
// groupe). Le verrou rendu (unlock) est à libérer une fois le conteneur créé.
func (e Engine) Place(proj string, a Asker) (Engine, func(), error) {
	noop := func() {}
	if !e.B.IsPool() {
		return e, noop, nil
	}
	find := func() ([]Found, []Found, map[string]error) {
		fs, down := e.ListPool()
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
			load := LoadOf(e.B, fs, down2)
			AddMates(e.B, load)
			best := leastLoaded(e.B, load)
			if best == "" {
				unlock()
				if len(load) > 0 {
					return e, noop, fmt.Errorf("groupe %s : aucun serveur disponible (tous en drain)", e.B.Name)
				}
				return e, noop, fmt.Errorf("groupe %s : aucun serveur ne répond (%s)", e.B.Name, DownText(down2))
			}
			if len(down2) > 0 {
				r := a.ask(fmt.Sprintf("⚠ %s injoignable(s) : le projet y est peut-être. Démarrer quand même sur %s ? [o/N] ", DownText(down2), best))
				if r != "o" && r != "y" {
					unlock()
					return e, noop, fmt.Errorf("ouverture annulée : %s injoignable(s)", DownText(down2))
				}
			}
			what := "conteneur(s) NG2 en marche"
			if len(e.B.Mates) > 0 {
				what += " sur la machine, tous backends du pool"
			}
			fmt.Fprintf(a.Out, "  serveur choisi : %s (%d %s)\n", best, load[best], what)
			return Engine{e.B.At(best)}, unlock, nil
		}
		unlock()
		mine = mine2
	}
	if len(mine) > 1 {
		return e.resolveDup(mine, a)
	}
	fmt.Fprintf(a.Out, "  serveur : %s\n", mine[0].Server)
	return e.Of(mine[0]), noop, nil
}

// resolveDup : le même projet sur plusieurs serveurs — demander lequel garder ;
// les autres conteneurs sont supprimés (avec leurs volumes anonymes).
func (e Engine) resolveDup(mine []Found, a Asker) (Engine, func(), error) {
	noop := func() {}
	fmt.Fprintf(a.Out, "\n⚠ conteneur en double pour ce projet :\n")
	for i, f := range mine {
		st := "arrêté"
		if f.Running {
			st = "en marche"
		}
		fmt.Fprintf(a.Out, "  %d  %s  (%s)\n", i+1, f.Server, st)
	}
	r := a.ask(fmt.Sprintf("garder lequel ? [1-%d, autre : annuler] ", len(mine)))
	k, err := strconv.Atoi(r)
	if err != nil || k < 1 || k > len(mine) {
		return e, noop, fmt.Errorf("doublon non résolu : ouverture annulée")
	}
	keep := mine[k-1]
	for i, f := range mine {
		if i != k-1 {
			if out, err := e.Of(f).cmd("rm", "-f", "-v", f.Name).CombinedOutput(); err != nil {
				return e, noop, fmt.Errorf("suppression du doublon sur %s : %v (%s)", f.Server, err, strings.TrimSpace(string(out)))
			}
			fmt.Fprintf(a.Out, "  doublon supprimé sur %s\n", f.Server)
		}
	}
	return e.Of(keep), noop, nil
}
