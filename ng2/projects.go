package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxScanDepth = 8

// Node est une ligne de l'arbre : un groupe (dossier thématique) ou un projet.
type Node struct {
	Name    string
	Path    string
	Depth   int
	Project bool
	Last    bool   // dernier enfant de son parent (└─ au lieu de ├─)
	Key     string // nom de fenêtre tmux (basename, désambiguïsé si collision)
	Root    bool   // en-tête d'une racine de GITHUB_DIRS
	Special string // entrée d'action (« new » : nouveau projet), pas un projet
}

func isProjectDir(d string) bool {
	if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
		return true
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.Type().IsRegular() {
			return true
		}
	}
	return false
}

func subdirs(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			out = append(out, e.Name())
		} else if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// scanTree reproduit _collect_projects/_walk_emit du launcher bash : un dossier
// est un projet s'il contient un .git ou un fichier direct, sinon un groupe
// (affiché seulement s'il contient un projet en dessous).
func scanTree(roots []string) []Node {
	var nodes []Node
	counts := map[string]int{}

	var walk func(dir string, depth int) []Node
	walk = func(dir string, depth int) []Node {
		if depth >= maxScanDepth {
			return nil
		}
		var out []Node
		for _, name := range subdirs(dir) {
			child := filepath.Join(dir, name)
			if isProjectDir(child) {
				counts[name]++
				out = append(out, Node{Name: name, Path: child, Depth: depth, Project: true})
				continue
			}
			sub := walk(child, depth+1)
			if len(sub) > 0 {
				out = append(out, Node{Name: name, Path: child, Depth: depth})
				out = append(out, sub...)
			}
		}
		return out
	}

	for _, r := range roots {
		sub := walk(r, 0)
		if len(sub) == 0 {
			continue
		}
		nodes = append(nodes, Node{Name: r, Path: r, Root: true})
		nodes = append(nodes, sub...)
	}

	// dernier enfant de chaque niveau + clé tmux
	for i := range nodes {
		n := &nodes[i]
		if n.Root {
			continue
		}
		n.Last = true
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].Root || nodes[j].Depth < n.Depth {
				break
			}
			if nodes[j].Depth == n.Depth {
				n.Last = false
				break
			}
		}
		if n.Project {
			n.Key = n.Name
			if counts[n.Name] > 1 {
				n.Key = filepath.Base(filepath.Dir(n.Path)) + "-" + n.Name
			}
		}
	}
	return nodes
}
