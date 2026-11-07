package main

import (
	"strings"
	"testing"
	"time"
)

func TestSessionOf(t *testing.T) {
	me := ownerInfo{id: launcherID(), at: time.Now()}
	other := ownerInfo{id: "srv1:1000:x", at: time.Now()}
	gone := ownerInfo{id: "srv1:1000:x", at: time.Now().Add(-time.Hour)}
	cases := []struct {
		name string
		p    projInfo
		want sessState
		why  string
	}{
		{"rien", projInfo{}, sessNone, "non démarré"},
		{"fenêtre ici", projInfo{open: true, hasSess: true}, sessAttached, "ouverte dans ce hub"},
		{"conteneur vide", projInfo{ctrKnown: true, ctr: Found{Running: true}}, sessNone, "sans session"},
		{"session à moi", projInfo{hasLive: true, live: liveSess{owner: me}}, sessDetached, "rattacher"},
		{"pilotée ailleurs", projInfo{hasLive: true, live: liveSess{owner: other}}, sessDetached, "pilotée par srv1"},
		{"propriétaire parti", projInfo{hasLive: true, live: liveSess{owner: gone}}, sessDetached, "rattacher"},
		{"reprise", projInfo{hasSess: true}, sessAnomaly, "↺"},
		{"backend ⚠", projInfo{backend: "k8s", problems: []problem{{"x", "y"}}}, sessAnomaly, "inutilisable"},
		{"config invalide", projInfo{badCfg: "x"}, sessAnomaly, "invalide"},
		{"pod en échec", projInfo{ctrKnown: true, ctr: Found{Failed: true}}, sessAnomaly, "échec"},
	}
	for _, c := range cases {
		st, why := sessionOf(c.p)
		if st != c.want || !strings.Contains(why, c.why) {
			t.Errorf("%s : %v %q, attendu %v ~%q", c.name, st, why, c.want, c.why)
		}
	}
	// légende : le point vient de la sélection quand il est connu
	if i := dotIndicator(projInfo{dot: sessDetached, dotWhy: "w"}); i.icon != "◐" || i.text != "w" {
		t.Errorf("dotIndicator : %+v", i)
	}
	if i := dotIndicator(projInfo{open: true}); i.icon != "●" {
		t.Errorf("dotIndicator sans état : %+v", i)
	}
}

// fakeTmux : session simulée, le leader (%3) quitte claude après /exit.
type fakeTmux struct {
	cmd   map[string]string
	sent  []string
	exits bool
	kill  bool
}

func (f *fakeTmux) run(args ...string) (string, error) {
	switch args[0] {
	case "list-panes":
		var l []string
		for _, id := range []string{"%7", "%3", "%12"} {
			l = append(l, id+" "+f.cmd[id])
		}
		return strings.Join(l, "\n"), nil
	case "send-keys":
		k := args[len(args)-1]
		f.sent = append(f.sent, k)
		if k == "Enter" && f.exits && len(f.sent) >= 2 && f.sent[len(f.sent)-2] == "/exit" {
			f.cmd["%3"] = "bash"
		}
	case "display-message":
		return f.cmd[args[3]], nil
	case "kill-session":
		f.kill = true
	}
	return "", nil
}

func TestStopSession(t *testing.T) {
	old := closeWait
	closeWait = 1500 * time.Millisecond
	defer func() { closeWait = old }()

	f := &fakeTmux{cmd: map[string]string{"%3": "claude", "%7": "claude", "%12": "claude"}, exits: true}
	var steps []string
	_ = stopSession(f.run, "="+ctrSession+":", true, func(s string) { steps = append(steps, s) })
	if strings.Join(f.sent, ",") != "Escape,/exit,Enter" {
		t.Errorf("touches : %v", f.sent)
	}
	if !f.kill || len(steps) != 2 || !strings.HasPrefix(steps[1], "sortie propre") {
		t.Errorf("sortie propre attendue : kill=%v %v", f.kill, steps)
	}

	// claude ne répond pas : arrêt forcé après closeWait
	f = &fakeTmux{cmd: map[string]string{"%3": "claude", "%7": "x", "%12": "x"}}
	steps = nil
	_ = stopSession(f.run, "="+ctrSession+":", true, func(s string) { steps = append(steps, s) })
	if !f.kill || len(steps) != 2 || !strings.Contains(steps[1], "arrêt forcé") {
		t.Errorf("arrêt forcé attendu : kill=%v %v", f.kill, steps)
	}

	// claude déjà sorti (shell) : rien d'envoyé ; fenêtre du hub : pas de kill-session
	f = &fakeTmux{cmd: map[string]string{"%3": "bash", "%7": "x", "%12": "x"}}
	_ = stopSession(f.run, "=claude-hub-ng2:=p", false, func(string) {})
	if len(f.sent) != 0 || f.kill {
		t.Errorf("shell : %v kill=%v", f.sent, f.kill)
	}
}

func TestPlanCloseRefus(t *testing.T) {
	n := Node{Name: "p", Path: "/x/p", Key: "p", Project: true}
	m := model{windows: map[string]bool{}, git: map[string]GitInfo{}, tpl: map[string]TemplateState{}}
	m.ctr = ctrView{live: map[string]liveSess{n.Path: {owner: ownerInfo{id: "srv1:1:x", at: time.Now()}, panes: 3}}}
	if _, why := m.planClose(n); !strings.Contains(why, "piloté par srv1") || !strings.Contains(why, "prendre la main") {
		t.Errorf("piloté ailleurs : %q", why)
	}
	m.ctr = ctrView{}
	if _, why := m.planClose(n); !strings.Contains(why, "rien à fermer") {
		t.Errorf("non démarré : %q", why)
	}
	m.closing = map[string]string{n.Path: "x"}
	if _, why := m.planClose(n); !strings.Contains(why, "déjà en cours") {
		t.Errorf("en cours : %q", why)
	}
	// fenêtre ouverte (hôte) : question, sans arrêt de conteneur
	m.closing = nil
	m.windows[n.Key] = true
	pl, why := m.planClose(n)
	if why != "" || !strings.HasPrefix(pl.question, "Fermer p ? claude") || strings.Contains(pl.question, "conteneur") || !pl.window || !pl.session {
		t.Errorf("hôte : %q %+v", why, pl)
	}
	// session interrompue : abandon de la reprise
	m.windows[n.Key] = false
	m.sessions = map[string]Resumable{n.Path: {}}
	if pl, why := m.planClose(n); why != "" || !pl.abandon || !strings.HasPrefix(pl.question, "Abandonner p ?") {
		t.Errorf("reprise : %q %+v", why, pl)
	}
}
