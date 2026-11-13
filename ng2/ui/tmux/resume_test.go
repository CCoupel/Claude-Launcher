package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

func TestTreeRefresh(t *testing.T) {
	base := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(base, "grp", name)
		_ = os.MkdirAll(p, 0o755)
		_ = os.WriteFile(filepath.Join(p, "README"), []byte("x"), 0o644)
		return p
	}
	a, b, c := mk("alpha"), mk("beta"), mk("gamma")
	cfg := core.Config{Roots: []string{base}, ResumeMaxAgeH: 24}
	root := fakeClaude(t, b, map[string]time.Duration{"bbbbbbbb-2": 10 * time.Minute})
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	m := model{cfg: cfg, git: map[string]core.GitInfo{}, detail: map[string]core.GitDetail{}, asked: map[string]bool{},
		tpl: map[string]core.TemplateState{}, windows: map[string]bool{}}
	m.setNodes(scanCmd(cfg.Roots)().(treeMsg))
	m.sessions = sessCmd(cfg, m.projectPaths(), 0)().(sessMsg)
	for i, v := range m.visible { // sélection sur gamma
		if m.nodes[v].Path == c {
			m.cursor = i
		}
	}
	m.git[a] = core.GitInfo{IsRepo: true}

	// session en suspens de beta : icône tant que le projet n'est pas ouvert ici
	nb := core.Node{Path: b, Key: "beta", Project: true}
	if got := m.badges(nb); !strings.Contains(got, "↺") {
		t.Errorf("badge beta=%q", got)
	}
	if l := strings.Join(legendLines(m.projInfo(nb), 0), "\n"); !strings.Contains(l, "↺") || !strings.Contains(l, "reprise proposée") {
		t.Errorf("légende beta=%q", l)
	}
	m.windows["beta"] = true
	if got := m.badges(nb); strings.Contains(got, "↺") {
		t.Errorf("projet ouvert : badge inattendu %q", got)
	}
	if strings.Contains(m.badges(core.Node{Path: a, Key: "alpha"}), "↺") {
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

func TestEnsureEndSession(t *testing.T) {
	p := t.TempDir()
	ensureEndSession(p)
	f := filepath.Join(p, ".claude", "commands", "end-session.md")
	b, err := os.ReadFile(f)
	if err != nil || string(b) != endSessionCmd {
		t.Fatalf("end-session.md non déposé : %v", err)
	}
	// une commande existante (celle du template ou de l'utilisateur) n'est pas écrasée
	_ = os.WriteFile(f, []byte("perso"), 0o644)
	ensureEndSession(p)
	if b, _ := os.ReadFile(f); string(b) != "perso" {
		t.Error("end-session.md existant écrasé")
	}
}
