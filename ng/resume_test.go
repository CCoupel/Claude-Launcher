package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProjectDirName(t *testing.T) {
	got := projectDirName("/mnt/c/Users/cyril/Documents/PROJETS/CLAUDE_LAUNCHER/Claude-Launcher")
	if want := "-mnt-c-Users-cyril-Documents-PROJETS-CLAUDE-LAUNCHER-Claude-Launcher"; got != want {
		t.Errorf("projectDirName=%q, attendu %q", got, want)
	}
	if got := projectDirName("/a/é.b"); got != "-a---b" {
		t.Errorf("projectDirName non ASCII=%q", got)
	}
}

// fakeClaude : conversations (id → âge) d'un projet sous un faux ~/.claude.
func fakeClaude(t *testing.T, path string, convs map[string]time.Duration) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects", projectDirName(path))
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	for id, age := range convs {
		f := filepath.Join(dir, id+".jsonl")
		content := "{}\n"
		if age < 0 { // conversation vide : ignorée
			content, age = "", -age
		}
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		_ = os.Chtimes(f, mt, mt)
	}
	return root
}

func TestLastSession(t *testing.T) {
	root := fakeClaude(t, "/p/proj", map[string]time.Duration{
		"aaaaaaaa-1": 3 * time.Hour,
		"bbbbbbbb-2": 10 * time.Minute,
		"cccccccc-3": -time.Minute, // plus récente mais vide
	})
	r, ok := lastSession(root, "/p/proj")
	if !ok || r.ID != "bbbbbbbb-2" || len(r.Teammates) != 0 || r.Crashed {
		t.Fatalf("lastSession=%+v,%v", r, ok)
	}
	if _, ok := lastSession(root, "/p/autre"); ok {
		t.Error("projet sans conversation : ok attendu à false")
	}

	// team de la session : teammates listés, leader disparu sans libérer la team
	d := filepath.Join(root, "teams", "session-bbbbbbbb")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"leadSessionId":"bbbbbbbb-2",
		"leadProcess":{"pid":999999999},
		"members":[{"name":"team-lead","agentType":"team-lead"},{"name":"dev","agentType":"general-purpose"},{"name":"qa","agentType":"general-purpose"}]}`), 0o644)
	r, _ = lastSession(root, "/p/proj")
	if want := []string{"dev", "qa"}; !reflect.DeepEqual(r.Teammates, want) || !r.Crashed {
		t.Errorf("team : %+v", r)
	}
	if p := resumePrompt(r); !strings.Contains(p, "dev, qa") {
		t.Errorf("resumePrompt=%q", p)
	}
	if p := resumePrompt(Resumable{ID: "x"}); p != "" {
		t.Errorf("resumePrompt sans team=%q", p)
	}

	// team d'une autre session portant le même préfixe : ignorée
	_ = os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"leadSessionId":"bbbbbbbb-autre","members":[{"name":"dev"}]}`), 0o644)
	if r, _ = lastSession(root, "/p/proj"); r.Teammates != nil {
		t.Errorf("team étrangère prise en compte : %+v", r)
	}
}

func TestResumeShell(t *testing.T) {
	root := fakeClaude(t, "/p/proj", map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	cfg := Config{ResumeMode: "ask", ResumeMaxAgeH: 24}
	pre, r, ok := cfg.resumeShell("/p/proj")
	if !ok || r.ID != "bbbbbbbb-2" || !strings.Contains(pre, "read -rp") || !strings.Contains(pre, "10 min") {
		t.Errorf("ask : %q %+v %v", pre, r, ok)
	}
	cfg.ResumeMode = "always"
	if pre, _, ok = cfg.resumeShell("/p/proj"); !ok || pre != "_rs=1; " {
		t.Errorf("always : %q %v", pre, ok)
	}
	cfg.ResumeMode = "never"
	if _, _, ok = cfg.resumeShell("/p/proj"); ok || cfg.resumeMaxAge() != 0 {
		t.Error("never : aucune reprise attendue")
	}
	cfg = Config{ResumeMode: "ask", ResumeMaxAgeH: 24}

	// leader toujours vivant (ce processus de test) : session ouverte ailleurs
	d := filepath.Join(root, "teams", "session-bbbbbbbb")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, "config.json"), []byte(fmt.Sprintf(
		`{"leadSessionId":"bbbbbbbb-2","leadProcess":{"pid":%d},"members":[]}`, os.Getpid())), 0o644)
	if _, r, ok = cfg.resumeShell("/p/proj"); ok || !r.InUse {
		t.Errorf("session en cours : aucune reprise attendue (%+v)", r)
	}

	if _, _, ok = cfg.resumeShell("/p/autre"); ok {
		t.Error("projet sans conversation : aucune reprise attendue")
	}
	old := fakeClaude(t, "/p/proj", map[string]time.Duration{"x": 30 * time.Hour})
	t.Setenv("CLAUDE_CONFIG_DIR", old)
	if _, _, ok = cfg.resumeShell("/p/proj"); ok {
		t.Error("conversation trop ancienne : aucune reprise attendue")
	}
}

func TestSessionEnded(t *testing.T) {
	root := fakeClaude(t, "/p/proj", map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	f := filepath.Join(root, "projects", projectDirName("/p/proj"), "bbbbbbbb-2.jsonl")
	write := func(lines ...string) {
		_ = os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	cfg := Config{ResumeMode: "ask", ResumeMaxAgeH: 24}

	// marque citée dans un résultat d'outil ou un message méta : pas une clôture
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"<command-name>/end-session</command-name>"}]}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<command-name>/end-session</command-name>"}}`)
	if sessionEnded(f) {
		t.Error("marque citée prise pour une clôture")
	}
	if _, _, ok := cfg.resumeShell("/p/proj"); !ok {
		t.Error("session non close : reprise attendue")
	}

	// /end-session tapé, suivi d'échanges de fin (commit, push…) : session close
	write(`{"type":"user","message":{"role":"user","content":"bonjour"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-message>end-session</command-message>\n<command-name>/end-session</command-name>"},"origin":{"kind":"human"}}`,
		`{"type":"user","message":{"role":"user","content":"commit et push"}}`)
	if !sessionEnded(f) {
		t.Fatal("/end-session non détecté")
	}
	if _, r, ok := cfg.resumeShell("/p/proj"); ok || !r.Ended {
		t.Errorf("session close : aucune reprise attendue (%+v)", r)
	}
	if resumableSession(root, "bbbbbbbb-2", 24*time.Hour) {
		t.Error("session close : sa team ne doit plus être épargnée")
	}
}

func TestTreeRefresh(t *testing.T) {
	base := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(base, "grp", name)
		_ = os.MkdirAll(p, 0o755)
		_ = os.WriteFile(filepath.Join(p, "README"), []byte("x"), 0o644)
		return p
	}
	a, b, c := mk("alpha"), mk("beta"), mk("gamma")
	cfg := Config{Roots: []string{base}, ResumeMaxAgeH: 24}
	root := fakeClaude(t, b, map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	m := model{cfg: cfg, git: map[string]GitInfo{}, detail: map[string]GitDetail{}, asked: map[string]bool{},
		tpl: map[string]TemplateState{}, windows: map[string]bool{}}
	m.setNodes(scanCmd(cfg.Roots)().(treeMsg))
	m.sessions = sessCmd(cfg, m.projectPaths(), 0)().(sessMsg)
	for i, v := range m.visible { // sélection sur gamma
		if m.nodes[v].Path == c {
			m.cursor = i
		}
	}
	m.git[a] = GitInfo{IsRepo: true}

	// session en suspens de beta : icône tant que le projet n'est pas ouvert ici
	nb := Node{Path: b, Key: "beta", Project: true}
	if got := m.sessionBadge(nb); !strings.Contains(got, "↺") {
		t.Errorf("badge beta=%q", got)
	}
	m.windows["beta"] = true
	if got := m.sessionBadge(nb); got != "" {
		t.Errorf("projet ouvert : badge inattendu %q", got)
	}
	if m.sessionBadge(Node{Path: a, Key: "alpha"}) != "" {
		t.Error("alpha sans session : badge inattendu")
	}

	// dossier alpha supprimé hors du launcher : retiré, sélection gardée sur gamma
	_ = os.RemoveAll(a)
	tm := scanCmd(cfg.Roots)().(treeMsg)
	if sameTree(m.nodes, tm) {
		t.Fatal("suppression non vue par scanTree")
	}
	m.setNodes(tm)
	for _, p := range m.projectPaths() {
		if p == a {
			t.Error("alpha encore listé")
		}
	}
	if _, ok := m.git[a]; ok {
		t.Error("état git d'alpha non oublié")
	}
	if got := m.nodes[m.visible[m.cursor]].Path; got != c {
		t.Errorf("sélection=%q, attendu gamma", got)
	}
}
