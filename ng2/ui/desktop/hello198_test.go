package desktop

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Dès l'ouverture du flux SSE, le serveur envoie `hello` { server: {version, boot} } (#198), avant tout autre évènement.
func TestFluxSSEEnvoieHello198(t *testing.T) {
	sandbox(t, "", "alpha")
	p := l4FreePort(t)
	l4WriteBackends(t, "  desktop_web: { listen: \"127.0.0.1:"+strconv.Itoa(p)+"\" }\n")
	var r recorder
	a := newApp(t, r.emit)
	a.Start()
	t.Cleanup(func() { stopNative(a) })
	l4WaitWebUI(t, a, "on", 8*time.Second)
	addr, tok := "127.0.0.1:"+strconv.Itoa(p), l4WebToken(t)

	req, _ := http.NewRequest("GET", "http://"+addr+"/api/events", nil)
	req.AddCookie(scBootstrap(t, "http://"+addr, tok))
	resp, err := webClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("flux : %v %v", resp, err)
	}
	defer resp.Body.Close()
	first := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				first <- l[6:]
				return
			}
		}
	}()
	select {
	case l := <-first:
		var e struct {
			Name string `json:"name"`
			Data struct {
				Server ServerInfo `json:"server"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(l), &e); err != nil || e.Name != "hello" || e.Data.Server != serverInfo() || e.Data.Server.Boot == "" {
			t.Errorf("premier évènement = %s", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("aucun évènement à l'ouverture du flux")
	}
}
