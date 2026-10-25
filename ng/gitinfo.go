package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// GitInfo : état git purement local (jamais de fetch réseau), comme git_badge.
type GitInfo struct {
	IsRepo   bool
	Branch   string
	Main     string // branche principale (origin/HEAD, sinon « main »)
	Dirty    bool
	Upstream bool // amont configuré
	Gone     bool // amont configuré mais disparu (branche supprimée après merge + fetch --prune)
	Ahead    int
	Behind   int
	Merged   bool // hors branche principale : HEAD déjà contenu dans origin/<Main>
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// readGit : un seul `git status --branch` par projet (le disque 9p de WSL rend
// chaque fork coûteux) donne branche, avance/retard et modifications ; un
// second appel, seulement hors branche principale, teste le merge.
func readGit(dir string) GitInfo {
	var g GitInfo
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return g
	}
	g.IsRepo = true
	g.Main = mainBranch(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "status", "--porcelain=v2", "--branch").Output()
	if err != nil {
		return g
	}
	ab := false
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "# branch.head "):
			g.Branch = strings.TrimPrefix(l, "# branch.head ")
		case strings.HasPrefix(l, "# branch.upstream "):
			g.Upstream = true
		case strings.HasPrefix(l, "# branch.ab "):
			ab = true
			if f := strings.Fields(l); len(f) == 4 {
				g.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
				g.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
			}
		case l != "" && !strings.HasPrefix(l, "#"):
			g.Dirty = true
		}
	}
	g.Gone = g.Upstream && !ab
	if g.Upstream && g.Branch != g.Main {
		g.Merged = exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir,
			"merge-base", "--is-ancestor", "HEAD", "origin/"+g.Main).Run() == nil
	}
	return g
}

// mainBranch : branche principale d'après origin/HEAD, « main » à défaut.
func mainBranch(dir string) string {
	if b, err := os.ReadFile(filepath.Join(dir, ".git", "refs", "remotes", "origin", "HEAD")); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(string(b)), "ref: refs/remotes/origin/")
	}
	return "main"
}

// Local : état de la copie de travail (symbole, libellé, style).
func (g GitInfo) Local() (string, string, lipgloss.Style) {
	onMain := g.Branch == g.Main
	switch {
	case onMain && g.Dirty:
		return "!", "modifié directement sur " + g.Main, red
	case onMain:
		return "●", "au repos sur " + g.Main, green.Bold(true)
	case g.Dirty:
		return "✎", "en cours, non commité", yellow.Bold(true)
	default:
		return "◆", "terminé, tout commité", cyan
	}
}

// Remote : état par rapport à origin, le plus urgent d'abord.
func (g GitInfo) Remote() (string, string, lipgloss.Style) {
	switch {
	case !g.Upstream:
		return "○", "jamais poussé", blue
	case g.Behind > 0:
		return fmt.Sprintf("↓%d", g.Behind), fmt.Sprintf("en retard de %d commit(s)", g.Behind), red
	case g.Ahead > 0:
		return fmt.Sprintf("↑%d", g.Ahead), fmt.Sprintf("%d commit(s) non poussé(s)", g.Ahead), yellow.Bold(true)
	case g.Branch == g.Main:
		return "✓", "à jour", green.Bold(true)
	case g.Merged:
		return "✓", "publié, mergé dans " + g.Main, green.Bold(true)
	case g.Gone:
		return "⇡", "amont supprimé, non mergé", cyan
	default:
		return "⇡", "poussé, non mergé", cyan
	}
}

// GitDetail : chargé à la demande pour le seul projet sélectionné.
type GitDetail struct {
	LastMsg string
	// VerLocal : version de travail (describe de HEAD) ; VerRelease : dernier
	// tag atteignable depuis origin/<principale>.
	VerLocal   string
	VerRelease string
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
	}
	d.LastMsg, _ = git(ctx, dir, "log", "-1", "--format=%s")
	d.VerLocal, _ = git(ctx, dir, "describe", "--tags", "--dirty")
	d.VerRelease, _ = git(ctx, dir, "describe", "--tags", "--abbrev=0", "origin/"+mainBranch(dir))
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
