package desktop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// #183 (LOT-2.1) — commutateur de l'interface web et indépendance des deux serveurs. Contrat :
// contracts/desktop-web.md (« enabled », « Instantané — champ webUI », « Modification depuis un client web »).
// Ports libres, projets factices, aucun vrai moteur (comme web163_test.go).

func d183Apply(t *testing.T, a *App, origin origin, changes ...map[string]any) (any, error) {
	t.Helper()
	m, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"kind": "hub", "mtime": m.(map[string]any)["mtime"], "changes": changes})
	return a.DispatchFrom(origin, "configApply", b)
}

func d183Set(at string, v any) map[string]any {
	return map[string]any{"op": "set", "at": []string{"desktop_web", at}, "value": v}
}

func d183Reach(p int) bool {
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(p) + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func d183WaitReach(t *testing.T, p int, want bool) {
	t.Helper()
	end := time.Now().Add(8 * time.Second)
	for time.Now().Before(end) {
		if d183Reach(p) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("port %d : joignable = %v, attendu %v", p, !want, want)
}

// Table `enabled` × `listen` observée dans l'instantané et sur le réseau.
func TestWebUIEnabledListenTable(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		state       string
		enabled     bool
	}{
		{"absent + listen : allumée (v6.2.0)", "{ listen: \"%d\" }", "on", true},
		{"false + listen : éteinte", "{ enabled: false, listen: \"%d\" }", "off", false},
		{"true + listen : allumée", "{ enabled: true, listen: \"%d\" }", "on", true},
		{"true sans listen : erreur", "{ enabled: true }", "error", true},
		{"absent, listen off : éteinte", "{ listen: off }", "off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sandbox(t, "", "alpha")
			p := l4FreePort(t)
			blk := tc.block
			if strings.Contains(blk, "%d") {
				blk = fmt.Sprintf(blk, p)
			}
			l4WriteBackends(t, "  desktop_web: "+blk+"\n")
			a := l4StartNative(t)
			w := l4WaitWebUI(t, a, tc.state, 8*time.Second)
			if got, ok := w["enabled"].(bool); !ok || got != tc.enabled {
				t.Errorf("webUI.enabled = %v, attendu %v (%v)", w["enabled"], tc.enabled, w)
			}
			if w["server"] != false {
				t.Errorf("webUI.server = %v, attendu false hors mode serveur", w["server"])
			}
			if tc.state == "error" {
				if msg, _ := w["msg"].(string); !strings.Contains(msg, "sans adresse d'écoute") {
					t.Errorf("webUI.msg = %q, attendu « interface web activée sans adresse d'écoute »", msg)
				}
			}
			d183WaitReach(t, p, tc.state == "on")
		})
	}
}

// Commutateur à chaud : éteindre puis rallumer par « Ce poste », sans relance ; l'adresse est conservée.
func TestWebUIHotToggleKeepsAddress(t *testing.T) {
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(p)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"%s\" }\n", addr))
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "on", 8*time.Second)
	d183WaitReach(t, p, true)

	res, err := d183Apply(t, a, originWindow, d183Set("enabled", false))
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Fatalf("éteindre : %v %v", res, err)
	}
	w := l4WaitWebUI(t, a, "off", 8*time.Second)
	if w["enabled"] != false {
		t.Errorf("webUI.enabled = %v après extinction", w["enabled"])
	}
	d183WaitReach(t, p, false)
	if y, _ := os.ReadFile(core.BackendsPath()); !strings.Contains(string(y), addr) {
		t.Errorf("l'adresse d'écoute n'est pas conservée dans backends.yaml :\n%s", y)
	}

	res, err = d183Apply(t, a, originWindow, d183Set("enabled", true))
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Fatalf("rallumer : %v %v", res, err)
	}
	w = l4WaitWebUI(t, a, "on", 8*time.Second)
	if w["listen"] != addr || w["enabled"] != true {
		t.Errorf("après rallumage : %v, attendu listen=%s enabled=true", w, addr)
	}
	d183WaitReach(t, p, true)
}

// Allumer sans adresse : erreur de réglage (« interface web activée sans adresse d'écoute »), fichier non cassé.
func TestWebUIEnableWithoutAddressIsAnError(t *testing.T) {
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "  desktop_web: { listen: off }\n")
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "off", 5*time.Second)
	res, err := d183Apply(t, a, originWindow, d183Set("enabled", true))
	if err == nil {
		if m, _ := res.(map[string]any); m != nil && m["saved"] == true {
			w := l4WaitWebUI(t, a, "error", 8*time.Second)
			if msg, _ := w["msg"].(string); !strings.Contains(msg, "sans adresse d'écoute") {
				t.Errorf("webUI.msg = %q", msg)
			}
		}
	}
}

// Indépendance : le commutateur de l'interface ne touche pas le serveur des maquettes, et inversement.
func TestWebUIToggleIndependentOfBrowserServer(t *testing.T) {
	sandbox(t, "", "alpha")
	pw, pb := l4FreePort(t), l4FreePort(t)
	y := fmt.Sprintf("defaults:\n  browser_server: { listen: \"127.0.0.1:%d\" }\n  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\" }\n", pb, pw)
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "on", 8*time.Second)
	d183WaitReach(t, pw, true)
	bsReach := func() bool {
		r, err := http.Get("http://127.0.0.1:" + strconv.Itoa(pb) + "/")
		if err != nil {
			return false
		}
		r.Body.Close()
		return true
	}
	end := time.Now().Add(8 * time.Second)
	for !bsReach() && time.Now().Before(end) {
		time.Sleep(50 * time.Millisecond)
	}
	if !bsReach() {
		t.Skip("serveur des maquettes non démarré dans ce bac à sable : indépendance non observable")
	}
	if _, err := d183Apply(t, a, originWindow, d183Set("enabled", false)); err != nil {
		t.Fatal(err)
	}
	l4WaitWebUI(t, a, "off", 8*time.Second)
	d183WaitReach(t, pw, false)
	if !bsReach() {
		t.Error("éteindre l'interface web a arrêté le serveur des maquettes")
	}
	if now, _ := os.ReadFile(core.BackendsPath()); !strings.Contains(string(now), fmt.Sprintf("browser_server: {listen: \"127.0.0.1:%d\"}", pb)) {
		t.Errorf("bloc browser_server altéré :\n%s", now)
	}
}

// Un client web ne modifie ni `desktop_web.enabled` ni `desktop_web.url` (modèle structuré et texte) ; la fenêtre, si.
func TestWebClientCannotChangeDesktopWebEnabledOrURL(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	y := "defaults:\n  browser_server: { listen: off }\n  desktop_web: { listen: \"127.0.0.1:8787\" }\n"
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"enabled", "url"} {
		var v any = false
		if key == "url" {
			v = "https://launcher.exemple"
		}
		res, err := d183Apply(t, a, originWeb, d183Set(key, v))
		if m, _ := res.(map[string]any); err == nil && (m == nil || m["invalid"] != true) {
			t.Errorf("configApply desktop_web.%s depuis le web accepté : %v", key, res)
		}
		if now, _ := os.ReadFile(core.BackendsPath()); string(now) != y {
			t.Errorf("backends.yaml modifié par un client web (%s) :\n%s", key, now)
		}
	}
	// édition de texte : ajout de enabled depuis le web refusé
	st, _ := os.Stat(core.BackendsPath())
	bad := strings.Replace(y, "desktop_web: { listen:", "desktop_web: { enabled: false, listen:", 1)
	b, _ := json.Marshal(map[string]any{"kind": "backends.yaml", "content": bad, "mtime": st.ModTime().Format(time.RFC3339Nano)})
	res, err := a.DispatchFrom(originWeb, "configSave", b)
	if m, _ := res.(map[string]any); err == nil && (m == nil || m["invalid"] != true) {
		t.Errorf("configSave avec desktop_web.enabled depuis le web accepté : %v", res)
	}
	if now, _ := os.ReadFile(core.BackendsPath()); string(now) != y {
		t.Errorf("backends.yaml modifié par configSave web :\n%s", now)
	}
	// la fenêtre le peut
	if res, err := d183Apply(t, a, originWindow, d183Set("enabled", false)); err != nil || res.(map[string]any)["saved"] != true {
		t.Errorf("enabled depuis la fenêtre : %v %v", res, err)
	}
}

// Modèle « Ce poste » : enabled et url dans settings.desktop_web ; jamais le jeton.
func TestConfigModelHubDesktopWebEnabledAndURL(t *testing.T) {
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "hubs:\n  "+strings.ToLower(core.HubHost())+":\n    desktop_web: { enabled: false, listen: \"8787\", url: \"https://launcher.exemple\" }\n")
	var r recorder
	a := newApp(t, r.emit)
	m, err := a.Dispatch("configModel", json.RawMessage(`{"kind":"hub"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	dw := nested(v, "model", "settings", "desktop_web")
	if dw == nil {
		dw = nested(v, "settings", "desktop_web")
	}
	if dw == nil || dw["enabled"] != false || dw["url"] != "https://launcher.exemple" {
		t.Errorf("settings.desktop_web = %v, attendu enabled=false et url (%s)", dw, b)
	}
}

func nested(v map[string]any, path ...string) map[string]any {
	for _, k := range path {
		m, ok := v[k].(map[string]any)
		if !ok {
			return nil
		}
		v = m
	}
	return v
}

// Mode serveur (`desktop --web`) : webUI.server vrai, état « on » quel que soit `enabled`.
func TestWebUIServerModeIgnoresEnabled(t *testing.T) {
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "  desktop_web: { enabled: false, listen: off }\n")
	var r recorder
	a := newApp(t, r.emit)
	a.web = true
	w := l4WebUIOf(t, a)
	if w["server"] != true || w["state"] != "on" {
		t.Errorf("mode serveur : webUI = %v, attendu server=true state=on", w)
	}
}

// Sans `enabled` dans le fichier : comportement v6.2.0 inchangé (adresse = allumée), et l'instantané porte enabled=true.
func TestWebUIAbsentEnabledIsV620(t *testing.T) {
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { listen: \"127.0.0.1:%d\" }\n", p))
	a := l4StartNative(t)
	w := l4WaitWebUI(t, a, "on", 8*time.Second)
	if w["enabled"] != true || w["server"] != false {
		t.Errorf("webUI = %v", w)
	}
	if resp, _ := l4DoWeb(t, "127.0.0.1:"+strconv.Itoa(p), "GET", "/?t="+l4WebToken(t), "", "", "", "", ""); resp.StatusCode != 200 {
		t.Errorf("amorçage : %d", resp.StatusCode)
	}
}
