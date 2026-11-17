package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #188 (LOT-5) — commutateur du masquage des teammates inactifs : clé TEAM_HIDE_IDLE_ENABLED de config.conf (défaut activé),
// délai TEAM_HIDE_IDLE conservé, masquage désactivé = tout réapparaît + marques « caché à la main » effacées + double-clic
// de masquage refusé, état sur la carte TEAM (`cards.teamHead.hideIdle`). Contrats : tmux-options.md « Masquage
// désactivé », desktop-dispatch.md (#188). Écrit depuis les contrats (TDD). HOME factice, faux claude, tmux dédié.

func h188Call(t *testing.T, a *App, m string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(args)
	res, err := a.Dispatch(m, b)
	if err != nil {
		t.Fatalf("%s : %v", m, err)
	}
	out, _ := json.Marshal(res)
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	return v
}

func h188Setting(t *testing.T, a *App, key string) map[string]any {
	t.Helper()
	m := h188Call(t, a, "configModel", map[string]any{"kind": "config.conf"})
	for _, s := range m["model"].(map[string]any)["settings"].([]any) {
		if sm := s.(map[string]any); sm["key"] == key {
			return sm
		}
	}
	t.Fatalf("réglage %s absent du modèle de config.conf", key)
	return nil
}

func h188Set(t *testing.T, a *App, key string, v any) map[string]any {
	t.Helper()
	m := h188Call(t, a, "configModel", map[string]any{"kind": "config.conf"})
	return h188Call(t, a, "configApply", map[string]any{"kind": "config.conf", "mtime": m["mtime"], "changes": []any{
		map[string]any{"op": "set", "at": []string{key}, "value": v}}})
}

// Défaut = comportement de la v6.2.0 : clé absente → activé ; un fichier v6.2.0 (avec ou sans TEAM_HIDE_IDLE=0) est inchangé.
func TestHideIdleEnabledDefaultAndV62Files(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	s := h188Setting(t, a, "TEAM_HIDE_IDLE_ENABLED")
	if s["group"] != "teams" || s["type"] != "bool" || s["value"] != true || s["default"] != true || s["set"] != false || s["editable"] != true {
		t.Errorf("TEAM_HIDE_IDLE_ENABLED sans config.conf : %v, attendu {groupe teams, bool, valeur vraie, défaut vrai, non posée, éditable}", s)
	}
	for _, tc := range []struct {
		name, conf string
		delay      float64
	}{
		{"v6.2.0 délai 45", "TEAM_HIDE_IDLE=45\n", 45},
		{"v6.2.0 délai 0 (jamais)", "TEAM_HIDE_IDLE=0\n", 0},
		{"v6.2.0 sans clé", "GITHUB_DIRS=()\n", 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755)
			if err := os.WriteFile(core.ConfigPath(), []byte(tc.conf), 0o600); err != nil {
				t.Fatal(err)
			}
			var r2 recorder
			b := newApp(t, r2.emit)
			if e := h188Setting(t, b, "TEAM_HIDE_IDLE_ENABLED"); e["value"] != true || e["set"] != false {
				t.Errorf("activé attendu pour un fichier de la v6.2.0 : %v", e)
			}
			if d := h188Setting(t, b, "TEAM_HIDE_IDLE"); d["value"] != tc.delay {
				t.Errorf("délai %v, attendu %v (inchangé)", d["value"], tc.delay)
			}
		})
	}
}

// Désactiver garde le délai ; réactiver le retrouve ; la clé s'écrit « 0 »/« 1 » ; valeur invalide refusée ; relu à chaud.
func TestHideIdleDisableKeepsDelayAndIsReadHot(t *testing.T) {
	sandbox(t, "", "alpha")
	_ = os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755)
	_ = os.WriteFile(core.ConfigPath(), []byte("TEAM_HIDE_IDLE=45\n"), 0o600)
	var r recorder
	a := newApp(t, r.emit)
	if res := h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", false); res["saved"] != true {
		t.Fatalf("désactivation : %v", res)
	}
	b, _ := os.ReadFile(core.ConfigPath())
	if !strings.Contains(string(b), "TEAM_HIDE_IDLE_ENABLED=0") || !strings.Contains(string(b), "TEAM_HIDE_IDLE=45") {
		t.Errorf("config.conf : clé « =0 » et délai 45 conservé attendus :\n%s", b)
	}
	if s := h188Setting(t, a, "TEAM_HIDE_IDLE_ENABLED"); s["value"] != false || s["set"] != true {
		t.Errorf("après désactivation : %v", s)
	}
	if d := h188Setting(t, a, "TEAM_HIDE_IDLE"); d["value"] != 45.0 {
		t.Errorf("le délai n'est pas conservé : %v", d["value"])
	}
	if res := h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", true); res["saved"] != true {
		t.Fatalf("réactivation : %v", res)
	}
	if s := h188Setting(t, a, "TEAM_HIDE_IDLE_ENABLED"); s["value"] != true {
		t.Errorf("après réactivation : %v", s)
	}
	if d := h188Setting(t, a, "TEAM_HIDE_IDLE"); d["value"] != 45.0 {
		t.Errorf("délai après réactivation : %v, attendu 45 (sans le ressaisir)", d["value"])
	}
	// valeur invalide refusée, fichier inchangé
	before, _ := os.ReadFile(core.ConfigPath())
	res := h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", "peut-être")
	after, _ := os.ReadFile(core.ConfigPath())
	if res["saved"] == true || string(before) != string(after) {
		t.Errorf("valeur non booléenne acceptée : %v\n%s", res, after)
	}
}

// ── réglage appliqué à la règle de visibilité : tmux réel, faux claude ───────

func h188Project(t *testing.T) (a *App, r *recorder, path string, mates [2]string, tm func(args ...string) string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	ps := sandbox(t, "", "alpha")
	d, _ := os.MkdirTemp("", "cltmux")
	t.Setenv("TMUX_TMPDIR", d)
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	t.Setenv("TMUX", "")
	sock := "cl-desk-h188-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)
	t.Setenv("CL_DESK_SOCKET", sock)
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\necho faux claude prêt\nwhile read -r l; do echo \"reçu:$l\"; done\n"), 0o755)
	t.Setenv("CL_CLAUDE_BIN", fake)
	_ = core.WriteAssignment(ps[0], "host")
	r = &recorder{}
	a = newApp(t, r.emit)
	t.Cleanup(func() { a.Stop() })
	if _, err := a.open(ps[0], core.LaunchNormal); err != nil {
		t.Fatal(err)
	}
	pt := a.term(ps[0])
	pt.resize(120, 40)
	r.wait(t, 30*time.Second, "ready", isTerm(ps[0], "status", func(e map[string]any) bool { return e["state"] == "ready" }))
	r.wait(t, 15*time.Second, "layout", isTerm(ps[0], "layout", nil))
	tm = func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).Output()
		return strings.TrimSpace(string(out))
	}
	win := "=" + hostSession(ps[0]) + ":"
	for i := 0; i < 2; i++ {
		mates[i] = tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sh -c 'echo mate-"+strconv.Itoa(i)+"; exec cat'")
	}
	cfg, _ := json.Marshal(core.TeamConfig{Members: []core.TeamMember{
		{Name: "team-lead", Cwd: core.ClaudePath(ps[0])},
		{Name: "dev-a", AgentType: "dev-a", PaneID: mates[0], IsActive: true},
		{Name: "dev-b", AgentType: "dev-b", PaneID: mates[1]},
	}})
	td := filepath.Join(core.ClaudeDir(), "teams", "t1")
	_ = os.MkdirAll(td, 0o755)
	_ = os.WriteFile(filepath.Join(td, "config.json"), cfg, 0o644)
	r.wait(t, 30*time.Second, "3 panes", isTerm(ps[0], "layout", func(e map[string]any) bool {
		return len(e["layout"].(map[string]any)["panes"].([]any)) == 3
	}))
	return a, r, ps[0], mates, tm
}

func h188Panes(n int) func(map[string]any) bool {
	return func(e map[string]any) bool { return len(e["layout"].(map[string]any)["panes"].([]any)) == n }
}

// hideIdleOf : dernier cards.teamHead.hideIdle reçu pour le projet (nil : aucun).
func h188HideIdle(r *recorder) map[string]any {
	evs := r.all()
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i]["_name"] != "cards" {
			continue
		}
		if th, ok := evs[i]["teamHead"].(map[string]any); ok {
			if h, ok := th["hideIdle"].(map[string]any); ok {
				return h
			}
		}
	}
	return nil
}

func h188WaitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(45 * time.Second)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition non atteinte en 45 s : %s", what)
}

// Masquage désactivé : le teammate caché à la main réapparaît sans relance, sa marque « h » est effacée, le double-clic de
// masquage est refusé (message), la carte TEAM porte l'état ; réactivé : le délai conservé reprend.
func TestHideIdleDisabledShowsEverythingAndRefusesManualHide(t *testing.T) {
	a, r, path, mates, tm := h188Project(t)
	actif, calme := mates[0], mates[1]
	_ = actif
	pt := a.term(path)
	toggle := func(pane string) map[string]any {
		t.Helper()
		return h188Call(t, a, "teamToggle", map[string]any{"path": path, "pane": pane})
	}
	h188Call(t, a, "select", map[string]any{"path": path})
	// attendre que le surveillant ait lu la team et recensé les panes (condition, pas une durée)
	h188WaitCond(t, "team lue par le surveillant", func() bool {
		pt.mu.Lock()
		defer pt.mu.Unlock()
		return pt.team != nil && len(pt.vis) >= 3
	})
	// activé (défaut) : le double-clic cache l'inactif
	if res := toggle(calme); res["ok"] != true {
		t.Fatalf("bascule refusée alors que le masquage est activé : %v", res)
	}
	r.wait(t, 30*time.Second, "2 panes", isTerm(path, "layout", h188Panes(2)))
	if !strings.Contains(tm("show-option", "-wv", "-t", "="+hostSession(path)+":", "@cl_panevis"), calme+":h") {
		t.Errorf("la marque « %s:h » manque dans @cl_panevis après le masquage à la main", calme)
	}
	// désactivation à chaud (Réglages) : le teammate réapparaît, sans relance
	if res := h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", false); res["saved"] != true {
		t.Fatalf("désactivation : %v", res)
	}
	r.wait(t, 45*time.Second, "3 panes après désactivation", isTerm(path, "layout", h188Panes(3)))
	h188WaitCond(t, "@cl_panevis sans marque « h »", func() bool {
		return !strings.Contains(tm("show-option", "-wv", "-t", "="+hostSession(path)+":", "@cl_panevis"), ":h")
	})
	// le double-clic de masquage est refusé, rien n'est écrit
	res := toggle(calme)
	if res["ok"] != false || !strings.Contains(strings.ToLower(res["msg"].(string)), "masquage désactivé") {
		t.Errorf("bascule de masquage avec le masquage désactivé : %v, attendu un refus « masquage désactivé : l'activer dans les Réglages »", res)
	}
	if strings.Contains(tm("show-option", "-wv", "-t", "="+hostSession(path)+":", "@cl_panevis"), ":h") {
		t.Errorf("une marque « h » a été écrite malgré le refus")
	}
	// carte TEAM : état du masquage
	h188Call(t, a, "select", map[string]any{"path": path})
	h188WaitCond(t, "cards.teamHead.hideIdle.enabled = false", func() bool {
		h := h188HideIdle(r)
		return h != nil && h["enabled"] == false
	})
	if h := h188HideIdle(r); h["delay"] == nil {
		t.Errorf("hideIdle sans délai : %v (le délai est conservé même désactivé)", h)
	}
	for _, row := range a.readTeam(path) {
		if row.Hidden {
			t.Errorf("ligne cachée malgré le masquage désactivé : %+v", row)
		}
	}
	// réactivation : le délai (court) reprend, l'inactif est caché automatiquement
	if res := h188Set(t, a, "TEAM_HIDE_IDLE", 2); res["saved"] != true {
		t.Fatalf("délai : %v", res)
	}
	if res := h188Set(t, a, "TEAM_HIDE_IDLE_ENABLED", true); res["saved"] != true {
		t.Fatalf("réactivation : %v", res)
	}
	r.wait(t, 60*time.Second, "2 panes après réactivation (masquage automatique)", isTerm(path, "layout", h188Panes(2)))
	h188Call(t, a, "select", map[string]any{"path": path})
	h188WaitCond(t, "cards.teamHead.hideIdle = {enabled: true, delay: 2}", func() bool {
		h := h188HideIdle(r)
		return h != nil && h["enabled"] == true && h["delay"] == 2.0
	})
}

// Masquage désactivé AU DÉMARRAGE : un inactif n'est jamais caché automatiquement, même après le délai.
func TestHideIdleDisabledAtStartNeverHidesAutomatically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	sandbox(t, "", "alpha")
	_ = os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755)
	_ = os.WriteFile(core.ConfigPath(), []byte("TEAM_HIDE_IDLE=1\nTEAM_HIDE_IDLE_ENABLED=0\n"), 0o600)
	a, r, path, mates, _ := h188Project(t)
	_ = mates
	// plusieurs tours du surveillant après le délai de 1 s : toujours 3 panes
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if l := a.term(path).lastLayout(); l == nil || len(l.Panes) != 3 {
			t.Fatalf("un teammate inactif a été caché malgré TEAM_HIDE_IDLE_ENABLED=0 : %+v", l)
		}
	}
	_ = r
}

// État par défaut visible sur la carte : activé avec le délai.
func TestHideIdleCardShowsEnabledStateByDefault(t *testing.T) {
	a, r, path, _, _ := h188Project(t)
	h188Call(t, a, "select", map[string]any{"path": path})
	h188WaitCond(t, "cards.teamHead.hideIdle présent", func() bool { return h188HideIdle(r) != nil })
	if h := h188HideIdle(r); h["enabled"] != true || h["delay"] != 30.0 {
		t.Errorf("hideIdle par défaut : %v, attendu {enabled: true, delay: 30}", h)
	}
}

// Témoin du test précédent : même montage SANS la désactivation — l'inactif est bien caché automatiquement après le délai
// (sinon « jamais caché » ne prouverait rien).
func TestHideIdleControlEnabledShortDelayHidesAutomatically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pas de tmux sous Windows")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	sandbox(t, "", "alpha")
	_ = os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755)
	_ = os.WriteFile(core.ConfigPath(), []byte("TEAM_HIDE_IDLE=1\n"), 0o600)
	_, r, path, _, _ := h188Project(t)
	r.wait(t, 60*time.Second, "2 panes : inactif caché après le délai", isTerm(path, "layout", h188Panes(2)))
}
