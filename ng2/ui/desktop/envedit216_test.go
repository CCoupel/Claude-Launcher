package desktop

import (
	"os"
	"strings"
	"testing"

	"claude-launcher-ng2/core"
)

const envEditMention216 = "fermez le projet pour modifier son environnement"

func envChange216(mt any, pkgs ...any) map[string]any {
	return map[string]any{"mtime": mt, "changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": pkgs}}}
}

func setOwner216(t *testing.T, foreign, running string) {
	t.Helper()
	of, or := foreignOwner.Get(), runningOwner.Get()
	foreignOwner.Set(func(string) string { return foreign })
	runningOwner.Set(func(string) string { return running })
	t.Cleanup(func() { foreignOwner.Set(of); runningOwner.Set(or) })
}

// Formulaire (projectApply) : env.yaml refusé projet ouvert ici, ouvert par un autre launcher, conteneur actif — rien n'est
// écrit (ni env.yaml ni couleur) ; couleur seule reste à chaud ; projet libre : écrit.
func TestEnvYamlRefuseProjetOuvertOuConteneurActif216(t *testing.T) {
	for name, c := range map[string]struct {
		open         bool
		foreign, run string
		want         string
	}{
		"ouvert ici":      {true, "", "", "projet ouvert dans ce launcher"},
		"autre launcher":  {false, "pod-x", "", "autre launcher (pod-x)"},
		"conteneur actif": {false, "", "p-alpha sur srv1", "conteneur ou pod en cours d'exécution (p-alpha sur srv1)"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newProjFix(t)
			setOwner216(t, c.foreign, c.run)
			if c.open {
				f.open()
			}
			before, _ := os.ReadFile(core.EnvPath(f.proj))
			mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
			r := f.call("projectApply", map[string]any{"path": f.proj, "color": "52", "env": envChange216(mt, "ripgrep", "fd")})
			msg, _ := r["msg"].(string)
			if r["ok"] != false || r["field"] != "env" || !strings.Contains(msg, c.want) || !strings.Contains(msg, envEditMention216) {
				t.Fatalf("refus attendu (%s) : %v", c.want, r)
			}
			if after, _ := os.ReadFile(core.EnvPath(f.proj)); string(after) != string(before) {
				t.Error("env.yaml écrit malgré le refus")
			}
			if cf, _ := os.ReadFile(core.ConfigPath()); strings.Contains(string(cf), "52") {
				t.Error("couleur écrite malgré le refus de l'env")
			}
			if r := f.call("projectApply", map[string]any{"path": f.proj, "color": "52"}); r["ok"] != true {
				t.Errorf("couleur seule refusée : %v", r)
			}
		})
	}
	f := newProjFix(t)
	setOwner216(t, "", "")
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	if r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "ripgrep", "fd")}); r["ok"] != true {
		t.Fatalf("projet libre : %v", r)
	}
}

// Éditeur YAML (configSave / configApply, kind project-env) : mêmes refus, revérifiés à l'écriture ; projet libre : écrit.
func TestEnvYamlEditeurRefuseProjetOuvert216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "", "")
	f.open()
	before, _ := os.ReadFile(core.EnvPath(f.proj))
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	r := f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\npackages: [x]\n", "mtime": mt})
	if r["saved"] == true || r["field"] != "env" || !strings.Contains(r["msg"].(string), envEditMention216) {
		t.Fatalf("configSave sur projet ouvert : %v", r)
	}
	r = f.call("configApply", map[string]any{"kind": "project-env", "path": f.proj, "mtime": mt,
		"changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"x"}}}})
	if r["saved"] == true || r["field"] != "env" {
		t.Fatalf("configApply sur projet ouvert : %v", r)
	}
	if after, _ := os.ReadFile(core.EnvPath(f.proj)); string(after) != string(before) {
		t.Error("env.yaml écrit malgré le refus")
	}
	// projet fermé mais conteneur actif : refus (revérifié à l'écriture)
	f.a.mu.Lock()
	delete(f.a.terms, f.proj)
	f.a.mu.Unlock()
	f.a.openTabs.Delete(f.proj)
	setOwner216(t, "", "p-alpha sur srv1")
	if r := f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\n", "mtime": mt}); r["saved"] == true || r["field"] != "env" {
		t.Fatalf("conteneur actif : %v", r)
	}
	setOwner216(t, "", "")
	if r := f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\npackages: [x]\n", "mtime": mt}); r["saved"] != true {
		t.Fatalf("projet libre : %v", r)
	}
	// les autres fichiers (config.conf…) ne sont pas concernés
	setOwner216(t, "pod-x", "c sur b")
	if r := f.call("configSave", map[string]any{"kind": "config.conf", "content": "USAGE_REFRESH_SEC=300\n", "mtime": ""}); r["field"] == "env" {
		t.Errorf("config.conf refusé par la garde de l'env : %v", r)
	}
}
