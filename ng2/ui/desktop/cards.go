package desktop

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/core"
)

// ── Cartes ◈ État : affichage immédiat, complétées en tâche de fond (#130, #131)
//
// Au clic sur un projet, ses cartes partent tout de suite de ce qui est en
// mémoire et en cache (badges, état préparé par le gestionnaire ou le hub
// Linux, milestones) : aucune lecture du projet lui-même, lent par 9P. Ce qui
// manque (état jamais calculé, team du registre du template) est complété en
// tâche de fond, carte par carte, puis renvoyé. Chaque envoi porte son projet :
// l'interface ignore ce qui n'est plus sélectionné, et le Go n'envoie pas les
// cartes d'un projet qui ne l'est plus. Le projet survolé et les voisins sont
// préparés à l'avance.

// teamTTL : durée de vie de la team lue (registre du template du projet).
var teamTTL = 10 * time.Second

// cardsSlow : délai ajouté à la partie lente (tests de #131 : réponse lente
// simulée).
var cardsSlow time.Duration

type teamEntry struct {
	at   time.Time
	rows []TeamRow
}

var teamCache = struct {
	sync.Mutex
	m map[string]teamEntry
}{m: map[string]teamEntry{}}

// cardsFast : cartes depuis la mémoire et les caches seulement (pas d'accès
// au projet) ; Pending dit ce qui reste à compléter.
func (a *App) cardsFast(path string) Cards {
	a.mu.Lock()
	n, ok := a.node(path)
	if !ok {
		a.mu.Unlock()
		return Cards{Path: path}
	}
	p := a.projInfo(n)
	ms := a.milestones[n.Path]
	t := a.terms[n.Path]
	a.mu.Unlock()

	c := Cards{Path: n.Path, Name: n.Name, Open: p.Open}
	if ps, ok := a.cachedState(n.Path); ok {
		fillState(&c, &p, ps, ms)
	} else if ps, ok := directCached(n.Path); ok {
		fillState(&c, &p, ps, ms)
	} else {
		c.Pending = append(c.Pending, "état")
	}
	c.Legend = core.Legend(p)
	c.Milestone = msCard(ms)
	teamCache.Lock()
	te, ok := teamCache.m[n.Path]
	teamCache.Unlock()
	if ok && time.Since(te.at) < teamTTL && t == nil {
		c.Team = te.rows
	} else if ok {
		c.Team = te.rows // affichée, relue en tâche de fond
		c.Pending = append(c.Pending, "team")
	} else {
		c.Pending = append(c.Pending, "team")
	}
	return c
}

// cardsFull : cartes complètes (état calculé s'il manque, team relue).
func (a *App) cardsFull(path string) Cards {
	if cardsSlow > 0 {
		time.Sleep(cardsSlow)
	}
	c := a.cardsFast(path)
	if c.Name == "" {
		return c
	}
	var pending []string
	for _, k := range c.Pending {
		switch k {
		case "état":
			t0 := time.Now()
			ps := a.directState(path)
			a.perf.add("cartes : état", time.Since(t0))
			a.perf.trace("cartes : état", path, time.Since(t0))
			a.mu.Lock()
			n, _ := a.node(path)
			p := a.projInfo(n)
			ms := a.milestones[path]
			a.mu.Unlock()
			fillState(&c, &p, ps, ms)
			c.Legend = core.Legend(p)
		case "team":
			t0 := time.Now()
			c.Team = a.readTeam(path)
			a.perf.add("cartes : team", time.Since(t0))
			a.perf.trace("cartes : team", path+"/.claude", time.Since(t0))
		default:
			pending = append(pending, k)
		}
	}
	c.Pending = pending
	return c
}

func (a *App) readTeam(path string) []TeamRow {
	var panes []core.ProjPane
	var choices map[string]string
	if t := a.term(path); t != nil {
		panes, choices = t.teamPanes()
	}
	var rows []TeamRow
	for _, l := range core.TeamLines(core.ReadTeammates(path), path, panes, choices, a.conf().TeamHideIdle, time.Now().Unix()) {
		rows = append(rows, TeamRow{Name: l.Name, Tag: l.Tag, Companion: l.Companion,
			Model: strings.TrimPrefix(l.Model, "claude-"), Effort: l.Effort, Claude: l.Claude, Pane: l.Pane,
			Started: l.Started, Active: l.Active, Hidden: l.Hidden, Pinned: l.Pinned, IdleLeft: l.IdleLeft})
	}
	teamCache.Lock()
	teamCache.m[path] = teamEntry{time.Now(), rows}
	teamCache.Unlock()
	return rows
}

func fillState(c *Cards, p *core.ProjInfo, ps core.ProjState, ms core.MsState) {
	c.Loaded = true
	c.Updated = ageText(ps.UpdatedAt)
	c.StaleUpd = time.Since(ps.UpdatedAt) > 2*time.Minute
	p.Git, p.GitOK, p.Tpl, p.TplTag = ps.Git, true, ps.Tpl, ps.TplTag
	c.IsRepo = ps.Git.IsRepo
	c.Branch = ps.Git.Branch
	c.VerLocal = core.OrNone(ps.Detail.VerLocal)
	c.Release = releaseSpans(ms, ps.Detail.VerRelease)
	c.LastMsg = ps.Detail.LastMsg
	c.Files = nil
	for _, f := range ps.Files {
		fr := FileRow{Name: f.Name, Dir: f.Dir, Meta: humanSize(f.Size), Change: ps.Detail.Changes[f.Name]}
		if f.Dir {
			fr.Meta = fmt.Sprintf("%d élém.", f.Count)
		}
		c.Files = append(c.Files, fr)
	}
}

// selected : le projet est-il encore celui de la sélection ?
func (a *App) selected(path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sel == path
}

// cardsBg : complétions et préchargements en cours (attendus par les tests).
var cardsBg sync.WaitGroup

var cardsMu sync.Mutex // relecture périodique : une seule à la fois

// selectCards : sélection d'un projet ; rend tout de suite les cartes en cache
// et complète en tâche de fond (envoyées seulement s'il est toujours choisi).
func (a *App) selectCards(path string) Cards {
	t0 := time.Now()
	c := a.cardsFast(path)
	fast := time.Since(t0)
	if len(c.Pending) > 0 {
		cardsBg.Add(1)
		go func() { defer cardsBg.Done(); a.completeCards(path, t0, fast) }()
	} else {
		a.perfCards(path, fast, 0)
	}
	return c
}

func (a *App) completeCards(path string, t0 time.Time, fast time.Duration) {
	// pas de verrou commun : la complétion d'un projet quitté ne retarde pas
	// celle du projet choisi ensuite (elle est seulement jetée)
	if !a.selected(path) {
		return // un autre projet a été choisi entre-temps
	}
	c := a.cardsFull(path)
	if !a.selected(path) {
		return // réponse pour un projet qui n'est plus sélectionné : jetée (#131)
	}
	a.emit("cards", c)
	a.perfCards(path, fast, time.Since(t0))
}

// perfCards : durées de l'affichage des cartes (journal #130).
func (a *App) perfCards(path string, fast, full time.Duration) {
	a.perf.add("cartes : cache", fast)
	if full > 0 {
		a.perf.add("cartes : complètes", full)
	}
	a.perf.writeLine(fmt.Sprintf("cartes %s : cache %s, complètes %s", baseName(path),
		fast.Round(time.Millisecond), full.Round(time.Millisecond)))
}

// prefetchCards : prépare les cartes d'un projet survolé ou voisin (état et
// team en cache), sans rien envoyer ; rend les cartes depuis le cache.
func (a *App) prefetchCards(path string) Cards {
	c := a.cardsFast(path)
	if len(c.Pending) > 0 {
		cardsBg.Add(1)
		go func() {
			defer cardsBg.Done()
			for _, k := range c.Pending {
				switch k {
				case "état":
					_ = a.directState(path)
				case "team":
					_ = a.readTeam(path)
				}
			}
		}()
	}
	return c
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ── Cache d'état du hub Linux en mémoire (#130) ───────────────────────────
// hub_cache est souvent lu par 9P : jamais au clic ni avant l'affichage ; il
// est relu en tâche de fond (au démarrage puis toutes les minutes) et les
// cartes le prennent en mémoire.

var hubStates = struct {
	sync.Mutex
	m map[string]core.ProjState
	t map[string]time.Time
}{m: map[string]core.ProjState{}, t: map[string]time.Time{}}

// cachedState : état le plus frais en cache — local (disque local), ou celui
// du hub Linux lu en tâche de fond.
func (a *App) cachedState(p string) (core.ProjState, bool) {
	ps, mt, ok := core.ReadState(p)
	hubStates.Lock()
	hs, hok := hubStates.m[p]
	ht := hubStates.t[p]
	hubStates.Unlock()
	if hok && (!ok || ht.After(mt)) {
		return hs, true
	}
	return ps, ok
}

// hubCacheLoop : lecture du cache du hub Linux, notée par projet lent.
func (a *App) hubCacheLoop() {
	if a.hubCache == "" {
		return
	}
	read := func() {
		a.mu.Lock()
		paths := a.projectPaths()
		a.mu.Unlock()
		t0, n := time.Now(), 0
		for _, p := range paths {
			t := time.Now()
			hs, hmt, ok := core.ReadStateFrom(a.hubCache, core.RemotePath(p), p)
			a.perf.trace("cache du hub", a.hubCache+" ← "+p, time.Since(t))
			if !ok {
				continue
			}
			n++
			hubStates.Lock()
			hubStates.m[p], hubStates.t[p] = hs, hmt
			hubStates.Unlock()
			a.mu.Lock()
			if _, mt, lok := core.ReadState(p); !lok || hmt.After(mt) {
				a.git[p], a.tpl[p], a.detail[p] = hs.Git, hs.Tpl, hs.Detail
				a.fromCache[p] = true
			}
			a.mu.Unlock()
		}
		a.perf.trace("cache du hub", a.hubCache, time.Since(t0), fmt.Sprintf("%d états lus sur %d projets", n, len(paths)))
		a.changed()
	}
	a.every(time.Minute, read)
}
