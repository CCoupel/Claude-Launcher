package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Surveillance de l'arborescence des projets : un dossier créé, renommé ou
// supprimé hors du launcher doit apparaître ou disparaître sans Ctrl+R.
//
// Seuls comptent les dossiers dont le contenu définit l'arbre : les racines et
// les groupes (dossiers qui ne sont pas des projets, y compris un dossier vide
// qui en deviendra un). L'intérieur des projets n'est pas regardé.
//   - disque Linux natif : inotify sur ces dossiers ;
//   - /mnt/c (9p) : inotify n'y livre aucun événement (cf. cache.go), mais la date
//     d'un dossier y change bien à chaque ajout, suppression ou renommage, fait
//     depuis Linux comme depuis Windows (vérifié). On compare donc toutes les
//     treePollEvery la date de ces quelques dossiers (7 stat ≈ 3 ms) au lieu de
//     relire tout l'arbre (≈ 70 ms).
// L'arbre n'est relu (scanTree) qu'après un changement constaté.

const (
	treePollEvery = 3 * time.Second
	treeDebounce  = 300 * time.Millisecond
)

// treeDirs : racines et groupes, parcourus comme scanTree.
func treeDirs(roots []string) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth >= maxScanDepth {
			return
		}
		out = append(out, dir)
		for _, name := range subdirs(dir) {
			if child := filepath.Join(dir, name); !isProjectDir(child) {
				walk(child, depth+1)
			}
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// dirsSig : date de chaque dossier (« - » s'il a disparu).
func dirsSig(dirs []string) string {
	var b strings.Builder
	for _, d := range dirs {
		b.WriteString(d)
		b.WriteByte('=')
		if st, err := os.Stat(d); err == nil {
			b.WriteString(st.ModTime().Format(time.RFC3339Nano))
		} else {
			b.WriteByte('-')
		}
		b.WriteByte(';')
	}
	return b.String()
}

// watchTree : envoie sur le canal rendu à chaque changement de l'arborescence
// (rafales regroupées ; un signal en attente suffit).
func watchTree(roots []string) <-chan struct{} {
	c := make(chan struct{}, 1)
	go runTreeWatch(roots, c)
	return c
}

func runTreeWatch(roots []string, c chan<- struct{}) {
	w, _ := fsnotify.NewWatcher()
	var events <-chan fsnotify.Event
	var errs <-chan error
	if w != nil {
		events, errs = w.Events, w.Errors
	}
	watched := map[string]bool{}
	var polled []string
	sig := ""
	setup := func() {
		keep := map[string]bool{}
		polled = polled[:0]
		for _, d := range treeDirs(roots) {
			if w == nil || onNetworkFS(d) {
				polled = append(polled, d)
				continue
			}
			keep[d] = true
			if !watched[d] && w.Add(d) == nil {
				watched[d] = true
			}
		}
		for d := range watched {
			if !keep[d] {
				_ = w.Remove(d)
				delete(watched, d)
			}
		}
		sig = dirsSig(polled)
	}
	setup()
	tick := time.NewTicker(treePollEvery)
	var debounce <-chan time.Time
	for {
		select {
		case ev := <-events:
			if ev.Op != fsnotify.Chmod && ev.Op != fsnotify.Write {
				debounce = time.After(treeDebounce)
			}
		case <-errs:
		case <-tick.C:
			if len(polled) > 0 && debounce == nil && dirsSig(polled) != sig {
				debounce = time.After(treeDebounce)
			}
		case <-debounce:
			debounce = nil
			setup()
			select {
			case c <- struct{}{}:
			default:
			}
		}
	}
}
