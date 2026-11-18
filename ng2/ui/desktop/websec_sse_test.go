package desktop

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Re-revue LOT-4 : un flux d'évènements est fermé quand sa session expire, et un gestionnaire bloqué
// dans Write sur une connexion figée compte dans le plafond jusqu'à sa vraie sortie.

// readUntilClosed : le corps du flux se termine (EOF) dans le délai.
func readUntilClosed(resp *http.Response, d time.Duration) bool {
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func openSSE(t *testing.T, base string, c *http.Cookie) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/api/events", nil)
	req.AddCookie(c)
	resp, err := webClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSSEClosedWhenSessionExpires(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokE1tokE1tokE1", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	c := scBootstrap(t, srv.URL, "tokE1tokE1tokE1")
	old := sessionIdle.Get()
	sessionIdle.Set(150 * time.Millisecond)
	defer func() { sessionIdle.Set(old) }()
	resp := openSSE(t, srv.URL, c)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("flux : %d", resp.StatusCode)
	}
	if !readUntilClosed(resp, 3*time.Second) {
		t.Fatal("le flux reste ouvert après l'expiration de sa session")
	}
}

func TestSSEClosedWhenSessionEvictedOrRevoked(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	w := &webBridge{app: a, token: "tokE2tokE2tokE2", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	c := scBootstrap(t, srv.URL, "tokE2tokE2tokE2")
	resp := openSSE(t, srv.URL, c)
	defer resp.Body.Close()
	w.setToken("autreTokenE2autreTokenE2") // révocation
	if !readUntilClosed(resp, 2*time.Second) {
		t.Fatal("le flux reste ouvert après la révocation de sa session")
	}
	// évincée : plus de maxSessions amorçages derrière elle
	c2 := scBootstrap(t, srv.URL, "autreTokenE2autreTokenE2")
	resp2 := openSSE(t, srv.URL, c2)
	defer resp2.Body.Close()
	for i := 0; i < maxSessions; i++ {
		scBootstrap(t, srv.URL, "autreTokenE2autreTokenE2")
	}
	if !readUntilClosed(resp2, 2*time.Second) {
		t.Fatal("le flux reste ouvert après l'éviction de sa session")
	}
}

// Connexion figée : le client ne lit pas ; le serveur finit bloqué dans Write. L'envoi a une échéance
// (sseWriteTimeout) et le gestionnaire reste compté dans le plafond tant qu'il n'est pas sorti, même si
// emit l'a retiré des abonnés (tampon plein).
func TestSSEFrozenConnectionHasWriteDeadlineAndCountsInCap(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	oldMax, oldTO := maxSubscribers.Get(), sseWriteTimeout.Get()
	maxSubscribers.Set(2)
	sseWriteTimeout.Set(1500 * time.Millisecond)
	defer func() { maxSubscribers.Set(oldMax); sseWriteTimeout.Set(oldTO) }()
	w := &webBridge{app: a, token: "tokE3tokE3tokE3", subs: map[chan []byte]bool{}, strictHost: true}
	srv := httptest.NewServer(w.handler())
	defer srv.Close()
	c := scBootstrap(t, srv.URL, "tokE3tokE3tokE3")
	// deux clients figés : connexion brute, requête envoyée, jamais lue
	var conns []net.Conn
	defer func() {
		for _, cn := range conns {
			cn.Close()
		}
	}()
	for i := 0; i < maxSubscribers.Get(); i++ {
		cn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, cn)
		req := "GET /api/events HTTP/1.1\r\nHost: " + strings.TrimPrefix(srv.URL, "http://") + "\r\nCookie: " + c.Name + "=" + c.Value + "\r\n\r\n"
		_, _ = cn.Write([]byte(req))
	}
	time.Sleep(200 * time.Millisecond)
	// inonder : gros évènements (remplissent les tampons réseau) puis petits (remplissent le canal) → abonnés retirés
	big := bytes.Repeat([]byte("x"), 256<<10)
	for i := 0; i < 80; i++ {
		w.emit("big", string(big))
	}
	for i := 0; i < 4300; i++ {
		w.emit("p", i)
	}
	w.subMu.Lock()
	left := len(w.subs)
	w.subMu.Unlock()
	if left != 0 {
		t.Skipf("abonnés non retirés (%d) : les tampons de ce système absorbent l'inondation", left)
	}
	// les gestionnaires sont encore bloqués dans Write : ils comptent toujours dans le plafond
	if resp, err := webClient.Do(func() *http.Request {
		req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
		req.AddCookie(c)
		return req
	}()); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("flux de plus pendant que deux gestionnaires sont bloqués : %d, attendu 503", resp.StatusCode)
		}
	}
	// après l'échéance d'écriture, ils sortent et la place est rendue
	end := time.Now().Add(6 * time.Second)
	for {
		req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
		req.AddCookie(c)
		resp, err := webClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		if err == nil {
			resp.Body.Close()
		}
		if time.Now().After(end) {
			t.Fatal("les gestionnaires bloqués ne sortent jamais : pas d'échéance d'écriture")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
