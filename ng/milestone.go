package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Milestones GitHub d'un projet (badge « ⚑ » du navigateur), lus par
// « gh api …/milestones?state=all » — un appel par projet, comme le panneau
// milestone des projets (gh gère l'authentification). Trois états :
//   - en cours    : un milestone ouvert a encore des issues ouvertes ;
//   - à publier   : un milestone ouvert a toutes ses issues fermées (le panneau
//     le dit « en publication » jusqu'à sa clôture par /milestone close) ;
//   - publié      : plus rien d'ouvert, le dernier milestone est clos.
// Sans milestone (ou hors GitHub), pas de badge. Les milestones sans version
// (segment « x » dans le numéro, ex. « v2.x », backlog) ne comptent pas comme en cours.

type MsKind int

const (
	MsNone MsKind = iota
	MsWorking
	MsReleasing
	MsDone
)

type MsState struct {
	Kind   MsKind    `json:"kind"`
	Title  string    `json:"title"`
	Open   int       `json:"open"`
	Closed int       `json:"closed"`
	At     time.Time `json:"at"`      // lecture GitHub
	DoneAt time.Time `json:"done_at"` // clôture (publié)
}

type rawMilestone struct {
	Title    string    `json:"title"`
	State    string    `json:"state"`
	Open     int       `json:"open_issues"`
	Closed   int       `json:"closed_issues"`
	DueOn    string    `json:"due_on"`
	ClosedAt time.Time `json:"closed_at"`
}

const msRefresh = 5 * time.Minute

var (
	msSem   = make(chan struct{}, 3)
	verNums = regexp.MustCompile(`\d+`)
)

// msVersion : numéro du milestone = premier mot du titre, sans « v ».
func msVersion(title string) string {
	f := strings.Fields(title)
	if len(f) == 0 {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(f[0], "v"), "V")
}

// unversioned : version pas encore fixée (« vX.0.x », « vx.x.x ») — comme le panneau.
func unversioned(title string) bool {
	for _, seg := range strings.Split(msVersion(title), ".") {
		if strings.EqualFold(seg, "x") {
			return true
		}
	}
	return false
}

// onGitHub : le dépôt a un remote GitHub (évite d'appeler gh pour rien).
func onGitHub(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	return err == nil && strings.Contains(string(b), "github.com")
}

func fetchMilestones(dir, token string) ([]rawMilestone, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "api", "repos/{owner}/{repo}/milestones?state=all&per_page=100")
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if token != "" && os.Getenv("GH_TOKEN") == "" {
		cmd.Env = append(cmd.Env, "GH_TOKEN="+token)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var ms []rawMilestone
	return ms, json.Unmarshal(out, &ms)
}

// lessMilestone : échéance d'abord, puis version (v1.10 après v1.9).
func lessMilestone(a, b rawMilestone) bool {
	if a.DueOn != b.DueOn {
		if a.DueOn == "" || b.DueOn == "" {
			return a.DueOn != ""
		}
		return a.DueOn < b.DueOn
	}
	na, nb := verNums.FindAllString(msVersion(a.Title), 4), verNums.FindAllString(msVersion(b.Title), 4)
	for i := 0; i < len(na) && i < len(nb); i++ {
		x, _ := strconv.Atoi(na[i])
		y, _ := strconv.Atoi(nb[i])
		if x != y {
			return x < y
		}
	}
	if len(na) != len(nb) {
		return len(na) < len(nb)
	}
	return a.Title < b.Title
}

// milestoneState : état à afficher ; « à publier » passe devant « en cours ».
func milestoneState(ms []rawMilestone) MsState {
	var open []rawMilestone
	var last *rawMilestone
	for i, m := range ms {
		if m.State == "open" {
			open = append(open, m)
		} else if last == nil || m.ClosedAt.After(last.ClosedAt) {
			last = &ms[i]
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return lessMilestone(open[i], open[j]) })
	for _, m := range open {
		if m.Open == 0 && m.Closed > 0 {
			return MsState{Kind: MsReleasing, Title: m.Title, Closed: m.Closed}
		}
	}
	for _, m := range open {
		if m.Open > 0 && !unversioned(m.Title) {
			return MsState{Kind: MsWorking, Title: m.Title, Open: m.Open, Closed: m.Closed}
		}
	}
	if last != nil {
		return MsState{Kind: MsDone, Title: last.Title, Closed: last.Closed, DoneAt: last.ClosedAt}
	}
	return MsState{}
}

// ── cache disque : badges affichés dès le lancement ────────────────────────

var msCacheMu sync.Mutex

func msCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher", "milestones.json")
}

func readMsCache() map[string]MsState {
	out := map[string]MsState{}
	if b, err := os.ReadFile(msCachePath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func writeMsCache(m map[string]MsState) {
	msCacheMu.Lock()
	defer msCacheMu.Unlock()
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	p := msCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}
