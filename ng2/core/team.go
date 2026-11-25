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
	Model, Effort string // valeur résolue : compagnon > template (#250, P3 : plus le modèle réel du spawn)
	Claude        string // version de claude si lancé
	Count         int    // instances lancées
	RunModel      string // modèle réel d'un teammate lancé (config.json de la team) ; "" si inconnu

	// réglage modèle / effort depuis la carte (#250)
	CompFile              string // compagnon à écrire, relatif à .claude/agents ("generic.<nom>.md" pour une instance)
	ModelSrc, EffortSrc   string // "companion" (clé présente dans le compagnon) ou "template"
	TplModel, TplEffort   string // référence : template (instance : generic.md s'il fixe la clé)
	CompModel, CompEffort string // valeurs explicites du compagnon ("" si clé absente)
	Editable              bool   // le template porte model: ou effort:
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
	// instance générique : generic.template.md, puis generic.md (commun aux instances, repli), puis generic.<nom>.md
	mk := func(name, tag, tpl, comp string) Teammate {
		t := Teammate{Name: name, Tag: tag, Companion: has(comp), CompFile: comp}
		t.TplModel, t.TplEffort = frontKeys(filepath.Join(agents, tpl+".template.md"))
		t.Editable = t.TplModel != "" || t.TplEffort != ""
		if comp != tpl+".md" { // instance : generic.md sert de référence
			if m, e := frontKeys(filepath.Join(agents, tpl+".md")); m != "" || e != "" {
				t.TplModel, t.TplEffort = cmp.Or(m, t.TplModel), cmp.Or(e, t.TplEffort)
				t.Editable = true
			}
			t.Companion = t.Companion || has(tpl+".md")
		}
		t.CompModel, t.CompEffort = frontKeys(filepath.Join(agents, comp))
		t.Model, t.Effort = cmp.Or(t.CompModel, t.TplModel), cmp.Or(t.CompEffort, t.TplEffort)
		t.ModelSrc, t.EffortSrc = "template", "template"
		if t.CompModel != "" {
			t.ModelSrc = "companion"
		}
		if t.CompEffort != "" {
			t.EffortSrc = "companion"
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
			tm[i].RunModel = rs[0].model
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
	Agent         string // entrée du registre du template (deploy pour « deployer »)
	Companion     bool
	Model, Effort string // valeur résolue (#250, P3)
	Claude        string // version de claude si lancé
	Pane          string // %id, vide si non démarré
	Started       bool
	Active        bool
	Hidden        bool // mis de côté dans la réserve
	Pinned        bool // visible fixé à la main
	IdleLeft      int  // secondes avant masquage automatique (0 : sans objet)

	// réglage modèle / effort (#250) : sources, références, modèle réel d'un teammate lancé s'il diffère de la valeur
	// résolue (RunModel), éditable depuis la carte
	ModelSrc, EffortSrc, TplModel, TplEffort string
	Running                                  *TeamRunning
	Editable                                 bool

	// état du template (TplInfo.Apply) : ok / late / "" (neutre), version
	// cible ("" si inconnue), fichier compagnon ("" si aucun)
	TplState, TplTarget, CompanionFile string

	// écarts de la ligne avec l'en-tête de la carte (ApplyTeamHead) : OddTpl, OddClaude
	Odd []string
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
	return TeamLinesFor(tm, teamMembers(proj, live), panes, choices, hideIdle, now)
}

// registryName : nom de teammate dans la team → entrée du registre du template,
// quand ils diffèrent (CLAUDE.md, table « Agents Disponibles » : colonne Fichier).
var registryName = map[string]string{
	"deployer": "deploy", "planner": "implementation-planner", "marketing": "marketing-release",
	"teamleader": "teamleader", "leader": "teamleader",
}

// LiveMembers : membres d'une team déjà lue (tc) dont le pane est vivant ; pour
// les appelants qui gardent la team en mémoire (pas de relecture des fichiers).
func LiveMembers(tc *TeamConfig, panes []ProjPane) map[string]TeamMember {
	live := map[string]bool{}
	for _, p := range panes {
		live[p.ID] = true
	}
	m := map[string]TeamMember{}
	if tc != nil {
		for _, mb := range tc.Members {
			if mb.PaneID != "" && live[mb.PaneID] {
				m[mb.PaneID] = mb
			}
		}
	}
	return m
}

// TeamLinesFor : TeamLines avec les membres déjà connus (LiveMembers).
func TeamLinesFor(tm []Teammate, members map[string]TeamMember, panes []ProjPane, choices map[string]string, hideIdle int, now int64) []TeamLine {
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
		for _, k := range []string{name, registryName[name], registryName[typ]} {
			if t.Name == "" {
				t = reg[k]
			}
		}
		agent := cmp.Or(t.Name, typ)
		started[typ], started[name], started[agent] = true, true, true
		if t.Name == "" { // lancé hors registre : pas de valeur résolue, le modèle réel fait foi, non réglable
			t.Model = mb.Model
		}
		l := TeamLine{Name: name, Agent: agent, Tag: t.Tag, Companion: t.Companion, Model: t.Model, Effort: t.Effort,
			ModelSrc: t.ModelSrc, EffortSrc: t.EffortSrc, TplModel: t.TplModel, TplEffort: t.TplEffort, Editable: t.Editable,
			Running: runningOf(runModelDiff(t.Model, cmp.Or(mb.Model, t.RunModel))),
			Claude:  claudeVersion(p.Cmd, p.PID), Pane: p.ID, Started: true, Hidden: p.Hidden && hideIdle >= 0, // désactivé : jamais « caché »
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
			lines = append(lines, TeamLine{Name: t.Name, Agent: t.Name, Tag: t.Tag, Companion: t.Companion, Model: t.Model, Effort: t.Effort,
				ModelSrc: t.ModelSrc, EffortSrc: t.EffortSrc, TplModel: t.TplModel, TplEffort: t.TplEffort, Editable: t.Editable})
		}
	}
	return lines
}

// TeamRunning : ce qui tourne quand cela diffère de la valeur résolue (⟳ sur la carte) ; Effort : seulement s'il
// est connu (non relevé au spawn : jamais renseigné, R2 du plan).
type TeamRunning struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

func runningOf(model string) *TeamRunning {
	if model == "" {
		return nil
	}
	return &TeamRunning{Model: model}
}

// runModelDiff : modèle réel d'un teammate lancé s'il diffère de la valeur résolue (⟳ sur la carte), "" sinon. Le
// résolu est un alias (opus) ou un identifiant ; le réel, souvent un identifiant (claude-opus-4-6[1m]).
func runModelDiff(resolved, real string) string {
	r := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(real), "claude-"))
	w := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(resolved), "claude-"))
	if r == "" || w == "" || r == w || strings.HasPrefix(r, w+"-") || strings.HasPrefix(r, w+"[") {
		return ""
	}
	return real
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

// ── Team existante d'un projet (#231) ─────────────────────────────────────

// StartSessionPath : commande /start-session du template — sa présence dans le projet = projet géré.
func StartSessionPath(proj string) string {
	return filepath.Join(proj, ".claude", "commands", "start-session.md")
}

// TeamExists : la team du projet existe sur le disque (<CLAUDE_CONFIG_DIR>/teams/<team_name>/config.json) — une
// reprise, pas un démarrage neuf. team_name est lu dans <projet>/.claude/project-config.json ; absent, illisible ou
// suspect : une team dont le premier membre a le projet pour cwd (règle de teamMembers). Constat en Go, sur le
// dossier partagé (aucun wsl.exe / kubectl exec).
func TeamExists(proj string) bool {
	teams := filepath.Join(ClaudeDir(), "teams")
	if b, err := os.ReadFile(filepath.Join(proj, ".claude", "project-config.json")); err == nil {
		var pc struct {
			TeamName string `json:"team_name"`
		}
		if json.Unmarshal(b, &pc) == nil {
			if n := strings.TrimSpace(pc.TeamName); n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\`) {
				// team_name connu : seule cette team compte (pas de balayage de toutes les teams quand elle manque)
				_, err := os.Stat(filepath.Join(teams, n, "config.json"))
				return err == nil
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(teams, "*", "config.json"))
	want := path.Clean(ClaudePath(proj))
	for _, f := range files {
		if cfg, err := ReadTeam(f); err == nil && len(cfg.Members) > 0 && path.Clean(cfg.Members[0].Cwd) == want {
			return true
		}
	}
	return false
}
