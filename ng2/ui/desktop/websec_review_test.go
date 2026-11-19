package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Corrections de la revue LOT-4 (#163).

// 1. browser_server est protégé comme desktop_web : un client web ne l'ouvre pas sur le réseau.
func TestWebClientCannotChangeBrowserServer(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	y := "defaults:\n  browser_server: { listen: \"127.0.0.1:8765\" }\n  desktop_web: { listen: off }\n"
	if err := os.WriteFile(core.BackendsPath(), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := func() string { st, _ := os.Stat(core.BackendsPath()); return st.ModTime().Format(time.RFC3339Nano) }
	args := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	notSaved := func(res any, err error) bool {
		if err != nil {
			return true
		}
		m, _ := res.(map[string]any)
		return m["invalid"] == true
	}
	bad := strings.Replace(y, "127.0.0.1:8765", "0.0.0.0:8765", 1)
	res, err := a.DispatchFrom(originWeb, "configSave", args(map[string]any{"kind": "backends.yaml", "content": bad, "mtime": mt()}))
	if !notSaved(res, err) {
		t.Errorf("configSave de browser_server depuis le web : %v %v", res, err)
	}
	res, err = a.DispatchFrom(originWeb, "configApply", args(map[string]any{"kind": "hub", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"browser_server", "listen"}, "value": "0.0.0.0:8766"}}}))
	if !notSaved(res, err) {
		t.Errorf("configApply browser_server (machine) depuis le web : %v %v", res, err)
	}
	res, err = a.DispatchFrom(originWeb, "configApply", args(map[string]any{"kind": "hub", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"windows", "browser_server", "allow"}, "value": []string{"/"}}}}))
	if !notSaved(res, err) {
		t.Errorf("configApply browser_server (système) depuis le web : %v %v", res, err)
	}
	if now, _ := os.ReadFile(core.BackendsPath()); string(now) != y {
		t.Errorf("backends.yaml modifié par un client web : %s", now)
	}
	// la fenêtre le peut
	res, err = a.DispatchFrom(originWindow, "configApply", args(map[string]any{"kind": "hub", "mtime": mt(), "changes": []map[string]any{
		{"op": "set", "at": []string{"browser_server", "listen"}, "value": "127.0.0.1:8770"}}}))
	if err != nil || res.(map[string]any)["saved"] != true {
		t.Errorf("browser_server depuis la fenêtre : %v %v", res, err)
	}
}

// 2. Sessions bornées (le plus ancien évincé) et expirées après inactivité.
func TestWebSessionsAreBoundedAndExpire(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokS1tokS1tokS1", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	call := `{"method":"refresh","args":{}}`
	first := scBootstrap(t, srv.URL, "tokS1tokS1tokS1")
	var last *http.Cookie
	for i := 0; i < maxSessions+5; i++ {
		last = scBootstrap(t, srv.URL, "tokS1tokS1tokS1")
	}
	w.mu.Lock()
	n := len(w.sessions)
	w.mu.Unlock()
	if n > maxSessions {
		t.Errorf("%d sessions en mémoire, plafond %d", n, maxSessions)
	}
	if st := scCall(t, srv.URL, first, call); st != 403 {
		t.Errorf("la plus ancienne session n'a pas été évincée : %d", st)
	}
	if st := scCall(t, srv.URL, last, call); st != 200 {
		t.Errorf("la dernière session : %d", st)
	}
	// expiration après inactivité
	old := sessionIdle.Get()
	sessionIdle.Set(150 * time.Millisecond)
	defer func() { sessionIdle.Set(old) }()
	c := scBootstrap(t, srv.URL, "tokS1tokS1tokS1")
	time.Sleep(300 * time.Millisecond)
	if st := scCall(t, srv.URL, c, call); st != 403 {
		t.Errorf("session inactive non expirée : %d", st)
	}
}

// 3. Un abonné SSE lent ne bloque ni l'émission, ni l'authentification, ni les autres abonnés.
func TestSlowSubscriberDoesNotBlockAuthOrEmit(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokS3tokS3tokS3", subs: map[chan []byte]bool{}, strictHost: true}
	slow := make(chan []byte) // sans tampon et jamais lu : bloquerait l'émission
	fast := make(chan []byte, 8)
	w.subs[slow], w.subs[fast] = true, true
	done := make(chan struct{})
	go func() { w.emit("x", 1); close(done) }()
	authDone := make(chan bool, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		req := httptest.NewRequest("GET", "/?t=tokS3tokS3tokS3", nil)
		authDone <- w.authorized(req, true)
	}()
	select {
	case ok := <-authDone:
		if !ok {
			t.Error("authentification refusée")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("l'authentification attend l'émission vers un abonné lent")
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("l'émission attend un abonné lent")
	}
	select {
	case <-fast:
	default:
		t.Error("l'abonné rapide n'a pas reçu l'évènement")
	}
}

// 4. Messages : l'IP n'est pas « refusée » dans tous les cas ; --addr LAN sans allow_lan est signalé.
func TestLANMessagesAndAddrNotice(t *testing.T) {
	if w := core.LANWarning("0.0.0.0:8770"); strings.Contains(w, "(l'IP est refusée)") {
		t.Errorf("message inexact : %s", w)
	}
	if n := addrLANNotice("0.0.0.0:8770", false); !strings.Contains(n, "allow_lan") || !strings.Contains(n, "--addr") {
		t.Errorf("--addr LAN sans allow_lan : %q, attendu un avertissement citant allow_lan", n)
	}
	if n := addrLANNotice("0.0.0.0:8770", true); n != "" {
		t.Errorf("--addr LAN avec allow_lan : %q, attendu aucun avertissement supplémentaire", n)
	}
	if n := addrLANNotice("127.0.0.1:8770", false); n != "" {
		t.Errorf("boucle locale : %q", n)
	}
}

// 5. Jeton : la régénération est réécrite sur disque ; le changement d'adresse entre deux lancements
// renouvelle le jeton ; un échec d'écriture est dit (l'ancien pourrait servir à la relance).
func TestTokenRotationIsPersisted(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	p1 := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p1)+"\" }\n")
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 5*time.Second)
	tok1, _ := desktopWebToken()
	if res := a.webUIRegen(); res["ok"] != true {
		t.Fatalf("webUIRegen : %v", res)
	}
	disk, _ := os.ReadFile(desktopWebTokenPath())
	tok2 := strings.TrimSpace(string(disk))
	a.webui.mu.Lock()
	mem := a.webui.bridge.tokenNow()
	a.webui.mu.Unlock()
	if tok2 == tok1 || tok2 != mem {
		t.Errorf("régénération non persistée : disque %q, mémoire %q, ancien %q", tok2, mem, tok1)
	}
	a.stopWebUI()
	// « autre lancement » (nouvelle App) sur une autre adresse : jeton renouvelé
	p2 := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p2)+"\" }\n")
	var r2 recorder
	b := newApp(t, r2.emit)
	b.applyWebUI()
	l4WaitWebUI(t, b, "on", 5*time.Second)
	if tok3, _ := desktopWebToken(); tok3 == tok2 {
		t.Error("jeton conservé alors que l'adresse a changé entre deux lancements")
	}
	b.stopWebUI()
}

func TestTokenRotationWriteFailureIsReported(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	p := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+itoa(p)+"\" }\n")
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 5*time.Second)
	// échec d'écriture, portable (pas de droits de dossier, sans effet sous Windows) : un dossier non vide
	// à la place du fichier du jeton — ni le renommage ni la suppression de l'ancien ne peuvent aboutir
	tp := desktopWebTokenPath()
	if err := os.Remove(tp); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tp, "occupe"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := a.webUIRegen()
	if m, _ := res["msg"].(string); res["ok"] != true || !strings.Contains(m, "jeton") {
		t.Errorf("échec d'écriture non dit : %v", res)
	}
}
