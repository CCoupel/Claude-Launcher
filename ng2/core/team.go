package core

import (
	"cmp"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Composition de la team d'un projet, pour la vue « ◈ État » : les teammates du
// registre du template (project-config.json → agents.registry[], instances
// agents.generic[]), la version de leur copie, la présence d'un compagnon,
// leur modèle et effort, et la version de claude des teammates lancés.

type Teammate struct {
	Name, Tag     string
	Companion     bool
	Model, Effort string // compagnon > template ; modèle réel si lancé
	Claude        string // version de claude si lancé
	Count         int    // instances lancées
}

// frontKeys : model/effort du frontmatter d'un fichier d'agent.
func frontKeys(path string) (model, effort string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		if l == "---" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch {
		case !ok:
		case k == "model":
			model = v
		case k == "effort":
			effort = v
		}
	}
	return model, effort
}

var ClaudeVerRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// ReadTeammates : teammates déclarés par le template dans le projet. Sans
// registre (projet antérieur), les copies *.template.md, au tag du projet.
func ReadTeammates(proj string) []Teammate {
	agents := filepath.Join(proj, ".claude", "agents")
	has := func(f string) bool {
		st, err := os.Stat(filepath.Join(agents, f))
		return err == nil && st.Size() > 0
	}
	var cfg struct {
		Agents struct {
			Registry []struct{ Name, Tag string } `json:"registry"`
			Generic  []struct{ Name string }      `json:"generic"`
		} `json:"agents"`
	}
	if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	// mk : le compagnon (ou la spécification d'instance) prime sur le template
	mk := func(name, tag, tpl, comp string) Teammate {
		t := Teammate{Name: name, Tag: tag, Companion: has(comp)}
		t.Model, t.Effort = frontKeys(filepath.Join(agents, tpl+".template.md"))
		if m, e := frontKeys(filepath.Join(agents, comp)); m != "" || e != "" {
			t.Model, t.Effort = cmp.Or(m, t.Model), cmp.Or(e, t.Effort)
		}
		return t
	}
	var out []Teammate
	for _, r := range cfg.Agents.Registry {
		if r.Name == "generic" && len(cfg.Agents.Generic) > 0 {
			for _, g := range cfg.Agents.Generic {
				out = append(out, mk(g.Name, r.Tag, "generic", "generic."+g.Name+".md"))
			}
			continue
		}
		out = append(out, mk(r.Name, r.Tag, r.Name, r.Name+".md"))
	}
	if len(out) > 0 {
		return out
	}
	tag := TemplateTag(proj)
	files, _ := filepath.Glob(filepath.Join(agents, "*.template.md"))
	for _, f := range files {
		n := strings.TrimSuffix(filepath.Base(f), ".template.md")
		out = append(out, mk(n, tag, n, n+".md"))
	}
	return out
}

// teamMembers : pane tmux → membre de la team, pour la team du projet dont un
// pane est vivant (la plus récente si plusieurs).
func teamMembers(proj string, live map[string]bool) map[string]TeamMember {
	dirs, _ := filepath.Glob(filepath.Join(ClaudeDir(), "teams", "*", "config.json"))
	var best map[string]TeamMember
	var bestT int64
	for _, f := range dirs {
		cfg, err := ReadTeam(f)
		if err != nil || len(cfg.Members) == 0 || path.Clean(cfg.Members[0].Cwd) != path.Clean(ClaudePath(proj)) {
			continue
		}
		m := map[string]TeamMember{}
		for _, mb := range cfg.Members {
			if live[mb.PaneID] {
				m[mb.PaneID] = mb
			}
		}
		st, _ := os.Stat(f)
		if len(m) > 0 && st != nil && st.ModTime().UnixNano() > bestT {
			best, bestT = m, st.ModTime().UnixNano()
		}
	}
	return best
}

// claudeVersion : version de claude tournant dans un pane. claude nomme son
// processus d'après sa version (teammates) ; sinon (leader, lancé par un
// shell), le binaire du processus claude du pane ou de ses enfants
// (~/.local/share/claude/versions/<v>, Linux seulement).
func claudeVersion(cmd, pid string) string {
	if v := ClaudeVerRe.FindString(cmd); v != "" {
		return v
	}
	if pid == "" { // pane d'un conteneur : PID d'un autre espace, rien à lire ici
		return ""
	}
	pids := []string{pid}
	if b, err := os.ReadFile("/proc/" + pid + "/task/" + pid + "/children"); err == nil {
		pids = append(pids, strings.Fields(string(b))...)
	}
	for _, p := range pids {
		if c, _ := os.ReadFile("/proc/" + p + "/comm"); strings.TrimSpace(string(c)) != "claude" {
			continue
		}
		if exe, err := os.Readlink("/proc/" + p + "/exe"); err == nil {
			return ClaudeVerRe.FindString(filepath.Base(exe))
		}
	}
	return ""
}

// LiveTeam : complète les teammates avec la version de claude (et le modèle
// réel) des panes lancés de la fenêtre du projet. Le leader (pane hors team)
// porte le teamleader.
func LiveTeam(tm []Teammate, proj string, panes []PaneInfo) []Teammate {
	live := map[string]bool{}
	for _, p := range panes {
		live[p.ID] = true
	}
	members := teamMembers(proj, live)
	type run struct{ ver, model string }
	byType := map[string][]run{}
	for _, p := range panes {
		v := claudeVersion(p.Cmd, p.PID)
		if v == "" {
			continue
		}
		mb, ok := members[p.ID]
		if !ok {
			mb.AgentType = "teamleader"
		}
		byType[mb.AgentType] = append(byType[mb.AgentType], run{v, mb.Model})
	}
	for i := range tm {
		if rs := byType[tm[i].Name]; len(rs) > 0 {
			tm[i].Claude, tm[i].Count = rs[0].ver, len(rs)
			tm[i].Model = cmp.Or(rs[0].model, tm[i].Model)
			delete(byType, tm[i].Name)
		}
	}
	// lancés hors registre : agents propres au projet, claude seul sans template
	extra := make([]string, 0, len(byType))
	for t := range byType {
		extra = append(extra, t)
	}
	sort.Strings(extra)
	for _, t := range extra {
		n := t
		if t == "teamleader" {
			n = "leader"
		}
		rs := byType[t]
		tm = append(tm, Teammate{Name: n, Model: rs[0].model, Claude: rs[0].ver, Count: len(rs)})
	}
	return tm
}

// TeamLine : une ligne de la carte TEAM, par instance de teammate (#136) : les
// instances lancées (fenêtre du projet et réserve), puis les teammates du
// registre non démarrés.
type TeamLine struct {
	Name, Tag     string
	Companion     bool
	Model, Effort string
	Claude        string // version de claude si lancé
	Pane          string // %id, vide si non démarré
	Started       bool
	Active        bool
	Hidden        bool // mis de côté dans la réserve
	Pinned        bool // visible fixé à la main
	IdleLeft      int  // secondes avant masquage automatique (0 : sans objet)
}

// TeamLines : lignes de la carte TEAM. tm : registre (ReadTeammates), panes :
// recensement unique (ProjectPanes : fenêtre + réserve), choices : @cl_panevis,
// hideIdle : TEAM_HIDE_IDLE, now : heure Unix. Le leader (pane hors team) vient
// en premier sous le nom « leader » ; les instances suivent par pane.
func TeamLines(tm []Teammate, proj string, panes []ProjPane, choices map[string]string, hideIdle int, now int64) []TeamLine {
	live := map[string]bool{}
	for _, p := range panes {
		live[p.ID] = true
	}
	members := teamMembers(proj, live)
	reg := map[string]Teammate{}
	for _, t := range tm {
		reg[t.Name] = t
	}
	tc := TeamConfig{}
	for _, mb := range members {
		tc.Members = append(tc.Members, mb)
	}
	vp := visPanes(panes, &tc)
	prot := map[string]bool{}
	for _, v := range vp {
		prot[v.ID] = v.Protected
	}
	sorted := append([]ProjPane(nil), panes...)
	sort.SliceStable(sorted, func(i, j int) bool { return paneNum(sorted[i].ID) < paneNum(sorted[j].ID) })
	leaderID := ""
	if len(sorted) > 0 {
		leaderID = sorted[0].ID
	}
	var lines, leaders []TeamLine
	started := map[string]bool{}
	for _, p := range sorted {
		mb, ok := members[p.ID]
		name, typ := mb.Name, mb.AgentType
		if !ok && p.ID != leaderID {
			continue // pane ouvert à la main : pas un teammate
		}
		if !ok || name == "team-lead" {
			name, typ = "leader", "teamleader"
		}
		t := reg[typ]
		if t.Name == "" {
			t = reg[name]
		}
		started[typ], started[name] = true, true
		l := TeamLine{Name: name, Tag: t.Tag, Companion: t.Companion, Model: cmp.Or(mb.Model, t.Model), Effort: t.Effort,
			Claude: claudeVersion(p.Cmd, p.PID), Pane: p.ID, Started: true, Hidden: p.Hidden,
			Pinned: choices[p.ID] == visPinned}
		l.Active = ok && mb.IsActive
		if !prot[p.ID] && !l.Active && choices[p.ID] == "" && hideIdle > 0 && !p.Hidden {
			l.IdleLeft = max(hideIdle-int(now-cmpIdle(p.IdleAt, now)), 0)
		}
		if prot[p.ID] {
			leaders = append(leaders, l)
		} else {
			lines = append(lines, l)
		}
	}
	lines = append(leaders, lines...)
	for _, t := range tm {
		if !started[t.Name] {
			lines = append(lines, TeamLine{Name: t.Name, Tag: t.Tag, Companion: t.Companion, Model: t.Model, Effort: t.Effort})
		}
	}
	return lines
}

func cmpIdle(at, now int64) int64 {
	if at == 0 {
		return now
	}
	return at
}

func paneNum(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "%"))
	if err != nil {
		return 1 << 30
	}
	return n
}
