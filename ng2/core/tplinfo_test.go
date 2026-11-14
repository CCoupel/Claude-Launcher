package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTplInfoApply(t *testing.T) {
	proj, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	_ = os.MkdirAll(filepath.Dir(TplVersionPath()), 0o755)
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proj, ".claude", "project-config.json"), `{"agents":{
		"registry":[{"name":"teamleader","tag":"v5.9.2"},{"name":"dev-backend","tag":"v5.9.2"},{"name":"qa","tag":"v5.8.0"},
		            {"name":"generic","tag":"v5.9.2"}],"generic":[{"name":"redacteur"}]}}`)
	write(filepath.Join(proj, ".claude", "agents", "dev-backend.md"), "adaptations")
	write(filepath.Join(proj, ".claude", "agents", "generic.redacteur.md"), "spec")
	write(filepath.Join(proj, ".claude", "agents", "qa.md"), "") // vide : pas de compagnon
	write(TplVersionPath(), "v5.9.2\n")

	lines := []TeamLine{{Name: "leader", Tag: "v5.9.2"}, {Name: "dev-backend", Tag: "v5.9.2"}, {Name: "qa", Tag: "v5.8.0"},
		{Name: "redacteur", Tag: "v5.9.2"}, {Name: "inconnu"}}
	got := ReadTplInfo(proj).Apply(lines)
	want := []struct {
		state, comp string
	}{{TplLineOK, ""}, {TplLineOK, "dev-backend.md"}, {TplLineLate, ""}, {TplLineOK, "generic.redacteur.md"}, {TplLineNone, ""}}
	for i, w := range want {
		if got[i].TplState != w.state || got[i].CompanionFile != w.comp || got[i].TplTarget != "v5.9.2" || got[i].Companion != (w.comp != "") {
			t.Errorf("ligne %s : %+v", got[i].Name, got[i])
		}
	}

	// cible inconnue (launcher hors ligne) : neutre, sans cible
	_ = os.Remove(TplVersionPath())
	for _, l := range ReadTplInfo(proj).Apply([]TeamLine{{Name: "qa", Tag: "v5.8.0"}}) {
		if l.TplState != TplLineNone || l.TplTarget != "" {
			t.Errorf("cible inconnue : %+v", l)
		}
	}
	// projet sans registre : neutre
	bare := t.TempDir()
	write(TplVersionPath(), "v5.9.2")
	for _, l := range ReadTplInfo(bare).Apply([]TeamLine{{Name: "cdp", Tag: "v2.0.0"}}) {
		if l.TplState != TplLineNone || l.CompanionFile != "" {
			t.Errorf("sans registre : %+v", l)
		}
	}
}

// Les teammates portent un nom d'instance (deployer, planner, marketing) qui
// diffère de l'entrée du registre (deploy, implementation-planner,
// marketing-release) : tag, état et compagnon viennent de l'entrée du registre,
// que l'agentType de la team le dise ou non ; pas de doublon « non démarré ».
func TestTeamLinesRegistryNames(t *testing.T) {
	unixOnly(t)
	root, proj, cache := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("XDG_CACHE_HOME", cache)
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(TplVersionPath(), "v5.9.2")
	write(filepath.Join(proj, ".claude", "project-config.json"), `{"agents":{"registry":[
		{"name":"teamleader","tag":"v5.9.2"},{"name":"deploy","tag":"v5.8.0"},
		{"name":"implementation-planner","tag":"v5.9.2"},{"name":"marketing-release","tag":"v5.9.2"},{"name":"qa","tag":"v5.9.2"}]}}`)
	for _, n := range []string{"deploy", "implementation-planner", "marketing-release"} {
		write(filepath.Join(proj, ".claude", "agents", n+".template.md"), "---\nmodel: sonnet\n---\n")
	}
	write(filepath.Join(proj, ".claude", "agents", "deploy.md"), "compagnon")
	write(filepath.Join(proj, ".claude", "agents", "implementation-planner.md"), "compagnon")
	write(filepath.Join(proj, ".claude", "agents", "marketing-release.md"), "compagnon")
	write(filepath.Join(root, "teams", "s1", "config.json"), `{"members":[
		{"name":"team-lead","agentType":"team-lead","tmuxPaneId":"%1","cwd":"`+proj+`"},
		{"name":"deployer","agentType":"deploy","tmuxPaneId":"%2"},
		{"name":"planner","agentType":"general-purpose","tmuxPaneId":"%3"},
		{"name":"marketing","agentType":"marketing-release","tmuxPaneId":"%4"}]}`)
	pane := func(id string) ProjPane { return ProjPane{PaneInfo: PaneInfo{ID: id, Cmd: "2.1.87"}} }
	lines := TeamLines(ReadTeammates(proj), proj, []ProjPane{pane("%1"), pane("%2"), pane("%3"), pane("%4")}, map[string]string{}, 0, 100)
	lines = ReadTplInfo(proj).Apply(lines)
	by := map[string]TeamLine{}
	for _, l := range lines {
		by[l.Name] = l
	}
	for name, want := range map[string]struct{ agent, state, file string }{
		"deployer":  {"deploy", TplLineLate, "deploy.md"},
		"planner":   {"implementation-planner", TplLineOK, "implementation-planner.md"},
		"marketing": {"marketing-release", TplLineOK, "marketing-release.md"},
	} {
		l, ok := by[name]
		if !ok || l.Agent != want.agent || l.TplState != want.state || l.CompanionFile != want.file || !l.Started {
			t.Errorf("%s : %+v", name, l)
		}
	}
	if by["deployer"].Tag != "v5.8.0" || by["deployer"].TplTarget != "v5.9.2" {
		t.Errorf("tag/cible de deployer : %+v", by["deployer"])
	}
	for _, dup := range []string{"deploy", "implementation-planner", "marketing-release", "teamleader"} {
		if l, ok := by[dup]; ok {
			t.Errorf("doublon %s : %+v", dup, l)
		}
	}
	if l := by["qa"]; l.Started || l.CompanionFile != "" || l.TplState != TplLineOK {
		t.Errorf("qa non démarré : %+v", l)
	}
}
