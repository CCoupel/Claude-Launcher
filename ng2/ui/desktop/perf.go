package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/core"
	"claude-launcher-ng2/platform"
)

// ── Lecture des projets : mesures et caches (#125) ────────────────────────
//
// Sous Windows, les projets sont lus par \\wsl.localhost (9P) : chaque
// opération de fichier traverse la frontière WSL et un « git status » par
// projet coûte cher. D'où :
//   - un journal des durées par étape (parcours, git, conso, milestones,
//     conteneurs), écrit dans desktop-perf.log du cache et résumé au pied ;
//   - l'arbre et les badges affichés tout de suite depuis le cache du
//     lancement précédent, puis complétés en tâche de fond ;
//   - le cache d'état du hub Linux (hubs.<machine>.hub_cache), lu sans y
//     écrire, quand il est plus frais que le cache local ;
//   - git en parallèle limité, projets ouverts et sélectionné d'abord, et
//     seulement pour les projets sans état en cache (les autres sont tenus à
//     jour par le gestionnaire d'état).

// perfLog : durées des étapes du chargement.
type perfLog struct {
	mu    sync.Mutex
	steps map[string]time.Duration
	order []string
}

func (p *perfLog) add(step string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.steps == nil {
		p.steps = map[string]time.Duration{}
	}
	if _, ok := p.steps[step]; !ok {
		p.order = append(p.order, step)
	}
	p.steps[step] = d
}

// time : mesure f sous le nom step.
func (p *perfLog) time(step string, f func()) {
	t := time.Now()
	f()
	p.add(step, time.Since(t))
}

// summary : « parcours 12 ms · git 3,4 s · … ».
func (p *perfLog) summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var parts []string
	for _, s := range p.order {
		parts = append(parts, s+" "+p.steps[s].Round(time.Millisecond).String())
	}
	return strings.Join(parts, " · ")
}

// write : une ligne datée dans desktop-perf.log (gardé à 200 lignes).
func (p *perfLog) write(nProjects int) {
	path := filepath.Join(core.CacheDir(), "desktop-perf.log")
	line := fmt.Sprintf("%s %s %s/%s %d projets : %s", time.Now().Format("2006-01-02 15:04:05"), core.NgVersion, runtime.GOOS, runtime.GOARCH, nProjects, p.summary())
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	lines = append(lines, line)
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// ── Caches du lancement précédent ─────────────────────────────────────────

func treeCachePath() string { return filepath.Join(core.CacheDir(), "desktop-tree.json") }
func gitCachePath() string  { return filepath.Join(core.CacheDir(), "desktop-git.json") }

type treeCache struct {
	Roots []string    `json:"roots"`
	Nodes []core.Node `json:"nodes"`
}

// cachedTree : arborescence du lancement précédent, pour les mêmes racines.
func cachedTree(roots []string) ([]core.Node, bool) {
	var c treeCache
	b, err := os.ReadFile(treeCachePath())
	if err != nil || json.Unmarshal(b, &c) != nil || strings.Join(c.Roots, "\n") != strings.Join(roots, "\n") || len(c.Nodes) == 0 {
		return nil, false
	}
	return c.Nodes, true
}

func saveTree(roots []string, nodes []core.Node) {
	writeJSON(treeCachePath(), treeCache{Roots: roots, Nodes: nodes})
}

type gitEntry struct {
	Git core.GitInfo       `json:"git"`
	Tpl core.TemplateState `json:"tpl"`
}

func loadGitCache() map[string]gitEntry {
	m := map[string]gitEntry{}
	if b, err := os.ReadFile(gitCachePath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func writeJSON(path string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// bestState : état en cache le plus frais d'un projet — cache local, ou celui
// du hub Linux (hub_cache) sous le chemin des backends.
func bestState(hubDir, p string) (core.ProjState, bool) {
	ps, mt, ok := core.ReadState(p)
	if hubDir != "" {
		if hs, hmt, hok := core.ReadStateFrom(hubDir, core.RemotePath(p), p); hok && (!ok || hmt.After(mt)) {
			return hs, true
		}
	}
	return ps, ok
}

// gitWorkers : git simultanés ; moins sur un disque réseau (9P, UNC), où
// chaque appel est déjà lent et où trop d'appels se gênent.
func gitWorkers(roots []string) int {
	for _, r := range roots {
		if platform.OnNetworkFS(r) {
			return 4
		}
	}
	return 8
}

// gitOrder : projets ouverts et sélectionné d'abord, puis dans l'ordre de
// l'arbre (les premiers affichés).
func gitOrder(paths []string, first map[string]bool) []string {
	out := append([]string(nil), paths...)
	sort.SliceStable(out, func(i, j int) bool { return first[out[i]] && !first[out[j]] })
	return out
}
