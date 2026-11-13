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
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Milestones GitHub d'un projet : badge « ⚑ » du navigateur et carte
// « Milestone » de la vue État. Même règle que le panneau milestone des
// projets (skills/milestone-panel/hooks/lib.ts, buildView), pour que le
// launcher et le projet ouvert disent la même chose :
//   - en cours    : le milestone ouvert portant une due date (GITHUB.md 3.0 :
//     `due_on` = date de démarrage, posée par MILESTONE START), la plus
//     ancienne ; toutes ses issues fermées, il est « à publier » jusqu'à sa
//     clôture, même si le suivant a déjà des issues en PLANNING ;
//   - repli sans aucune due date (convention antérieure) : premier milestone
//     ouvert (version) portant une issue avec un label de phase autre que
//     PLANNING (un plan en cours d'élaboration ne démarre pas le milestone) ;
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
	PRs     int       `json:"prs,omitempty"`     // pull requests ouvertes (badge ⇄)
	Drafts  int       `json:"drafts,omitempty"`  // dont brouillons
	// Release : dernière release publiée sur GitHub (ni brouillon ni pré-release)
	Release   string    `json:"release,omitempty"`
	ReleaseAt time.Time `json:"release_at,omitzero"`
	At        time.Time `json:"at"` // lecture GitHub
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
	MsRefresh = 5 * time.Minute
	msNoPhase = "SANS PHASE"
	msMaxRows = 6
	msBarMin  = 10 // barre d'avancement : de 10 cellules…
	msBarMax  = 40 // … à 40, selon la largeur du pane
)

var MsSem = make(chan struct{}, 3)

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

func Ratio(r MsRow) float64 {
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

	isDone := func(m rawMilestone) bool { return m.Open == 0 && m.Closed > 0 }
	var dated *rawMilestone
	for i := range open {
		if open[i].DueOn != "" {
			dated = &open[i]
			break
		}
	}
	var working, releasing *rawMilestone
	if dated != nil {
		if isDone(*dated) {
			releasing = dated
		} else {
			working = dated
		}
	} else {
		started := map[string]bool{}
		for _, i := range issues {
			if p := phaseOf(i); i.Milestone != nil && p != "" && p != "PLANNING" {
				started[i.Milestone.Title] = true
			}
		}
		for k := range open {
			if started[open[k].Title] {
				working = &open[k]
				break
			}
		}
		if working == nil {
			for k := range open {
				if isDone(open[k]) {
					releasing = &open[k]
					break
				}
			}
		}
	}
	if m := working; m != nil {
		st := MsState{Kind: MsWorking, MsRow: row(*m)}
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
	if releasing != nil {
		return MsState{Kind: MsReleasing, MsRow: row(*releasing)}
	}
	if len(open) > 0 {
		// ordre du panneau : sans version en dernier, avancement décroissant, version
		sorted := append([]rawMilestone{}, open...)
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i], sorted[j]
			if ua, ub := unversioned(a.Title), unversioned(b.Title); ua != ub {
				return ub
			}
			if ra, rb := Ratio(row(a)), Ratio(row(b)); ra != rb {
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

// OnGitHub : le dépôt a un remote GitHub (évite d'appeler gh pour rien).
func OnGitHub(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	return err == nil && strings.Contains(string(b), "github.com")
}

func ghJSON(dir, token string, v any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := platform.NoWindow(exec.CommandContext(ctx, "gh", args...))
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

type rawRelease struct {
	Tag string    `json:"tagName"`
	At  time.Time `json:"publishedAt"`
}

type RawPR struct {
	IsDraft bool `json:"isDraft"`
}

// ReadMilestones : état des milestones du projet, ses PR ouvertes et sa
// dernière release GitHub (quatre appels gh en parallèle).
func ReadMilestones(dir, token string) (MsState, error) {
	var ms []rawMilestone
	var issues []rawIssue
	var prs []RawPR
	var rels []rawRelease
	var e1, e2, e3, e4 error
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		e4 = ghJSON(dir, token, &rels, "release", "list", "--exclude-drafts", "--exclude-pre-releases",
			"--limit", "1", "--json", "tagName,publishedAt")
	}()
	go func() {
		defer wg.Done()
		e1 = ghJSON(dir, token, &ms, "api", "repos/{owner}/{repo}/milestones?state=all&per_page=100")
	}()
	go func() {
		defer wg.Done()
		e2 = ghJSON(dir, token, &issues, "issue", "list", "--state", "open", "--limit", "500", "--json", "labels,milestone")
	}()
	go func() {
		defer wg.Done()
		e3 = ghJSON(dir, token, &prs, "pr", "list", "--state", "open", "--limit", "200", "--json", "isDraft")
	}()
	wg.Wait()
	for _, e := range []error{e1, e2, e3, e4} {
		if e != nil {
			return MsState{}, e
		}
	}
	st := milestoneState(ms, issues)
	st.PRs, st.Drafts = CountPRs(prs)
	if len(rels) > 0 {
		st.Release, st.ReleaseAt = rels[0].Tag, rels[0].At
	}
	return st, nil
}

// ReleaseText : dernière release GitHub et sa date ; à défaut (pas encore lue,
// dépôt hors GitHub) le dernier tag git de la branche principale. Un tag local
// différent de la release est signalé (tags non récupérés, ou tag sans release).
func ReleaseText(ms MsState, tag string) string {
	if ms.Release == "" {
		if tag == "" {
			return OrNone(tag)
		}
		return tag + Dim.Render(" (tag git)")
	}
	s := ms.Release
	if !ms.ReleaseAt.IsZero() {
		s += Dim.Render(" GitHub, il y a " + AgeLabel(time.Since(ms.ReleaseAt)))
	}
	if tag != "" && tag != ms.Release {
		s += Yellow.Render(" · tag git " + tag)
	}
	return s
}

func CountPRs(prs []RawPR) (n, drafts int) {
	for _, p := range prs {
		n++
		if p.IsDraft {
			drafts++
		}
	}
	return
}

// WithoutAt : l'état sans l'heure de lecture (pour savoir s'il a changé).
func (s MsState) WithoutAt() MsState {
	s.At = time.Time{}
	return s
}

// ── affichage (navigateur et vue État) ─────────────────────────────────────

var MsPurple = lipgloss.NewStyle().Foreground(lipgloss.Color("#a371f7"))

// msBar : « ███████░░░  70% 7/10[ titre] », comme le panneau. La barre prend la
// largeur laissée par le pourcentage, le compte et le titre (titre vide : aucun),
// entre msBarMin et msBarMax cellules.
func msBar(r MsRow, width int, title string) string {
	t := r.Open + r.Closed
	tail := fmt.Sprintf(" %3d%%", 0) + fmt.Sprintf(" %d/%d", r.Closed, t)
	if title != "" {
		tail += " " + title
	}
	bw := width - lipgloss.Width(tail)
	bw = max(msBarMin, min(msBarMax, bw))
	n, pct := 0, 0
	if t > 0 {
		n = int(float64(r.Closed)/float64(t)*float64(bw) + 0.5)
		pct = int(float64(r.Closed)/float64(t)*100 + 0.5)
	}
	s := MsPurple.Render(strings.Repeat("█", n)) + Dim.Render(strings.Repeat("░", bw-n)) +
		Bold.Render(fmt.Sprintf(" %3d%%", pct)) + Dim.Render(fmt.Sprintf(" %d/%d", r.Closed, t))
	if title != "" {
		s += " " + title
	}
	return s
}

// msListRows : la liste des milestones ouverts, une ligne chacun, colonnes
// alignées : « v1.2.0 — Titre  n/m  ███░░░░  P% ». Quand la largeur manque, on
// retire dans l'ordre : la barre, puis la fin du titre (« … », la version reste
// entière), puis le pourcentage ; au plus étroit : « v1.2.0 n/m ».
func msListRows(rows []MsRow, width int) []string {
	const pctW = 4 // « 100% »
	titleW, verW, cw := 0, 0, 0
	for _, r := range rows {
		titleW = max(titleW, lipgloss.Width(r.Title))
		if f := strings.Fields(r.Title); len(f) > 0 {
			verW = max(verW, lipgloss.Width(f[0]))
		}
		cw = max(cw, len(fmt.Sprintf("%d/%d", r.Closed, r.Open+r.Closed)))
	}
	bar, pct := 0, true
	switch tail := 1 + cw + 1 + pctW; {
	case titleW+tail+1+msBarMin <= width: // tout tient : la barre prend le reste
		bar = min(msBarMax, width-titleW-tail-1)
	case titleW+tail <= width: // sans barre
	case verW+tail <= width: // titre tronqué
		titleW = width - tail
	default: // sans pourcentage, titre réduit jusqu'à la version
		pct = false
		titleW = max(verW, width-1-cw)
	}
	var out []string
	for _, r := range rows {
		t := r.Open + r.Closed
		n, p := 0, 0
		if t > 0 {
			n = int(float64(r.Closed)/float64(t)*float64(bar) + 0.5)
			p = int(float64(r.Closed)/float64(t)*100 + 0.5)
		}
		title := r.Title
		if lipgloss.Width(title) > titleW {
			// tronqué ; s'il ne reste que la version (et un « — »), la version seule
			ver := strings.Fields(title)[0]
			title = ansi.Truncate(title, titleW, "…")
			if titleW < lipgloss.Width(ver)+2 || strings.TrimRight(strings.TrimSuffix(title, "…"), " —–-") == ver {
				title = ver
			}
		}
		s := PadRight(title, titleW) + Dim.Render(" "+fmt.Sprintf("%*s", cw, fmt.Sprintf("%d/%d", r.Closed, t)))
		if bar > 0 {
			s += " " + MsPurple.Render(strings.Repeat("█", n)) + Dim.Render(strings.Repeat("░", bar-n))
		}
		if pct {
			s += Bold.Render(fmt.Sprintf(" %3d%%", p))
		}
		out = append(out, s)
	}
	return out
}

// MsLines : milestone en cours et son avancement (titre, barre, phases), ou
// liste des milestones ouverts quand aucun n'est en cours ; nil sans milestone.
// width : largeur disponible (les barres s'y adaptent).
func MsLines(st MsState, width int) []string {
	switch st.Kind {
	case MsWorking:
		l := []string{Yellow.Render("⚑ ") + Cyan.Render(st.Title), msBar(st.MsRow, width, "")}
		var ph []string
		for _, p := range st.Phases {
			s := fmt.Sprintf("%s %d", strings.ToLower(p.Name), p.N)
			if p.Name == msNoPhase {
				s = Dim.Render(s)
			}
			ph = append(ph, s)
		}
		if len(ph) > 0 {
			l = append(l, strings.Join(ph, Dim.Render(" · ")))
		}
		if st.Blocked > 0 {
			l = append(l, Red.Render(fmt.Sprintf("⛔ %d bloquée(s)", st.Blocked)))
		}
		return l
	case MsReleasing:
		return []string{Orange.Render("⚑ ") + Cyan.Render(st.Title) + Orange.Render(" — à publier"), msBar(st.MsRow, width, "")}
	case MsIdle:
		l := []string{Dim.Render("⚑ aucun milestone en cours")}
		rows := st.Pending
		if len(rows) > msMaxRows {
			rows = rows[:msMaxRows]
		}
		l = append(l, msListRows(rows, width)...)
		if n := len(st.Pending) - len(rows); n > 0 {
			l = append(l, Dim.Render(fmt.Sprintf("  … %d autre(s)", n)))
		}
		return l
	case MsDone:
		return []string{Green.Render("⚑ "+st.Title+" publié") + Dim.Render(" (il y a "+AgeLabel(time.Since(st.DoneAt))+")")}
	}
	return nil
}

// ── cache disque : partagé avec la vue État, badges dès le lancement ───────

var msCacheMu sync.Mutex

func MsCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher-ng2", "milestones-v2.json")
}

func ReadMsCache() map[string]MsState {
	out := map[string]MsState{}
	if b, err := os.ReadFile(MsCachePath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func WriteMsCache(m map[string]MsState) {
	msCacheMu.Lock()
	defer msCacheMu.Unlock()
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	p := MsCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
	_ = os.Remove(filepath.Join(filepath.Dir(p), "milestones.json")) // format v4.6.0
}
