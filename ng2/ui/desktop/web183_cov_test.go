package desktop

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Couverture LOT-2.1 (#183) : RunWeb (mode serveur), webUIOpen et openURL. Aucun navigateur n'est lancé :
// un faux xdg-open placé en tête du PATH note ses arguments (Linux) ; aucun vrai projet.

// d183FakeOpener : faux lanceur de navigateur (xdg-open) ; rend le fichier où il note l'adresse reçue.
func d183FakeOpener(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("faux xdg-open : Linux seulement")
	}
	dir, out := t.TempDir(), filepath.Join(t.TempDir(), "opened")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return out
}

func d183WaitOpened(t *testing.T, file string) string {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(file); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return ""
}

// openURL choisit la commande du système (xdg-open sous Linux) avec l'adresse en seul argument, sans attendre.
func TestOpenURLUsesSystemCommand(t *testing.T) {
	out := d183FakeOpener(t)
	openURL("http://127.0.0.1:1/?t=abc")
	if got := d183WaitOpened(t, out); got != "http://127.0.0.1:1/?t=abc" {
		t.Errorf("xdg-open a reçu %q", got)
	}
}

// webUIOpen : désactivée → refus lisible, aucun navigateur ; allumée → adresse d'amorçage avec le jeton du serveur ;
// écoute sur toutes les interfaces → « localhost » ; client web → refusé.
func TestWebUIOpenNominalDisabledAndWebClient(t *testing.T) {
	out := d183FakeOpener(t)
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "  desktop_web: { listen: off }\n")
	a := l4StartNative(t)
	l4WaitWebUI(t, a, "off", 5*time.Second)
	res, err := a.Dispatch("webUIOpen", nil)
	if m, _ := res.(map[string]any); err != nil || m["ok"] != false || !strings.Contains(m["msg"].(string), "désactivée") {
		t.Errorf("interface éteinte : %v %v, attendu ok=false et message", res, err)
	}
	time.Sleep(150 * time.Millisecond)
	if b, _ := os.ReadFile(out); len(b) != 0 {
		t.Errorf("un navigateur a été lancé alors que l'interface est éteinte : %q", b)
	}

	p := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { enabled: true, listen: \"127.0.0.1:"+itoa(p)+"\" }\n")
	a.applyWebUI() // relecture du réglage à chaud
	l4WaitWebUI(t, a, "on", 8*time.Second)
	res, err = a.Dispatch("webUIOpen", nil)
	if m, _ := res.(map[string]any); err != nil || m["ok"] != true {
		t.Fatalf("interface allumée : %v %v", res, err)
	}
	want := "http://127.0.0.1:" + itoa(p) + "/?t=" + l4WebToken(t)
	if got := d183WaitOpened(t, out); got != want {
		t.Errorf("adresse ouverte %q, attendu %q", got, want)
	}
	if _, err := a.DispatchFrom(originWeb, "webUIOpen", nil); err == nil {
		t.Error("webUIOpen depuis un client web accepté")
	}
}

var d183URLRe = regexp.MustCompile(`http://(127\.0\.0\.1:\d+)/\?t=(\S+)`)

// RunWeb (« desktop --web ») : démarre sur un port libre, annonce l'adresse, sert l'interface, expose
// webUI.server=true / state=on / enabled=true, puis s'arrête proprement sur « quit ».
func TestRunWebServerModeStartsAndQuits(t *testing.T) {
	sandbox(t, "", "alpha")
	l4WriteBackends(t, "  desktop_web: { enabled: false, listen: off }\n")
	t.Setenv("CL_DESKTOP_TOKEN", "tokRunWebServerMode123456")
	oldQuit, oldRestart, oldEnv := quitHook, restartApp, restartEnv
	defer func() { quitHook, restartApp, restartEnv = oldQuit, oldRestart, oldEnv }()

	// la sortie standard de RunWeb porte l'adresse d'amorçage
	pr, pw, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = pw
	urlc := make(chan [2]string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		sent := false
		for sc.Scan() {
			if m := d183URLRe.FindStringSubmatch(sc.Text()); m != nil && !sent {
				urlc <- [2]string{m[1], m[2]}
				sent = true
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- RunWeb("127.0.0.1:0", false) }()
	var addr, tok string
	select {
	case v := <-urlc:
		addr, tok = v[0], v[1]
	case <-time.After(15 * time.Second):
		t.Fatal("RunWeb n'a pas annoncé d'adresse")
	}
	// os.Stdout reste remplacé jusqu'à la FIN de RunWeb (il écrit encore après l'adresse : « Ctrl+C pour arrêter ») :
	// restauré plus bas, une fois la goroutine terminée (sinon course de données avec fmt.Println)
	defer func() { os.Stdout = oldOut }()
	if tok != "tokRunWebServerMode123456" {
		t.Errorf("jeton annoncé %q, attendu celui de CL_DESKTOP_TOKEN", tok)
	}
	base := "http://" + addr
	c := scBootstrap(t, base, tok)
	call := func(body string) map[string]any {
		req, _ := http.NewRequest("POST", base+"/api/call", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(c)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		return v
	}
	snap := call(`{"method":"init","args":{}}`)
	w, _ := d183Find(snap, "webUI").(map[string]any)
	if w == nil || w["server"] != true || w["state"] != "on" || w["enabled"] != true || w["listen"] != addr {
		t.Errorf("webUI en mode serveur = %v, attendu server=true state=on enabled=true listen=%s ", w, addr)
	}
	// arrêt propre par « quit » (permis dans « desktop --web »)
	call(`{"method":"quit","args":{"mode":"l"}}`)
	select {
	case err := <-done:
		os.Stdout = oldOut
		_ = pw.Close()
		if err != nil {
			t.Errorf("RunWeb : %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunWeb ne s'arrête pas après quit")
	}
	time.Sleep(300 * time.Millisecond) // tâches de fond (trace de performance) avant l'effacement des dossiers
}

// d183Find : première valeur de la clé key dans un JSON décodé (l'instantané peut être emballé).
func d183Find(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if r, ok := x[key]; ok {
			return r
		}
		for _, c := range x {
			if r := d183Find(c, key); r != nil {
				return r
			}
		}
	case []any:
		for _, c := range x {
			if r := d183Find(c, key); r != nil {
				return r
			}
		}
	}
	return nil
}
