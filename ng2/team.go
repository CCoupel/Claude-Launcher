package main

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Composition de la team d'un projet, pour la vue « ◈ État » : les teammates du
// registre du template (project-config.json → agents.registry[], instances
// agents.generic[]), la version de leur copie, la présence d'un compagnon,
// leur modèle et effort, et la version de claude des teammates lancés.

type teammate struct {
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

var claudeVerRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// readTeammates : teammates déclarés par le template dans le projet. Sans
// registre (projet antérieur), les copies *.template.md, au tag du projet.
func readTeammates(proj string) []teammate {
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
	mk := func(name, tag, tpl, comp string) teammate {
		t := teammate{Name: name, Tag: tag, Companion: has(comp)}
		t.Model, t.Effort = frontKeys(filepath.Join(agents, tpl+".template.md"))
		if m, e := frontKeys(filepath.Join(agents, comp)); m != "" || e != "" {
			t.Model, t.Effort = cmp.Or(m, t.Model), cmp.Or(e, t.Effort)
		}
		return t
	}
	var out []teammate
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
	tag := templateTag(proj)
	files, _ := filepath.Glob(filepath.Join(agents, "*.template.md"))
	for _, f := range files {
		n := strings.TrimSuffix(filepath.Base(f), ".template.md")
		out = append(out, mk(n, tag, n, n+".md"))
	}
	return out
}

type member struct{ AgentType, Model string }

// teamMembers : pane tmux → teammate, pour la team du projet dont un pane est
// vivant (la plus récente si plusieurs).
func teamMembers(proj string, live map[string]bool) map[string]member {
	dirs, _ := filepath.Glob(filepath.Join(claudeDir(), "teams", "*", "config.json"))
	var best map[string]member
	var bestT int64
	for _, f := range dirs {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var cfg struct {
			Members []struct {
				Name       string `json:"name"`
				AgentType  string `json:"agentType"`
				Model      string `json:"model"`
				TmuxPaneID string `json:"tmuxPaneId"`
				Cwd        string `json:"cwd"`
			} `json:"members"`
		}
		if json.Unmarshal(b, &cfg) != nil || len(cfg.Members) == 0 || filepath.Clean(cfg.Members[0].Cwd) != filepath.Clean(proj) {
			continue
		}
		m := map[string]member{}
		for _, mb := range cfg.Members {
			if live[mb.TmuxPaneID] {
				m[mb.TmuxPaneID] = member{mb.AgentType, mb.Model}
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
	if v := claudeVerRe.FindString(cmd); v != "" {
		return v
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
			return claudeVerRe.FindString(filepath.Base(exe))
		}
	}
	return ""
}

// liveTeam : complète les teammates avec la version de claude (et le modèle
// réel) des panes lancés de la fenêtre du projet. Le leader (pane hors team)
// porte le teamleader.
func liveTeam(tm []teammate, proj string, panes []paneInfo) []teammate {
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
		tm = append(tm, teammate{Name: n, Model: rs[0].model, Claude: rs[0].ver, Count: len(rs)})
	}
	return tm
}
