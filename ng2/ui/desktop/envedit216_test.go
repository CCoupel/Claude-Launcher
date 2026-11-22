package desktop

import (
	"os"
	"strings"
	"testing"
	"time"

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
			bl, _ := r["blockers"].([]any)
			if r["ok"] != false || r["field"] != "env" || !strings.Contains(msg, c.want) || !strings.Contains(msg, envEditMention216) || len(bl) == 0 || !strings.Contains(bl[0].(string), c.want) {
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

// Affichage : projectSettings.envLocked / envLockReason et configOpen.readOnly suivent « projet ouvert » (sans interroger les
// moteurs) ; projet libre : déverrouillés.
func TestEnvLockedAffichage216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "", "")
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	if s["envLocked"] != false || s["envLockReason"] != "" {
		t.Errorf("libre : %v %v", s["envLocked"], s["envLockReason"])
	}
	if d := f.call("configOpen", map[string]any{"kind": "project-env", "path": f.proj}); d["readOnly"] == true {
		t.Errorf("configOpen libre en lecture seule : %v", d["readOnly"])
	}
	f.open()
	s = f.call("projectSettings", map[string]any{"path": f.proj})
	if s["envLocked"] != true || !strings.Contains(s["envLockReason"].(string), envEditMention216) {
		t.Errorf("ouvert : %v %v", s["envLocked"], s["envLockReason"])
	}
	d := f.call("configOpen", map[string]any{"kind": "project-env", "path": f.proj})
	if d["readOnly"] != true || !strings.Contains(d["readOnlyReason"].(string), envEditMention216) {
		t.Errorf("configOpen ouvert : %v %v", d["readOnly"], d["readOnlyReason"])
	}
	if r := f.call("configValidate", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: x\n"}); r == nil {
		t.Error("configValidate doit rester permis")
	}
}

// Affichage d'avance : conteneur actif vu par la dernière lecture, session vivante d'un autre launcher.
func TestEnvLockedAffichageConteneurEtAutreLauncher216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "", "")
	founds := []core.Found{{Name: "p-alpha", Project: f.proj, Running: true}}
	f.a.ctrFounds.Store(&founds)
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	if s["envLocked"] != true || !strings.Contains(s["envLockReason"].(string), "p-alpha") || !strings.Contains(s["envLockReason"].(string), envEditMention216) {
		t.Errorf("conteneur actif : %v %v", s["envLocked"], s["envLockReason"])
	}
	none := []core.Found{}
	f.a.ctrFounds.Store(&none)
	live := map[string]bool{f.proj: true}
	f.a.liveSnap.Store(&live)
	if s := f.call("projectSettings", map[string]any{"path": f.proj}); s["envLocked"] != true {
		t.Errorf("session vivante : %v", s["envLocked"])
	}
	if d := f.call("configOpen", map[string]any{"kind": "project-env", "path": f.proj}); d["readOnly"] != true {
		t.Errorf("configOpen : %v", d["readOnly"])
	}
}

// Backend injoignable (propriétaire / conteneur non vérifiables) ET projet non ouvert ici : env.yaml ÉCRIT, avec l'avertissement
// « état du conteneur inconnu » ; un blocker certain reste un refus.
func TestEnvYamlBackendInjoignableAutoriseAvecAvertissement216(t *testing.T) {
	for name, c := range map[string][2]string{
		"propriétaire": {core.UnverifiablePrefix + " : propriétaire non vérifiable", ""},
		"conteneur":    {"", core.UnverifiablePrefix + " : conteneur non vérifiable"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newProjFix(t)
			setOwner216(t, c[0], c[1])
			mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
			r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "ripgrep", "fd")})
			if r["ok"] != true || r["env"].(map[string]any)["saved"] != true || r["warn"] != core.EnvWarnUnknown {
				t.Fatalf("écriture avertie attendue : %v", r)
			}
			if b, _ := os.ReadFile(core.EnvPath(f.proj)); !strings.Contains(string(b), "fd") {
				t.Error("env.yaml non écrit")
			}
			// éditeur YAML : même autorisation avec avertissement
			mt2 := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
			e := f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\npackages: [y]\n", "mtime": mt2})
			if e["saved"] != true || e["warn"] != core.EnvWarnUnknown {
				t.Errorf("configSave : %v", e)
			}
		})
	}
	// injoignable pour l'un, blocker CERTAIN pour l'autre : refus
	f := newProjFix(t)
	setOwner216(t, core.UnverifiablePrefix+" : propriétaire non vérifiable", "p-alpha sur srv1")
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	if r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "x")}); r["ok"] != false || r["field"] != "env" {
		t.Errorf("conteneur actif confirmé : %v", r)
	}
	// projet ouvert ici : refus même si le backend est injoignable
	f.open()
	setOwner216(t, core.UnverifiablePrefix+" : propriétaire non vérifiable", "")
	if r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "x")}); r["ok"] != false || r["field"] != "env" {
		t.Errorf("ouvert ici : %v", r)
	}
}

// Backend / image : moteur NON vérifiable (échec rapide OU délai) = REFUS fail-closed, pour le PROPRIÉTAIRE (autre launcher)
// comme pour le CONTENEUR ; l'env.yaml seul reste écrit avec warn.
func TestBackendImageNonVerifiableRefuse216(t *testing.T) {
	unv := core.UnverifiablePrefix + " : non vérifiable"
	for _, src := range []string{"propriétaire", "conteneur"} {
		for _, mode := range []string{"échec rapide", "délai"} {
			for _, what := range []string{"backend", "image"} {
				t.Run(src+"/"+mode+"/"+what, func(t *testing.T) {
					f := newProjFix(t)
					setOwner216(t, "", "")
					hook := &runningOwner // conteneur
					if src == "propriétaire" {
						hook = nil
					}
					if mode == "échec rapide" {
						if src == "propriétaire" {
							foreignOwner.Set(func(string) string { return unv })
						} else {
							(*hook).Set(func(string) string { return unv })
						}
					} else {
						release := make(chan struct{})
						oldT := foreignOwnerTimeout.Get()
						foreignOwnerTimeout.Set(150 * time.Millisecond)
						hang := func(string) string { <-release; return "" } // le moteur ne répond pas
						if src == "propriétaire" {
							foreignOwner.Set(hang)
						} else {
							(*hook).Set(hang)
						}
						t.Cleanup(func() { close(release); foreignOwnerTimeout.Set(oldT) })
					}
					args := map[string]any{"path": f.proj}
					if what == "backend" {
						args["backend"], args["image"] = "srv1", "docker.io/library/debian:12"
					} else {
						if err := core.WriteAssignment(f.proj, "srv1"); err != nil { // image seule : le projet a déjà un backend valide
							t.Fatal(err)
						}
						args["image"] = "docker.io/library/debian:13"
					}
					before, _ := os.ReadFile(core.EnvPath(f.proj))
					t0 := time.Now()
					r := f.call("projectApply", args)
					bl, _ := r["blockers"].([]any)
					if r["ok"] != false || r["field"] != "env" || len(bl) == 0 || !strings.Contains(r["msg"].(string), core.UnverifiablePrefix) {
						t.Fatalf("refus fail-closed attendu : %v", r)
					}
					if d := time.Since(t0); d > 3*time.Second {
						t.Errorf("garde non bornée : %v", d)
					}
					if after, _ := os.ReadFile(core.EnvPath(f.proj)); string(after) != string(before) {
						t.Error("env.yaml modifié")
					}
					if what == "backend" && core.Assigned(f.proj) {
						t.Error("affectation écrite malgré le refus")
					}
				})
			}
		}
	}
	// env.yaml seul : toujours écrit avec avertissement
	f := newProjFix(t)
	setOwner216(t, "", unv)
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	if r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "ripgrep", "fd")}); r["ok"] != true || r["warn"] != core.EnvWarnUnknown {
		t.Fatalf("env seul : %v", r)
	}
}
