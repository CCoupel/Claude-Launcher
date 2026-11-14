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
