package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GitInfo : état git purement local (jamais de fetch réseau), comme git_badge.
type GitInfo struct {
	IsRepo   bool
	Branch   string
	Upstream bool
	Behind   int
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// readGit : un seul appel git par projet (le disque 9p de WSL rend chaque
// fork coûteux). La branche vient de .git/HEAD ; `rev-list --left-right`
// échoue s'il n'y a pas d'amont, ce qui sert aussi de détection.
func readGit(dir string) GitInfo {
	var g GitInfo
	st, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return g
	}
	g.IsRepo = true
	if st.IsDir() {
		if b, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD")); err == nil {
			g.Branch = strings.TrimPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s, err := git(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
		g.Upstream = true
		if f := strings.Fields(s); len(f) == 2 {
			g.Behind, _ = strconv.Atoi(f[1])
		}
	}
	return g
}

// GitDetail : chargé à la demande pour le seul projet sélectionné.
type GitDetail struct {
	Dirty   bool
	LastMsg string
	// Changes : entrée de premier niveau -> code git (M, A, D, ?) ; « ● » pour
	// un dossier contenant des modifications.
	Changes map[string]string
}

func readDetail(dir string) GitDetail {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := GitDetail{Changes: map[string]string{}}
	// Sortie brute : git() retire les espaces de tête, qui font partie du format.
	if raw, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "status", "--porcelain").Output(); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if len(l) < 4 {
				continue
			}
			code, path := strings.TrimSpace(l[:2]), l[3:]
			if i := strings.Index(path, " -> "); i >= 0 {
				path = path[i+4:]
			}
			path = strings.Trim(path, `"`)
			top, rest, nested := strings.Cut(path, "/")
			_ = rest
			if nested {
				d.Changes[top] = "●"
				continue
			}
			if code == "??" {
				code = "?"
			}
			d.Changes[top] = code
		}
		d.Dirty = len(d.Changes) > 0
	}
	d.LastMsg, _ = git(ctx, dir, "log", "-1", "--format=%s")
	return d
}

// FileEntry : une entrée de premier niveau du projet.
type FileEntry struct {
	Name  string
	Dir   bool
	Size  int64 // fichiers
	Count int   // dossiers : nombre d'éléments
}

// readFiles liste le premier niveau du projet (dossiers d'abord), hors .git.
func readFiles(dir string) []FileEntry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []FileEntry
	for _, e := range ents {
		if e.Name() == ".git" {
			continue
		}
		fe := FileEntry{Name: e.Name()}
		if e.IsDir() {
			fe.Dir = true
			if sub, err := os.ReadDir(filepath.Join(dir, e.Name())); err == nil {
				fe.Count = len(sub)
			}
		} else if info, err := e.Info(); err == nil {
			fe.Size = info.Size()
		}
		out = append(out, fe)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// templateTag : tag du template installé dans le projet (ex. v3.9.1).
func templateTag(proj string) string {
	for _, c := range []string{
		filepath.Join(proj, "TEMPLATE_claude", ".template-source.json"),
		filepath.Join(proj, ".claude", ".template-source.json"),
	} {
		if b, err := os.ReadFile(c); err == nil {
			var s struct {
				Tag string `json:"tag"`
			}
			if json.Unmarshal(b, &s) == nil {
				return s.Tag
			}
		}
	}
	return ""
}

// Template : état de TEMPLATE_claude vs dernier commit connu du template.
type TemplateState int

const (
	TplNone TemplateState = iota
	TplOK
	TplOutdated
	TplMissing
)

func readTemplate(proj, latest string) TemplateState {
	var src string
	for _, c := range []string{
		filepath.Join(proj, "TEMPLATE_claude", ".template-source.json"),
		filepath.Join(proj, ".claude", ".template-source.json"),
	} {
		if _, err := os.Stat(c); err == nil {
			src = c
			break
		}
	}
	if src == "" {
		var pc struct {
			TV struct {
				Commit string `json:"commit"`
			} `json:"template_version"`
		}
		if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil {
			if json.Unmarshal(b, &pc) == nil && pc.TV.Commit != "" {
				return TplMissing
			}
		}
		return TplNone
	}
	var s struct {
		Commit string `json:"commit"`
	}
	b, err := os.ReadFile(src)
	if err != nil || json.Unmarshal(b, &s) != nil || s.Commit == "" {
		return TplNone
	}
	if latest != "" && s.Commit == latest {
		return TplOK
	}
	return TplOutdated
}
