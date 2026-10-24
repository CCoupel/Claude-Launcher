package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Cache d'état des projets, calculé en tâche de fond.
//
// Le navigateur (qui tourne en permanence) prépare git, fichiers et template de
// chaque projet ; la vue « ◈ État » ne fait que LIRE ces fichiers de cache, donc
// la sélection est instantanée même quand `git status` prend 3 s sur /mnt/c.
//
// Détection des changements :
//   - disque Linux natif : inotify récursif sur le projet et .git (commit, branche,
//     index), regroupé par un court délai ;
//   - /mnt/c (9p/drvfs) : inotify n'y livre AUCUN événement, même pour une écriture
//     faite depuis Linux (vérifié, cf. inotify_probe_test.go) ; on interroge donc
//     à la place une « signature » bon marché (quelques stat : HEAD, index, entrées
//     de premier niveau) du projet sélectionné toutes les 2 s et des projets
//     ouverts toutes les 8 s.
//
// Filet de sécurité commun, car la signature ne voit pas l'édition d'un fichier
// existant en sous-dossier : sélectionné 10 s (≥ 3 × durée du calcul), ouverts 45 s (60 s avec inotify),
// les autres 5 min. À la sélection, recalcul seulement si la donnée a > 15 s.

// ProjState : tout ce que la vue État affiche pour un projet.
type ProjState struct {
	Path      string        `json:"path"`
	Git       GitInfo       `json:"git"`
	Detail    GitDetail     `json:"detail"`
	Tpl       TemplateState `json:"tpl"`
	TplTag    string        `json:"tpl_tag"`
	Files     []FileEntry   `json:"files"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func stateDir() string {
	d := filepath.Join(cacheDir(), "state")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func statePath(project string) string {
	h := sha1.Sum([]byte(project))
	return filepath.Join(stateDir(), hex.EncodeToString(h[:8])+".json")
}

func writeState(ps ProjState) {
	b, err := json.Marshal(ps)
	if err != nil {
		return
	}
	p := statePath(ps.Path)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// readState lit le cache d'un projet ; mtime sert à détecter une mise à jour.
func readState(project string) (ps ProjState, mtime time.Time, ok bool) {
	p := statePath(project)
	st, err := os.Stat(p)
	if err != nil {
		return ps, mtime, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &ps) != nil || ps.Path != project {
		return ps, mtime, false
	}
	return ps, st.ModTime(), true
}

func computeState(project string) ProjState {
	latest := readTrim(tplCommitPath())
	return ProjState{
		Path:      project,
		Git:       readGit(project),
		Detail:    readDetail(project),
		Tpl:       readTemplate(project, latest),
		TplTag:    templateTag(project),
		Files:     readFiles(project),
		UpdatedAt: time.Now(),
	}
}

// ── Gestionnaire ──────────────────────────────────────────────────────────

const (
	maxDirsPerProject = 1500
	debounceDelay     = 500 * time.Millisecond
	selectStale       = 15 * time.Second
	selectedRefresh   = 10 * time.Second // plancher ; relevé à 3 × la durée du dernier calcul
	openRefresh       = 45 * time.Second
	watchedRefresh    = 60 * time.Second // filet de sécurité quand inotify marche
	coldRefresh       = 5 * time.Minute
	pollSelected      = 2 * time.Second
	pollOpenEvery     = 4 // un tour sur 4 (soit 8 s) pour les projets ouverts
	stateWorkers      = 2
)

// dossiers dont le contenu ne change pas l'état affiché et qui coûteraient des
// milliers de watches
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".venv": true, "venv": true, ".cache": true,
	".next": true, ".gradle": true, ".idea": true,
}

// fichiers de .git dont le changement modifie l'état (commit, branche, fetch…)
var gitWatched = map[string]bool{
	"HEAD": true, "index": true, "packed-refs": true, "FETCH_HEAD": true, "ORIG_HEAD": true, "MERGE_HEAD": true,
}

type stateMgr struct {
	w *fsnotify.Watcher

	mu       sync.Mutex
	projects []string
	open     map[string]bool
	selected string
	pending  map[string]bool
	updated  map[string]time.Time
	dur      map[string]time.Duration // durée du dernier calcul : borne la charge disque
	dirOwner map[string]string        // dossier surveillé -> projet
	poll     map[string]bool          // projets sur disque réseau (9p) : signature interrogée
	sigs     map[string]string
	timers   map[string]*time.Timer

	hi, lo chan string
	Done   chan ProjState // projets recalculés, pour mettre à jour le navigateur
}

func newStateMgr() (*stateMgr, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	m := &stateMgr{
		w: w, open: map[string]bool{}, pending: map[string]bool{}, updated: map[string]time.Time{}, dur: map[string]time.Duration{},
		dirOwner: map[string]string{}, timers: map[string]*time.Timer{}, poll: map[string]bool{}, sigs: map[string]string{},
		hi: make(chan string, 256), lo: make(chan string, 1024), Done: make(chan ProjState, 256),
	}
	for i := 0; i < stateWorkers; i++ {
		go m.worker()
	}
	go m.events()
	go m.periodic()
	go m.poller()
	return m, nil
}

func (m *stateMgr) enqueue(p string, hi bool) {
	m.mu.Lock()
	if m.pending[p] {
		m.mu.Unlock()
		return
	}
	m.pending[p] = true
	m.mu.Unlock()
	if hi {
		m.hi <- p
	} else {
		m.lo <- p
	}
}

func (m *stateMgr) worker() {
	for {
		var p string
		select {
		case p = <-m.hi:
		default:
			select {
			case p = <-m.hi:
			case p = <-m.lo:
			}
		}
		m.mu.Lock()
		delete(m.pending, p) // un changement pendant le calcul replanifie un tour
		m.mu.Unlock()
		start := time.Now()
		ps := computeState(p)
		writeState(ps)
		m.mu.Lock()
		m.dur[p] = time.Since(start)
		m.updated[p] = ps.UpdatedAt
		m.mu.Unlock()
		select {
		case m.Done <- ps:
		default: // navigateur en retard : la prochaine lecture du cache suffira
		}
	}
}

// SetProjects : liste des projets connus ; surveille les nouveaux et prépare
// leur état (sélection et projets ouverts d'abord).
func (m *stateMgr) SetProjects(paths []string) {
	m.mu.Lock()
	old := map[string]bool{}
	for _, p := range m.projects {
		old[p] = true
	}
	m.projects = paths
	m.mu.Unlock()
	for _, p := range paths {
		if !old[p] {
			if onNetworkFS(p) {
				m.mu.Lock()
				m.poll[p] = true
				m.mu.Unlock()
			} else {
				m.watchProject(p)
			}
			if _, _, ok := readState(p); !ok {
				m.enqueue(p, false)
			}
		}
	}
}

func (m *stateMgr) SetOpen(paths map[string]bool) {
	m.mu.Lock()
	m.open = paths
	m.mu.Unlock()
}

// Select : appelé à chaque sélection. La donnée en cache s'affiche tout de
// suite ; elle n'est recalculée (en priorité) que si elle est périmée.
func (m *stateMgr) Select(p string) {
	m.mu.Lock()
	m.selected = p
	last := m.updated[p]
	m.mu.Unlock()
	if last.IsZero() {
		if ps, _, ok := readState(p); ok {
			last = ps.UpdatedAt
		}
	}
	if time.Since(last) > selectStale {
		m.enqueue(p, true)
	}
}

func (m *stateMgr) periodic() {
	for range time.Tick(2 * time.Second) {
		m.mu.Lock()
		var due []string
		var hot []bool
		for _, p := range m.projects {
			isHot := m.open[p] || p == m.selected
			age := time.Since(m.updated[p])
			limit := coldRefresh
			switch {
			case p == m.selected:
				limit = max(selectedRefresh, 3*m.dur[p])
			case m.open[p]:
				limit = max(openRefresh, 6*m.dur[p])
			}
			if isHot && !m.poll[p] {
				limit = watchedRefresh // inotify voit les changements : simple filet
			}
			if age > limit {
				due = append(due, p)
				hot = append(hot, isHot)
			}
		}
		m.mu.Unlock()
		for i, p := range due {
			m.enqueue(p, hot[i])
		}
	}
}

// ── Disque réseau : signature interrogée ──────────────────────────────────

// onNetworkFS : le projet est-il sur un système 9p (ex. /mnt/c sous WSL) ?
func onNetworkFS(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	const v9fsMagic = 0x01021997
	return int64(st.Type) == v9fsMagic
}

// projectSig : signature bon marché d'un projet — mtime de HEAD, index, refs,
// du dossier lui-même et de chacune de ses entrées de premier niveau.
func projectSig(p string) string {
	var b strings.Builder
	add := func(path string) {
		if st, err := os.Stat(path); err == nil {
			b.WriteString(path[len(p):])
			b.WriteByte('=')
			b.WriteString(st.ModTime().Format(time.RFC3339Nano))
			b.WriteByte(';')
		}
	}
	for _, f := range []string{".git/HEAD", ".git/index", ".git/packed-refs", ".git/FETCH_HEAD", ".git/refs/heads", "."} {
		add(filepath.Join(p, f))
	}
	if ents, err := os.ReadDir(p); err == nil {
		for _, e := range ents {
			if skipDirs[e.Name()] {
				continue
			}
			add(filepath.Join(p, e.Name()))
		}
	}
	return b.String()
}

func (m *stateMgr) poller() {
	tick := 0
	for range time.Tick(pollSelected) {
		tick++
		m.mu.Lock()
		var targets []string
		if m.selected != "" && m.poll[m.selected] {
			targets = append(targets, m.selected)
		}
		if tick%pollOpenEvery == 0 {
			for p := range m.open {
				if m.poll[p] && p != m.selected {
					targets = append(targets, p)
				}
			}
		}
		m.mu.Unlock()
		for _, p := range targets {
			sig := projectSig(p)
			m.mu.Lock()
			prev, seen := m.sigs[p], m.sigs[p] != ""
			m.sigs[p] = sig
			m.mu.Unlock()
			if seen && sig != prev {
				m.schedule(p)
			}
		}
	}
}

// ── inotify ───────────────────────────────────────────────────────────────

func (m *stateMgr) watchProject(p string) {
	n := 0
	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != p && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if n >= maxDirsPerProject {
			return filepath.SkipAll
		}
		m.addDir(p, path)
		n++
		return nil
	})
	// .git : HEAD, index, refs (commit, changement de branche, fetch)
	gitDir := filepath.Join(p, ".git")
	if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
		m.addDir(p, gitDir)
		_ = filepath.WalkDir(filepath.Join(gitDir, "refs"), func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				m.addDir(p, path)
			}
			return nil
		})
	}
}

func (m *stateMgr) addDir(project, dir string) {
	m.mu.Lock()
	if _, ok := m.dirOwner[dir]; ok {
		m.mu.Unlock()
		return
	}
	m.dirOwner[dir] = project
	m.mu.Unlock()
	if err := m.w.Add(dir); err != nil {
		m.mu.Lock()
		delete(m.dirOwner, dir)
		m.mu.Unlock()
	}
}

func (m *stateMgr) events() {
	for {
		select {
		case ev, ok := <-m.w.Events:
			if !ok {
				return
			}
			m.onEvent(ev)
		case <-m.w.Errors:
		}
	}
}

func (m *stateMgr) onEvent(ev fsnotify.Event) {
	if ev.Op == fsnotify.Chmod {
		return
	}
	dir := filepath.Dir(ev.Name)
	m.mu.Lock()
	project, ok := m.dirOwner[dir]
	m.mu.Unlock()
	if !ok {
		return
	}
	base := filepath.Base(ev.Name)
	inGit := dir == filepath.Join(project, ".git") || strings.HasPrefix(dir, filepath.Join(project, ".git")+string(filepath.Separator))
	switch {
	case inGit:
		// dans .git : seulement l'état utile (le reste est du bruit de git lui-même)
		if dir == filepath.Join(project, ".git") && !gitWatched[base] {
			return
		}
		if strings.HasSuffix(base, ".lock") {
			return
		}
	case skipDirs[base] && ev.Op&fsnotify.Create != 0:
		return
	default:
		// nouveau sous-dossier : à surveiller aussi
		if ev.Op&fsnotify.Create != 0 {
			if st, err := os.Stat(ev.Name); err == nil && st.IsDir() && !skipDirs[base] {
				m.addDir(project, ev.Name)
			}
		}
	}
	m.schedule(project)
}

// schedule regroupe les rafales d'événements (checkout, npm install…) en un
// seul recalcul, lancé en priorité.
func (m *stateMgr) schedule(project string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.timers[project]; ok {
		t.Reset(debounceDelay)
		return
	}
	m.timers[project] = time.AfterFunc(debounceDelay, func() {
		m.mu.Lock()
		delete(m.timers, project)
		m.mu.Unlock()
		m.enqueue(project, true)
	})
}
