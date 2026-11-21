//go:build !windows

package desktop

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

func TestIsDuplicateSession(t *testing.T) {
	for out, want := range map[string]bool{"duplicate session: p-alpha": true, "tmux: duplicate session: p-x\n": true, "no server running": false, "": false} {
		if isDuplicateSession(out) != want {
			t.Errorf("isDuplicateSession(%q) = %v", out, !want)
		}
	}
}

// Non-régression : plusieurs ouvertures SIMULTANÉES du même projet sur l'hôte (clic doublé, reprise après « ignorer ») —
// la course has-session → new-session ne produit plus « tmux : duplicate session » : tous se rattachent à UNE session.
func TestPrepareHostOuverturesSimultanees(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	fakeClaudeSession214(t) // socket tmux isolé, faux claude, kill-server en fin de test
	var r recorder
	a := newApp(t, r.emit)
	a.rescan()
	n, ok := a.node(ps[0])
	if !ok {
		t.Fatal("projet inconnu")
	}
	cfg := a.conf()
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		term := newProjTerm(a, n, 0)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- term.prepareHost(cfg, "true", "", false)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("ouverture simultanée en échec : %v", err)
		}
	}
	out, _ := exec.Command("tmux", "-L", deskSocket(), "list-sessions", "-F", "#{session_name}").Output()
	if got := strings.Fields(string(out)); len(got) != 1 || got[0] != hostSession(ps[0]) {
		t.Errorf("sessions = %v, attendu une seule (%s)", got, hostSession(ps[0]))
	}
}
