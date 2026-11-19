package core

import (
	"claude-launcher-ng2/platform"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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

// gitStopWait : attente laissée à git, après le signal d'arrêt, pour libérer ses
// verrous avant d'être tué.
const gitStopWait = 2 * time.Second

// gitCmd : lecture git sans écriture dans .git. GIT_OPTIONAL_LOCKS=0 en plus de
// --no-optional-locks ; au délai, git reçoit un signal d'arrêt (il retire son
// index.lock) et n'est tué qu'après gitStopWait — jamais de SIGKILL immédiat (#154).
func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	cmd.Env = WithSafeDirectory(append(os.Environ(), "GIT_OPTIONAL_LOCKS=0"), dir)
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil { // Windows : pas de signal, arrêt direct
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = gitStopWait
	return platform.NoWindow(cmd)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitCmd(ctx, dir, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// ReadGit : un seul `git status --branch` par projet (le disque 9p de WSL rend
// chaque fork coûteux) donne branche, avance/retard et modifications ; un
// second appel, seulement hors branche principale, teste le merge.
func ReadGit(dir string) GitInfo {
	var g GitInfo
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return g
	}
	g.IsRepo = true
	g.Main = mainBranch(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := gitCmd(ctx, dir, "status", "--porcelain=v2", "--branch").Output()
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
	// Poussée sans -u : pas de suivi configuré, mais origin/<branche> existe ;
	// on compare avec elle plutôt que d'afficher « jamais poussé ».
	if !g.Upstream && g.Branch != "" && g.Branch != "(detached)" {
		if c, err := git(ctx, dir, "rev-list", "--left-right", "--count", "HEAD...refs/remotes/origin/"+g.Branch); err == nil {
			if f := strings.Fields(c); len(f) == 2 {
				g.Upstream = true
				g.Ahead, _ = strconv.Atoi(f[0])
				g.Behind, _ = strconv.Atoi(f[1])
			}
		}
	}
	if g.Upstream && g.Branch != g.Main {
		g.Merged = gitCmd(ctx, dir,
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
	i, t, tone := g.LocalTone()
	return i, t, tone.Style()
}

// LocalTone : état de la copie de travail (symbole, libellé, couleur sémantique).
func (g GitInfo) LocalTone() (string, string, Tone) {
	onMain := g.Branch == g.Main
	switch {
	case onMain && g.Dirty:
		return "!", "modifié directement sur " + g.Main, ToneRed
	case onMain:
		return "●", "au repos sur " + g.Main, ToneGreen.B()
	case g.Dirty:
		return "✎", "en cours, non commité", ToneYellow.B()
	default:
		return "◆", "terminé, tout commité", ToneCyan
	}
}

// Remote : état par rapport à origin, le plus urgent d'abord.
func (g GitInfo) Remote() (string, string, lipgloss.Style) {
	i, t, tone := g.RemoteTone()
	return i, t, tone.Style()
}

// RemoteTone : état par rapport à origin (symbole, libellé, couleur sémantique).
func (g GitInfo) RemoteTone() (string, string, Tone) {
	switch {
	case !g.Upstream:
		return "○", "jamais poussé", ToneBlue
	case g.Behind > 0:
		return fmt.Sprintf("↓%d", g.Behind), fmt.Sprintf("en retard de %d commit(s)", g.Behind), ToneRed
	case g.Ahead > 0:
		return fmt.Sprintf("↑%d", g.Ahead), fmt.Sprintf("%d commit(s) non poussé(s)", g.Ahead), ToneYellow.B()
	case g.Branch == g.Main:
		return "✓", "à jour", ToneGreen.B()
	case g.Merged:
		return "✓", "publié, mergé dans " + g.Main, ToneGreen.B()
	case g.Gone:
		return "⇡", "amont supprimé, non mergé", ToneCyan
	default:
		return "⇡", "poussé, non mergé", ToneCyan
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

func ReadDetail(dir string) GitDetail {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := GitDetail{Changes: map[string]string{}}
	dirty := false // fichier suivi modifié : ce que `describe --dirty` aurait signalé (sans toucher à l'index)
	// Sortie brute : git() retire les espaces de tête, qui font partie du format.
	if raw, err := gitCmd(ctx, dir, "status", "--porcelain").Output(); err == nil {
		for _, l := range strings.Split(string(raw), "\n") {
			if len(l) < 4 {
				continue
			}
			code, path := strings.TrimSpace(l[:2]), l[3:]
			if code != "??" {
				dirty = true
			}
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
	// jamais `describe --dirty` : il rafraîchit l'index et prend index.lock
	if d.VerLocal, _ = git(ctx, dir, "describe", "--tags"); dirty && d.VerLocal != "" {
		d.VerLocal += "-dirty"
	}
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

// ReadFiles liste le premier niveau du projet (dossiers d'abord), hors .git.
func ReadFiles(dir string) []FileEntry {
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

// TemplateTag : tag du template installé dans le projet (ex. v3.9.1).
func TemplateTag(proj string) string {
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

// ProjectTemplateTag : version du template du projet (#199) — .template-source.json, à défaut template_version.tag
// de .claude/project-config.json ; "" si inconnue.
func ProjectTemplateTag(proj string) string {
	if t := TemplateTag(proj); t != "" {
		return t
	}
	var pc struct {
		TV struct {
			Tag string `json:"tag"`
		} `json:"template_version"`
	}
	if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil && json.Unmarshal(b, &pc) == nil {
		return pc.TV.Tag
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

// TemplateRef : version d'un template, « v3.28.0 (8f583bf) », ou ce qui en est connu.
func TemplateRef(tag, commit string) string {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	switch {
	case tag != "" && commit != "":
		return tag + " (" + commit + ")"
	case tag != "":
		return tag
	case commit != "":
		return commit
	}
	return "version inconnue"
}

// ProjectTemplateRef : version du template du projet (.template-source.json,
// sinon template_version de project-config.json).
func ProjectTemplateRef(proj string) string {
	var s struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	for _, c := range []string{
		filepath.Join(proj, "TEMPLATE_claude", ".template-source.json"),
		filepath.Join(proj, ".claude", ".template-source.json"),
	} {
		if b, err := os.ReadFile(c); err == nil && json.Unmarshal(b, &s) == nil && (s.Tag != "" || s.Commit != "") {
			return TemplateRef(s.Tag, s.Commit)
		}
	}
	var pc struct {
		TV struct {
			Tag    string `json:"tag"`
			Commit string `json:"commit"`
		} `json:"template_version"`
	}
	if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil && json.Unmarshal(b, &pc) == nil {
		return TemplateRef(pc.TV.Tag, pc.TV.Commit)
	}
	return TemplateRef("", "")
}

func ReadTemplate(proj, latest string) TemplateState {
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
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	b, err := os.ReadFile(src)
	if err != nil || json.Unmarshal(b, &s) != nil || s.Commit == "" {
		return TplNone
	}
	// comparaison ordonnée (#199) : en retard seulement si la version adoptée est strictement plus récente ;
	// un projet plus récent que la version adoptée est à jour. Tag manquant d'un côté : égalité des commits.
	if adopted := ReadTrim(TplVersionPath()); s.Tag != "" && adopted != "" {
		if VersionNewer(s.Tag, adopted) {
			return TplOutdated
		}
		return TplOK
	}
	if latest != "" && s.Commit == latest {
		return TplOK
	}
	return TplOutdated
}
