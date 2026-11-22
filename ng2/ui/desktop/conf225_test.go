//go:build !windows

package desktop

// #225 : config.conf édité à la main (éditeur, autre launcher, git pull) est pris en compte SANS relancer le launcher —
// fenêtre native, appels web /api/call — et l'ouverture d'un projet recopie la NOUVELLE valeur dans settings.json (cas
// souris 0 → 1). Config invalide : dernière copie valide gardée et dite. CLAUDE_CONFIG_DIR temporaire (sandbox) : jamais le
// vrai settings.json. Projets factices, faux claude, tmux isolé.

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

const mouseVar225 = "CLAUDE_CODE_DISABLE_MOUSE"

// writeConf225 : config.conf avec la souris demandée, daté explicitement (jamais deux écritures « à la même date »).
func writeConf225(t *testing.T, content string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(core.ConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.ConfigPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(core.ConfigPath(), at, at); err != nil {
		t.Fatal(err)
	}
}

func mouseConf225(mouse string) string { return "CLAUDE_DISABLE_MOUSE=" + mouse + "\n" }

// settingsMouse225 : (valeur de la variable souris, settings.json existe) dans le settings.json TEMPORAIRE.
func settingsMouse225(t *testing.T) (string, bool) {
	t.Helper()
	p := filepath.Join(core.ClaudeDir(), "settings.json")
	if !strings.HasPrefix(p, os.TempDir()) {
		t.Fatalf("settings.json hors dossier temporaire : %s", p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	_ = json.Unmarshal(b, &doc)
	return doc.Env[mouseVar225], true
}

func waitFor225(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("%s : pas constaté en %v", what, within)
}

// Édition extérieure de config.conf → relue par la fenêtre SANS relance (conf() relit par date, au plus une fois par
// seconde), dans les deux sens ; la valeur qui n'a pas changé n'est pas « inventée ».
func TestEditionExterneRelueSansRelance225(t *testing.T) {
	sandbox(t, "", "alpha")
	t0 := time.Now().Add(-time.Hour)
	writeConf225(t, mouseConf225("0")+"TEAM_HIDE_IDLE=30\n", t0)
	var r recorder
	a := newApp(t, r.emit)
	if a.conf().DisableMouse || a.conf().TeamHideIdle != 30 {
		t.Fatalf("départ : souris=%v hideidle=%d", a.conf().DisableMouse, a.conf().TeamHideIdle)
	}
	writeConf225(t, mouseConf225("1")+"TEAM_HIDE_IDLE=45\n", t0.Add(10*time.Second))
	waitFor225(t, 4*time.Second, "édition extérieure relue sans relance", func() bool { return a.conf().DisableMouse })
	if a.conf().TeamHideIdle != 45 {
		t.Errorf("TEAM_HIDE_IDLE = %d, attendu 45 (config.conf relu en entier)", a.conf().TeamHideIdle)
	}
	writeConf225(t, mouseConf225("0")+"TEAM_HIDE_IDLE=45\n", t0.Add(20*time.Second))
	waitFor225(t, 4*time.Second, "retour de la souris à 0 relu", func() bool { return !a.conf().DisableMouse })
}

// L'ouverture d'un projet juste après l'édition extérieure (copie du launcher PÉRIMÉE, aucun appel conf() entre-temps)
// recopie la NOUVELLE valeur dans settings.json : souris 0 → 1 (cas utilisateur) et 1 → 0.
func TestOuvertureApresEditionExternePorteLaNouvelleValeur225(t *testing.T) {
	for name, c := range map[string]struct {
		avant, apres string
		wantVar      string
	}{"souris 0 vers 1": {"0", "1", "1"}, "souris 1 vers 0": {"1", "0", ""}} {
		t.Run(name, func(t *testing.T) {
			ps := sandbox(t, "", "alpha")
			noContainerBackend(t)
			fakeClaudeSession214(t)
			t0 := time.Now().Add(-time.Hour)
			writeConf225(t, mouseConf225(c.avant), t0)
			var r recorder
			a := newApp(t, r.emit)
			a.rescan()
			// laisse passer d'éventuelles recopies de démarrage, PUIS édite : la copie en mémoire est périmée
			time.Sleep(300 * time.Millisecond)
			writeConf225(t, mouseConf225(c.apres), t0.Add(10*time.Second))
			if res, err := a.open(ps[0], 0); err != nil || res.(map[string]any)["tab"] == nil {
				t.Fatalf("open : %v %v", res, err)
			}
			if !a.waitOpenings(20 * time.Second) {
				t.Fatal("l'ouverture ne finit pas")
			}
			waitFor225(t, 5*time.Second, "settings.json à jour après l'ouverture", func() bool {
				v, ok := settingsMouse225(t)
				return ok && v == c.wantVar
			})
		})
	}
}

// Config invalide : la dernière copie valide est gardée (souris inchangée en mémoire ET dans settings.json), le problème est
// dit en pied de volet ; corrigée, la nouvelle valeur est prise.
func TestConfigInvalideGardeLAncienneCopie225(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	noContainerBackend(t)
	fakeClaudeSession214(t)
	t0 := time.Now().Add(-time.Hour)
	writeConf225(t, mouseConf225("1"), t0)
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	bad := "CLAUDE_DISABLE_MOUSE=0\nif then ((( fi\n"
	if p := core.ValidateConfig(core.ConfConf, []byte(bad)); !core.HasErrors(p) {
		t.Skip("contenu jugé valide : choisir une autre erreur de syntaxe")
	}
	writeConf225(t, bad, t0.Add(10*time.Second))
	waitFor225(t, 4*time.Second, "config invalide signalée en pied de volet", func() bool {
		_ = a.conf() // la relecture par date part de là
		a.mu.Lock()
		defer a.mu.Unlock()
		return strings.Contains(a.msg, "invalide")
	})
	if !a.conf().DisableMouse {
		t.Error("config invalide : la dernière copie valide (souris 1) n'est plus utilisée")
	}
	if _, err := a.open(ps[0], 0); err != nil {
		t.Fatal(err)
	}
	if !a.waitOpenings(20 * time.Second) {
		t.Fatal("l'ouverture ne finit pas")
	}
	waitFor225(t, 5*time.Second, "settings.json garde la souris à 1", func() bool { v, ok := settingsMouse225(t); return ok && v == "1" })
	writeConf225(t, mouseConf225("0"), t0.Add(20*time.Second)) // corrigée
	waitFor225(t, 4*time.Second, "config corrigée relue", func() bool { return !a.conf().DisableMouse })
}

// Enregistrement depuis les réglages (configApply / configSave) : recopie dans settings.json AVEC la config relue — résultat
// `sync {ok:true}` — et jamais une valeur plus ancienne que le fichier (édition extérieure juste avant l'enregistrement).
func TestConfigApplyRecopieSettingsEtSync225(t *testing.T) {
	sandbox(t, "", "alpha")
	t0 := time.Now().Add(-time.Hour)
	writeConf225(t, mouseConf225("0"), t0)
	var r recorder
	a := newApp(t, r.emit)
	call := func(m string, args map[string]any) map[string]any {
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
	set := func(k string, v any) map[string]any {
		return map[string]any{"op": "set", "at": []string{k}, "value": v}
	}
	// édition extérieure (souris 1) puis enregistrement d'une AUTRE option : la souris du fichier doit être recopiée
	writeConf225(t, mouseConf225("1"), t0.Add(10*time.Second))
	m := call("configModel", map[string]any{"kind": "config.conf"})
	s := call("configApply", map[string]any{"kind": "config.conf", "mtime": m["mtime"], "changes": []any{set("TEAM_HIDE_IDLE", 41)}})
	if s["saved"] != true {
		t.Fatalf("configApply : %v", s)
	}
	if sync, _ := s["sync"].(map[string]any); sync == nil || sync["ok"] != true {
		t.Errorf("configApply.sync = %v, attendu {ok:true}", s["sync"])
	}
	if v, ok := settingsMouse225(t); !ok || v != "1" {
		t.Errorf("settings.json après configApply : souris=%q (existe=%v), attendu 1 (valeur du fichier édité à la main)", v, ok)
	}
	// éditeur YAML / texte : même recopie, même champ sync
	b, _ := os.ReadFile(core.ConfigPath())
	st, _ := os.Stat(core.ConfigPath())
	content := strings.Replace(string(b), "CLAUDE_DISABLE_MOUSE=1", "CLAUDE_DISABLE_MOUSE=0", 1)
	sv := call("configSave", map[string]any{"kind": "config.conf", "content": content, "mtime": st.ModTime().Format(time.RFC3339Nano)})
	if sv["saved"] != true {
		t.Fatalf("configSave : %v", sv)
	}
	if sync, _ := sv["sync"].(map[string]any); sync == nil || sync["ok"] != true {
		t.Errorf("configSave.sync = %v, attendu {ok:true}", sv["sync"])
	}
	if v, _ := settingsMouse225(t); v != "" {
		t.Errorf("settings.json après configSave (souris 0) : %q, attendu la variable retirée", v)
	}
}

// Appel forgé /api/call (mode --web) : mêmes effets que dans la fenêtre — configApply recopie dans settings.json et rend
// `sync` ; sans cookie : refusé, rien n'est écrit.
func TestWebConfigApplyRecopieSettings225(t *testing.T) {
	sandbox(t, "", "alpha")
	t0 := time.Now().Add(-time.Hour)
	writeConf225(t, mouseConf225("0"), t0)
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tok225", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	writeConf225(t, mouseConf225("1"), t0.Add(10*time.Second)) // édition extérieure, copie périmée
	st, _ := os.Stat(core.ConfigPath())
	body, _ := json.Marshal(map[string]any{"method": "configApply", "args": map[string]any{"kind": "config.conf", "mtime": st.ModTime().Format(time.RFC3339Nano),
		"changes": []any{map[string]any{"op": "set", "at": []string{"TEAM_HIDE_IDLE"}, "value": 42}}}})
	if resp, _ := doWith(t, srv.URL, "POST", "/api/call", "", "", string(body), "application/json", ""); resp.StatusCode == 200 {
		t.Error("configApply accepté sans cookie")
	}
	if _, ok := settingsMouse225(t); ok {
		t.Error("settings.json écrit par un appel refusé")
	}
	resp, out := doWith(t, srv.URL, "POST", "/api/call", "", "tok225", string(body), "application/json", "")
	var v map[string]any
	_ = json.Unmarshal([]byte(out), &v)
	if res, ok := v["result"].(map[string]any); ok {
		v = res
	}
	if resp.StatusCode != 200 || v["saved"] != true {
		t.Fatalf("configApply web : %d %s", resp.StatusCode, out)
	}
	if sync, _ := v["sync"].(map[string]any); sync == nil || sync["ok"] != true {
		t.Errorf("sync = %v, attendu {ok:true}", v["sync"])
	}
	if got, ok := settingsMouse225(t); !ok || got != "1" {
		t.Errorf("settings.json après configApply web : souris=%q, attendu 1", got)
	}
}
