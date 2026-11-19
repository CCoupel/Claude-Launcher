package desktop

import (
	"os"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #209 (LOT-4) : masquage des teammates inactifs PAR PROJET (contracts/desktop-dispatch.md « v6.5.0 »).
// Par Dispatch : projectApply/projectSettings {hideIdle}, cards.teamHead.hideIdle {enabled, delay, mode, global}, teamToggle.

const hideRefus209 = "masquage désactivé pour ce projet : choisir « auto hide » dans le titre de la carte TEAM"

func hi209Settings(t *testing.T, a *App, path string) string {
	t.Helper()
	s := h188Call(t, a, "projectSettings", map[string]any{"path": path})
	v, _ := s["hideIdle"].(string)
	return v
}

func hi209Apply(t *testing.T, a *App, path, v string) map[string]any {
	t.Helper()
	return h188Call(t, a, "projectApply", map[string]any{"path": path, "hideIdle": v})
}

func hi209Conf(t *testing.T) string {
	b, _ := os.ReadFile(core.ConfigPath())
	return string(b)
}

// écriture / relecture : auto et none écrivent l'entrée, default la retire, absent = inchangé ; valeur inconnue : refus sans écriture.
func TestProjectApplyHideIdleEcritureRelecture209(t *testing.T) {
	a, _, path, _, _ := h188Project(t, "")
	if got := hi209Settings(t, a, path); got != "default" {
		t.Fatalf("hideIdle initial : %q, attendu default", got)
	}
	for _, v := range []string{"auto", "none"} {
		if res := hi209Apply(t, a, path, v); res["ok"] != true {
			t.Fatalf("projectApply hideIdle=%s : %v", v, res)
		}
		if got := hi209Settings(t, a, path); got != v {
			t.Errorf("après %s : projectSettings %q", v, got)
		}
		if c := hi209Conf(t); !strings.Contains(c, "PROJECT_HIDE_IDLE") || !strings.Contains(c, v) {
			t.Errorf("après %s : entrée absente de config.conf", v)
		}
	}
	// absent : inchangé
	h188Call(t, a, "projectApply", map[string]any{"path": path, "color": 1})
	if got := hi209Settings(t, a, path); got != "none" {
		t.Errorf("hideIdle absent de projectApply : valeur changée (%q)", got)
	}
	// valeur inconnue : refus AVANT toute écriture
	before := hi209Conf(t)
	if res := hi209Apply(t, a, path, "toujours"); res["ok"] != false || res["msg"] == "" {
		t.Errorf("valeur inconnue acceptée : %v", res)
	}
	if hi209Conf(t) != before {
		t.Error("config.conf modifié par un refus")
	}
	// default : retire l'entrée
	if res := hi209Apply(t, a, path, "default"); res["ok"] != true {
		t.Fatalf("default : %v", res)
	}
	if got := hi209Settings(t, a, path); got != "default" {
		t.Errorf("après default : %q", got)
	}
	if c := hi209Conf(t); strings.Contains(c, "[\"alpha\"]=\"none\"") {
		t.Error("l'entrée n'a pas été retirée")
	}
}

// priorité projet > global : auto actif même si global désactivé ; none inactif même si global activé ; default suit le global.
// Global changé : les projets auto / none restent intacts.
func TestHideIdlePrioriteProjetSurGlobal209(t *testing.T) {
	a, r, path, _, _ := h188Project(t, "")
	eff := func() map[string]any {
		var h map[string]any
		h188WaitCond(t, "cards teamHead.hideIdle", func() bool { h = h188HideIdle(r); return h != nil })
		return h
	}
	refresh := func() { h188Call(t, a, "select", map[string]any{"path": path}) }
	want := func(what string, enabled bool, mode string, global bool) {
		t.Helper()
		h188WaitCond(t, what, func() bool {
			refresh()
			h := eff()
			return h["enabled"] == enabled && h["mode"] == mode && h["global"] == global
		})
	}
	want("default, global activé", true, "default", true)
	h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", false)
	want("default suit le global désactivé", false, "default", false)
	hi209Apply(t, a, path, "auto")
	want("auto actif malgré global désactivé", true, "auto", false)
	h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", true)
	hi209Apply(t, a, path, "none")
	want("none inactif malgré global activé", false, "none", true)
	// global changé : none et auto intacts
	h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", false)
	want("none intact, global désactivé", false, "none", false)
	if got := hi209Settings(t, a, path); got != "none" {
		t.Errorf("le global a modifié PROJECT_HIDE_IDLE : %q", got)
	}
	hi209Apply(t, a, path, "auto")
	h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", true)
	want("auto intact, global activé", true, "auto", true)
	// délai 0 : désactive dans tous les cas, même en auto
	h188Set(t, a, "TEAM_HIDE_IDLE", 0)
	h188WaitCond(t, "délai 0 désactive même auto", func() bool { refresh(); h := eff(); return h["enabled"] == false })
}

// teamToggle refusé (message du contrat) quand le masquage effectif du projet est désactivé : mode none, ou default avec global
// désactivé ; accepté en auto même si le global est désactivé.
func TestTeamToggleRefuseEnNoHide209(t *testing.T) {
	a, _, path, mates, _ := h188Project(t, "")
	pt := a.term(path)
	h188Call(t, a, "select", map[string]any{"path": path})
	h188WaitCond(t, "team lue", func() bool { pt.mu.Lock(); defer pt.mu.Unlock(); return pt.team != nil && len(pt.vis) >= 3 })
	toggle := func() map[string]any {
		return h188Call(t, a, "teamToggle", map[string]any{"path": path, "pane": mates[1]})
	}
	hi209Apply(t, a, path, "none")
	time.Sleep(100 * time.Millisecond)
	if res := toggle(); res["ok"] != false || res["msg"] != hideRefus209 {
		t.Errorf("no hide : refus attendu %q, obtenu %v", hideRefus209, res)
	}
	hi209Apply(t, a, path, "default")
	h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", false)
	if res := toggle(); res["ok"] != false || res["msg"] != hideRefus209 {
		t.Errorf("default + global désactivé : refus attendu, obtenu %v", res)
	}
	hi209Apply(t, a, path, "auto")
	if res := toggle(); res["ok"] != true {
		t.Errorf("auto hide : bascule attendue même global désactivé, obtenu %v", res)
	}
}
