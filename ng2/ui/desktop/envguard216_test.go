//go:build !windows

package desktop

// #216 (complément) : l'env.yaml d'un projet (formulaire, éditeur YAML, F2) est refusé tant que le projet est ouvert ou que
// son conteneur tourne — compléments boîte noire à envedit216_test.go : forme exacte du refus et liste des raisons,
// propriétaire / conteneur injoignables (borné, jamais bloquant), appels forgés en --web, F2 (execApply), réglages à chaud
// jamais refusés. Projets factices, propriétaire et conteneur injectés (setOwner216) : aucun vrai moteur.

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

func blockersOf216(t *testing.T, r map[string]any) []string {
	t.Helper()
	var out []string
	l, _ := r["blockers"].([]any)
	for _, b := range l {
		out = append(out, b.(string))
	}
	return out
}

// Refus : forme {ok:false, saved:false (config*), field:"env", msg, blockers} identique pour projectApply, configSave et
// configApply ; msg = « environnement non modifiable : <raisons séparées par « ; »> — fermez le projet… » ; rien d'écrit.
func TestRefusEnvFormeEtListeDesRaisons216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "pod-x", "p-alpha sur srv1")
	f.open()
	before := func() string { b, _ := os.ReadFile(core.EnvPath(f.proj)); return string(b) }
	envBefore := before()
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	calls := map[string]map[string]any{
		"projectApply": f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "x")}),
		"configSave":   f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\npackages: [x]\n", "mtime": mt}),
		"configApply": f.call("configApply", map[string]any{"kind": "project-env", "path": f.proj, "mtime": mt,
			"changes": []any{map[string]any{"op": "set", "at": []string{"packages"}, "value": []any{"x"}}}}),
	}
	for name, r := range calls {
		bl := blockersOf216(t, r)
		if r["ok"] == true || r["field"] != "env" || len(bl) != 3 {
			t.Errorf("%s : %v, attendu {ok:false, field:env, 3 raisons}", name, r)
			continue
		}
		for i, want := range []string{"projet ouvert dans ce launcher", "autre launcher (pod-x)", "conteneur ou pod en cours d'exécution (p-alpha sur srv1)"} {
			if !strings.Contains(bl[i], want) {
				t.Errorf("%s : raison %d = %q, attendu %q (ordre : ouvert, autre launcher, conteneur)", name, i, bl[i], want)
			}
		}
		if want := core.EnvEditMessage(bl); r["msg"] != want {
			t.Errorf("%s : msg = %q, attendu %q", name, r["msg"], want)
		}
		if name != "projectApply" && r["saved"] != false {
			t.Errorf("%s : saved = %v, attendu false", name, r["saved"])
		}
	}
	if before() != envBefore {
		t.Error("env.yaml écrit malgré les refus")
	}
	// affichage : projet ouvert → éditeur et formulaire verrouillés (lecture seule) avec la raison, sans interroger les moteurs
	s := f.call("projectSettings", map[string]any{"path": f.proj})
	if s["open"] != true || s["envLocked"] != true || !strings.Contains(s["envLockReason"].(string), "fermez le projet") {
		t.Errorf("projectSettings d'un projet ouvert : open=%v envLocked=%v raison=%v", s["open"], s["envLocked"], s["envLockReason"])
	}
	o := f.call("configOpen", map[string]any{"kind": "project-env", "path": f.proj})
	if o["readOnly"] != true || !strings.Contains(o["readOnlyReason"].(string), "fermez le projet") {
		t.Errorf("configOpen (éditeur YAML) d'un projet ouvert : readOnly=%v raison=%v", o["readOnly"], o["readOnlyReason"])
	}
}

// Propriétaire ou conteneur INJOIGNABLE (délai dépassé) : la vérification est BORNÉE (jamais d'attente sans fin) ; pour
// l'env.yaml (projet non ouvert ici) ce n'est PAS un refus : écrit, avec l'avertissement « état du conteneur inconnu ».
// Le déplacement, lui, reste fail-closed (déplacer sans savoir est irréversible). Projet ouvert ici : refus malgré tout.
func TestBackendInjoignableBorneAutoriseAvecAvertissement216(t *testing.T) {
	old := foreignOwnerTimeout.Set(300 * time.Millisecond)
	t.Cleanup(func() { foreignOwnerTimeout.Set(old) })
	for name, hang := range map[string]func(release chan struct{}){
		"propriétaire": func(rel chan struct{}) {
			of, runOf := foreignOwner.Get(), runningOwner.Get()
			foreignOwner.Set(func(string) string { <-rel; return "" })
			runningOwner.Set(func(string) string { return "" })
			t.Cleanup(func() { foreignOwner.Set(of); runningOwner.Set(runOf) })
		},
		"conteneur": func(rel chan struct{}) {
			of, runOf := foreignOwner.Get(), runningOwner.Get()
			foreignOwner.Set(func(string) string { return "" })
			runningOwner.Set(func(string) string { <-rel; return "" })
			t.Cleanup(func() { foreignOwner.Set(of); runningOwner.Set(runOf) })
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newProjFix(t)
			rel := make(chan struct{})
			hang(rel)
			t.Cleanup(func() { close(rel) })
			mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
			t0 := time.Now()
			r := f.call("projectApply", map[string]any{"path": f.proj, "env": envChange216(mt, "ripgrep", "fd")})
			if el := time.Since(t0); el > 3*time.Second {
				t.Errorf("réponse après %v : la vérification doit être bornée (≈ 300 ms)", el)
			}
			if r["ok"] != true || r["field"] != nil || r["warn"] != core.EnvWarnUnknown {
				t.Fatalf("projectApply = %v, attendu {ok:true, warn: %q} (backend injoignable : écrit, avec avertissement)", r, core.EnvWarnUnknown)
			}
			if b, _ := os.ReadFile(core.EnvPath(f.proj)); !strings.Contains(string(b), "fd") {
				t.Error("env.yaml non écrit alors que seul un état INCONNU (pas un blocker confirmé) le contestait")
			}
			// éditeur YAML : même autorisation, même avertissement
			mt2 := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
			e := f.call("configSave", map[string]any{"kind": "project-env", "path": f.proj, "content": "image: docker.io/library/debian:12\npackages: [y]\n", "mtime": mt2})
			if e["saved"] != true || e["warn"] != core.EnvWarnUnknown {
				t.Errorf("configSave : %v", e)
			}
			// déplacement : FAIL-CLOSED (blocker « non vérifiable » dans projectMoveInfo, refus de projectMove), rien déplacé
			info := f.call("projectMoveInfo", map[string]any{"path": f.proj})
			if bl := strings.Join(blockersOf216(t, info), " | "); !strings.Contains(bl, "non vérifiable") {
				t.Errorf("projectMoveInfo.blockers = %q, attendu une raison « non vérifiable » (le déplacement reste fail-closed)", bl)
			}
			base := filepath.Dir(filepath.Dir(f.proj))
			v := f.call("projectMove", map[string]any{"path": f.proj, "base": base, "category": "AUTRE", "newCategory": true, "name": "beta"})
			if v["ok"] == true {
				t.Errorf("déplacement accepté sans pouvoir vérifier le propriétaire / conteneur : %v", v)
			}
			if _, err := os.Stat(core.EnvPath(f.proj)); err != nil {
				t.Errorf("projet déplacé ou env.yaml perdu malgré le refus : %v", err)
			}
		})
	}
	// blocker CONFIRMÉ pour l'un, injoignable pour l'autre : refus ; ouvert ici : refus même si le backend est injoignable
	g := newProjFix(t)
	setOwner216(t, core.UnverifiablePrefix+" : propriétaire non vérifiable", "p-alpha sur srv1")
	mt := g.call("projectSettings", map[string]any{"path": g.proj})["envMtime"]
	if r := g.call("projectApply", map[string]any{"path": g.proj, "env": envChange216(mt, "x")}); r["ok"] == true || r["field"] != "env" {
		t.Errorf("conteneur actif confirmé + propriétaire injoignable : %v", r)
	}
	g.open()
	setOwner216(t, core.UnverifiablePrefix+" : propriétaire non vérifiable", "")
	if r := g.call("projectApply", map[string]any{"path": g.proj, "env": envChange216(mt, "x")}); r["ok"] == true || r["field"] != "env" {
		t.Errorf("ouvert ici + backend injoignable : %v", r)
	}
}

// Appel forgé en --web : mêmes refus que dans la fenêtre ; sans session : refusé.
func TestRefusEnvAppelForgeWeb216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "", "")
	f.open()
	envBefore, _ := os.ReadFile(core.EnvPath(f.proj))
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	w := &webBridge{app: f.a, token: "tok216e", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	body, _ := json.Marshal(map[string]any{"method": "projectApply", "args": map[string]any{"path": f.proj, "env": envChange216(mt, "pirate")}})
	resp, out := doWith(t, srv.URL, "POST", "/api/call", "", "tok216e", string(body), "application/json", "")
	var v map[string]any
	_ = json.Unmarshal([]byte(out), &v)
	if res, ok := v["result"].(map[string]any); ok {
		v = res
	}
	if resp.StatusCode != 200 || v["ok"] == true || v["field"] != "env" || !strings.Contains(out, "fermez le projet") {
		t.Errorf("projectApply forgé : %d %s", resp.StatusCode, out)
	}
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", string(body), "application/json", ""); resp.StatusCode == 200 {
		t.Error("projectApply accepté sans cookie")
	}
	if after, _ := os.ReadFile(core.EnvPath(f.proj)); string(after) != string(envBefore) {
		t.Error("env.yaml écrit par un appel web forgé")
	}
}

// F2 (execApply : backend / image dans l'env.yaml) : refusé projet ouvert ici (même consigne que les autres entrées),
// ouvert ailleurs, conteneur actif ; projet libre : écrit.
func TestExecApplyRefuseEtAutorise216(t *testing.T) {
	// moteurs factices en échec immédiat : la relecture des conteneurs qui suit un F2 réussi ne doit JAMAIS joindre le serveur
	// docker (10.0.0.1:2375) du backend de la fixture — sans cela ce test attendait 8 s un délai réseau
	engines := t.TempDir()
	for _, n := range []string{"podman", "docker"} {
		_ = os.WriteFile(filepath.Join(engines, n), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	}
	t.Setenv("PATH", engines+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := newProjFix(t)
	be, img := "srv1", "docker.io/library/alpine:3.20"
	for name, c := range map[string]struct {
		open         bool
		foreign, run string
		want         string
	}{
		"ouvert ici":      {true, "", "", "projet ouvert dans ce launcher — fermez le projet pour modifier son environnement"},
		"autre launcher":  {false, "pod-x", "", "autre launcher (pod-x)"},
		"conteneur actif": {false, "", "p-alpha sur srv1", "conteneur ou pod en cours d'exécution"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newProjFix(t)
			setOwner216(t, c.foreign, c.run)
			if c.open {
				g.open()
			}
			envBefore, _ := os.ReadFile(core.EnvPath(g.proj))
			asBefore := core.Assigned(g.proj)
			_, err := g.a.execApply(g.proj, img, be, false)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("execApply = %v, attendu un refus contenant %q", err, c.want)
			}
			if after, _ := os.ReadFile(core.EnvPath(g.proj)); string(after) != string(envBefore) || core.Assigned(g.proj) != asBefore {
				t.Error("env.yaml ou affectation modifiés malgré le refus")
			}
		})
	}
	setOwner216(t, "", "")
	if _, err := f.a.execApply(f.proj, img, be, false); err != nil {
		t.Fatalf("projet libre : %v", err)
	}
	if got := core.ProjectBackend(f.proj).Name; got != be {
		t.Errorf("backend = %q, attendu %s", got, be)
	}
	if b, _ := os.ReadFile(core.EnvPath(f.proj)); !strings.Contains(string(b), "alpine:3.20") {
		t.Errorf("image non écrite dans l'env.yaml : %s", b)
	}
}

// Organisation, masquage des inactifs et couleur : à chaud sur un projet ouvert, jamais refusés — la garde ne les concerne pas.
func TestReglagesAChaudJamaisRefuses216(t *testing.T) {
	f := newProjFix(t)
	setOwner216(t, "pod-x", "p-alpha sur srv1") // tout est bloqué pour l'env
	f.open()
	for name, args := range map[string]map[string]any{
		"couleur":      {"path": f.proj, "color": "52"},
		"organisation": {"path": f.proj, "layout": "left"},
		"masquage":     {"path": f.proj, "hideIdle": "none"},
		"les trois":    {"path": f.proj, "color": "22", "layout": "top", "hideIdle": "auto"},
	} {
		if r := f.call("projectApply", args); r["ok"] != true || r["field"] != nil {
			t.Errorf("%s : %v, attendu un enregistrement à chaud", name, r)
		}
	}
	cfg, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(cfg), "22") {
		t.Errorf("config.conf sans la couleur : %s", cfg)
	}
	// un refus de l'env dans la même demande n'écrit PAS les réglages à chaud
	mt := f.call("projectSettings", map[string]any{"path": f.proj})["envMtime"]
	before, _ := os.ReadFile(core.ConfigPath())
	r := f.call("projectApply", map[string]any{"path": f.proj, "color": "100", "env": envChange216(mt, "x")})
	if r["ok"] == true || r["field"] != "env" {
		t.Fatalf("demande mixte = %v", r)
	}
	if after, _ := os.ReadFile(core.ConfigPath()); string(after) != string(before) {
		t.Error("couleur écrite alors que l'env de la même demande est refusé (un refus n'écrit rien)")
	}
}
