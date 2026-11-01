package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Milestones GitHub d'un projet : badge « ⚑ » du navigateur et carte
// « Milestone » de la vue État. Même règle que le panneau milestone des
// projets (skills/milestone-panel/hooks/lib.ts, buildView), pour que le
// launcher et le projet ouvert disent la même chose :
//   - en cours    : premier milestone ouvert (échéance, puis version) portant au
//     moins une issue ouverte avec un label de phase (PLANNING… DONE) ;
//   - à publier   : à défaut, premier milestone ouvert dont toutes les issues
//     sont fermées (le panneau : « — publication », jusqu'à sa clôture) ;
//   - en attente  : des milestones ouverts, aucun en cours (le panneau :
//     « Aucun milestone en cours » et la liste des milestones ouverts) ;
//   - publié      : plus aucun milestone ouvert, le dernier est clos.
// Sans milestone (ou hors GitHub), pas de badge. Deux appels gh par projet,
// ceux du panneau : milestones (ici state=all) et issues ouvertes.

type MsKind int

const (
	MsNone MsKind = iota
	MsWorking
	MsReleasing
	MsIdle
	MsDone
)

// MsRow : un milestone et sa progression.
type MsRow struct {
	Title  string `json:"title"`
	Open   int    `json:"open"`
	Closed int    `json:"closed"`
}

// MsPhase : issues ouvertes du milestone en cours dans une phase.
type MsPhase struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

type MsState struct {
	Kind    MsKind    `json:"kind"`
	MsRow             // milestone en cours / à publier / dernier publié
	Phases  []MsPhase `json:"phases,omitempty"`  // en cours : issues ouvertes par phase
	Blocked int       `json:"blocked,omitempty"` // en cours : issues BLOQUE
	Pending []MsRow   `json:"pending,omitempty"` // en attente : milestones ouverts
	DoneAt  time.Time `json:"done_at"`           // publié : date de clôture
	At      time.Time `json:"at"`                // lecture GitHub
}

type rawMilestone struct {
	Title    string    `json:"title"`
	State    string    `json:"state"`
	Open     int       `json:"open_issues"`
	Closed   int       `json:"closed_issues"`
	DueOn    string    `json:"due_on"`
	ClosedAt time.Time `json:"closed_at"`
}

type rawIssue struct {
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

var msPhases = []string{"PLANNING", "PRET", "EN COURS", "EN REVIEW", "EN QA", "DONE"}

const (
	msRefresh  = 5 * time.Minute
	msNoPhase  = "SANS PHASE"
	msMaxRows  = 6
	msBarWidth = 10
)

var msSem = make(chan struct{}, 3)

// panelSemver : version comme le panneau (« v » ôté, découpe sur « . »,
// parseInt de chaque morceau, 0 s'il ne commence pas par un nombre).
func panelSemver(title string) []int {
	t := title
	if len(t) > 0 && (t[0] == 'v' || t[0] == 'V') {
		t = t[1:]
	}
	var out []int
	for _, part := range strings.Split(t, ".") {
		part = strings.TrimLeft(part, " \t")
		n, i := 0, 0
		for ; i < len(part) && part[i] >= '0' && part[i] <= '9'; i++ {
			n = n*10 + int(part[i]-'0')
		}
		out = append(out, n)
	}
	return out
}

func compareTitles(a, b string) int {
	x, y := panelSemver(a), panelSemver(b)
	for i := 0; i < max(len(x), len(y)); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p - q
		}
	}
	return 0
}

// lessMilestone : échéance d'abord, puis version (compareMilestones du panneau).
func lessMilestone(a, b rawMilestone) bool {
	if a.DueOn != b.DueOn {
		if a.DueOn == "" || b.DueOn == "" {
			return a.DueOn != ""
		}
		return a.DueOn < b.DueOn
	}
	return compareTitles(a.Title, b.Title) < 0
}

// unversioned : version pas encore fixée (« vX.0.x ») : un segment du premier
// mot du titre est un « x » ; placé en dernier dans la liste des ouverts.
func unversioned(title string) bool {
	f := strings.Fields(title)
	if len(f) == 0 {
		return false
	}
	for _, seg := range strings.Split(strings.TrimLeft(f[0], "vV"), ".") {
		if strings.EqualFold(seg, "x") {
			return true
		}
	}
	return false
}

func phaseOf(i rawIssue) string {
	for _, l := range i.Labels {
		for _, p := range msPhases {
			if l.Name == p {
				return p
			}
		}
	}
	return ""
}

func ratio(r MsRow) float64 {
	if t := r.Open + r.Closed; t > 0 {
		return float64(r.Closed) / float64(t)
	}
	return 0
}

// milestoneState : buildView du panneau, plus l'état « publié ».
func milestoneState(ms []rawMilestone, issues []rawIssue) MsState {
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
	row := func(m rawMilestone) MsRow { return MsRow{m.Title, m.Open, m.Closed} }

	started := map[string]bool{}
	for _, i := range issues {
		if i.Milestone != nil && phaseOf(i) != "" {
			started[i.Milestone.Title] = true
		}
	}
	for _, m := range open {
		if !started[m.Title] {
			continue
		}
		st := MsState{Kind: MsWorking, MsRow: row(m)}
		count := map[string]int{}
		for _, i := range issues {
			if i.Milestone == nil || i.Milestone.Title != m.Title {
				continue
			}
			p := phaseOf(i)
			if p == "" {
				p = msNoPhase
			}
			count[p]++
			for _, l := range i.Labels {
				if l.Name == "BLOQUE" {
					st.Blocked++
					break
				}
			}
		}
		for _, p := range append(append([]string{}, msPhases...), msNoPhase) {
			if count[p] > 0 {
				st.Phases = append(st.Phases, MsPhase{p, count[p]})
			}
		}
		return st
	}
	for _, m := range open {
		if m.Open == 0 && m.Closed > 0 {
			return MsState{Kind: MsReleasing, MsRow: row(m)}
		}
	}
	if len(open) > 0 {
		// ordre du panneau : sans version en dernier, avancement décroissant, version
		sorted := append([]rawMilestone{}, open...)
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i], sorted[j]
			if ua, ub := unversioned(a.Title), unversioned(b.Title); ua != ub {
				return ub
			}
			if ra, rb := ratio(row(a)), ratio(row(b)); ra != rb {
				return ra > rb
			}
			if c := compareTitles(a.Title, b.Title); c != 0 {
				return c < 0
			}
			return a.Title < b.Title
		})
		st := MsState{Kind: MsIdle}
		for _, m := range sorted {
			st.Pending = append(st.Pending, row(m))
		}
		return st
	}
	if last != nil {
		return MsState{Kind: MsDone, MsRow: row(*last), DoneAt: last.ClosedAt}
	}
	return MsState{}
}

// ── lecture GitHub ─────────────────────────────────────────────────────────

// onGitHub : le dépôt a un remote GitHub (évite d'appeler gh pour rien).
func onGitHub(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	return err == nil && strings.Contains(string(b), "github.com")
}

func ghJSON(dir, token string, v any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if token != "" && os.Getenv("GH_TOKEN") == "" {
		cmd.Env = append(cmd.Env, "GH_TOKEN="+token)
	}
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

// readMilestones : état des milestones du projet (deux appels gh en parallèle).
func readMilestones(dir, token string) (MsState, error) {
	var ms []rawMilestone
	var issues []rawIssue
	var e1, e2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e1 = ghJSON(dir, token, &ms, "api", "repos/{owner}/{repo}/milestones?state=all&per_page=100")
	}()
	go func() {
		defer wg.Done()
		e2 = ghJSON(dir, token, &issues, "issue", "list", "--state", "open", "--limit", "500", "--json", "labels,milestone")
	}()
	wg.Wait()
	if e1 != nil {
		return MsState{}, e1
	}
	if e2 != nil {
		return MsState{}, e2
	}
	return milestoneState(ms, issues), nil
}

// withoutAt : l'état sans l'heure de lecture (pour savoir s'il a changé).
func (s MsState) withoutAt() MsState {
	s.At = time.Time{}
	return s
}

// ── affichage (navigateur et vue État) ─────────────────────────────────────

var msPurple = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))

// msBar : « ███████░░░  70% 7/10 », comme le panneau.
func msBar(r MsRow) string {
	t := r.Open + r.Closed
	n, pct := 0, 0
	if t > 0 {
		n = int(float64(r.Closed)/float64(t)*msBarWidth + 0.5)
		pct = int(float64(r.Closed)/float64(t)*100 + 0.5)
	}
	return msPurple.Render(strings.Repeat("█", n)) + dim.Render(strings.Repeat("░", msBarWidth-n)) +
		bold.Render(fmt.Sprintf(" %3d%%", pct)) + dim.Render(fmt.Sprintf(" %d/%d", r.Closed, t))
}

// msLines : milestone en cours et son avancement (titre, barre, phases), ou
// liste des milestones ouverts quand aucun n'est en cours ; nil sans milestone.
func msLines(st MsState) []string {
	switch st.Kind {
	case MsWorking:
		l := []string{yellow.Render("⚑ ") + cyan.Render(st.Title), msBar(st.MsRow)}
		var ph []string
		for _, p := range st.Phases {
			s := fmt.Sprintf("%s %d", strings.ToLower(p.Name), p.N)
			if p.Name == msNoPhase {
				s = dim.Render(s)
			}
			ph = append(ph, s)
		}
		if len(ph) > 0 {
			l = append(l, strings.Join(ph, dim.Render(" · ")))
		}
		if st.Blocked > 0 {
			l = append(l, red.Render(fmt.Sprintf("⛔ %d bloquée(s)", st.Blocked)))
		}
		return l
	case MsReleasing:
		return []string{orange.Render("⚑ ") + cyan.Render(st.Title) + orange.Render(" — à publier"), msBar(st.MsRow)}
	case MsIdle:
		l := []string{dim.Render("⚑ aucun milestone en cours")}
		for i, r := range st.Pending {
			if i == msMaxRows {
				l = append(l, dim.Render(fmt.Sprintf("  … %d autre(s)", len(st.Pending)-i)))
				break
			}
			l = append(l, msBar(r)+" "+r.Title)
		}
		return l
	case MsDone:
		return []string{green.Render("⚑ "+st.Title+" publié") + dim.Render(" (il y a "+ageLabel(time.Since(st.DoneAt))+")")}
	}
	return nil
}

// ── cache disque : partagé avec la vue État, badges dès le lancement ───────

var msCacheMu sync.Mutex

func msCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher", "milestones-v2.json")
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
	_ = os.Remove(filepath.Join(filepath.Dir(p), "milestones.json")) // format v4.6.0
}
