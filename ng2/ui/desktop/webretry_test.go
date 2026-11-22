package desktop

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func busyPort(t *testing.T) (int, net.Listener) {
	t.Helper()
	p := l4FreePort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		t.Skipf("port %d pris : %v", p, err)
	}
	return p, ln
}

func webMsg(t *testing.T, a *App) (state, msg string) {
	w := l4WebUIOf(t, a)
	s, _ := w["state"].(string)
	m, _ := w["msg"].(string)
	return s, m
}

// Port pris entre l'arrêt et la ré-écoute : réessai borné avec backoff, état visible (« nouvel essai n/N »), puis l'interface
// web revient dès que le port se libère.
func TestEcouteWebReessaieQuandLePortSeLibere(t *testing.T) {
	old := webListenRetryDelays.Get()
	webListenRetryDelays.Set([]time.Duration{60 * time.Millisecond, 60 * time.Millisecond, 60 * time.Millisecond, 60 * time.Millisecond, 60 * time.Millisecond})
	defer webListenRetryDelays.Set(old)
	sandbox(t, "", "alpha")
	p, ln := busyPort(t)
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\" }\n", p))
	a := l4StartNative(t)
	end := time.Now().Add(10 * time.Second)
	var msg string
	for time.Now().Before(end) {
		var st string
		if st, msg = webMsg(t, a); st == "error" && strings.Contains(msg, "nouvel essai") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(msg, "nouvel essai") || !strings.Contains(msg, "écoute impossible") {
		t.Fatalf("état visible attendu avec « nouvel essai n/N » : %q", msg)
	}
	ln.Close() // le port se libère entre deux essais
	l4WaitWebUI(t, a, "on", 10*time.Second)
	if st, m := webMsg(t, a); st != "on" || strings.Contains(m, "nouvel essai") {
		t.Errorf("après libération : state=%s msg=%q", st, m)
	}
	a.webui.mu.Lock()
	r, tm := a.webui.retries, a.webui.retryT
	a.webui.mu.Unlock()
	_ = tm
	if r != 0 {
		t.Errorf("compteur d'essais non remis à zéro : %d", r)
	}
}

// Port occupé durablement : les essais sont BORNÉS ; après le dernier, « abandon » et plus aucun réessai.
func TestEcouteWebAbandonneApresLesEssais(t *testing.T) {
	old := webListenRetryDelays.Get()
	webListenRetryDelays.Set([]time.Duration{30 * time.Millisecond, 30 * time.Millisecond, 30 * time.Millisecond})
	defer webListenRetryDelays.Set(old)
	sandbox(t, "", "alpha")
	p, ln := busyPort(t)
	defer ln.Close()
	l4WriteBackends(t, fmt.Sprintf("  desktop_web: { enabled: true, listen: \"127.0.0.1:%d\" }\n", p))
	a := l4StartNative(t)
	end := time.Now().Add(10 * time.Second)
	var msg string
	for time.Now().Before(end) {
		if _, msg = webMsg(t, a); strings.Contains(msg, "abandon après 3 essais") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(msg, "abandon après 3 essais") {
		t.Fatalf("abandon attendu : %q", msg)
	}
	time.Sleep(300 * time.Millisecond) // plus aucun réessai
	a.webui.mu.Lock()
	r, tm := a.webui.retries, a.webui.retryT
	a.webui.mu.Unlock()
	if r != 3 {
		t.Errorf("essais = %d, attendu 3 (bornés)", r)
	}
	if tm != nil && tm.Stop() {
		t.Error("un réessai est encore programmé après l'abandon")
	}
	if st, _ := webMsg(t, a); st != "error" {
		t.Errorf("état = %s, error attendu", st)
	}
	// réenregistrer le réglage (autre appel que le minuteur) remet le compteur à zéro et réessaie
	ln.Close()
	a.applyWebUI()
	l4WaitWebUI(t, a, "on", 10*time.Second)
}
