package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #221 (LOT-1 v6.7.0) — arrêt de l'interface : plus aucun terminal, goroutine de terminal ni processus enfant après
// Stop, en 25 s au plus, terminal bloqué compris. Contrat : contracts/desktop-dispatch.md § « v6.7.0 » (Arrêt de
// l'interface). Faux claude, tmux isolé (openFakeProject) : jamais un vrai projet.

// stopBudget : échéance unique de App.Stop (contrat).
const stopBudget = 25 * time.Second

// childClients : processus enfants directs du processus de test qui sont des clients tmux en mode contrôle (« tmux … -C »).
func childClients(t *testing.T) []int {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("lecture de /proc : Linux seulement (Windows : procédure HUMAIN #221)")
	}
	me := os.Getpid()
	ents, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// « pid (comm) S ppid … » : comm peut contenir des espaces, on repart de la dernière parenthèse
		s := string(st)
		f := strings.Fields(s[strings.LastIndex(s, ")")+1:])
		if len(f) < 2 || f[0] == "Z" { // zombie : déjà sorti, pas encore attendu par le noyau
			continue
		}
		if ppid, _ := strconv.Atoi(f[1]); ppid != me {
			continue
		}
		cl, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		args := strings.Split(string(cl), "\x00")
		for _, a := range args {
			if a == "-C" || a == "-CC" {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// terminalGoroutines : piles des goroutines propres aux terminaux (flusher, watcher, lecture du client tmux).
func terminalGoroutines() []string {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		for _, name := range []string{"(*projTerm).flusher", "(*projTerm).watcher", "(*tmuxClient).read"} {
			if strings.Contains(g, name) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func assertNoTerminalLeft(t *testing.T) {
	t.Helper()
	if pids := childClients(t); len(pids) > 0 {
		t.Errorf("processus client tmux -C orphelins après Stop : %v", pids)
	}
	if gs := terminalGoroutines(); len(gs) > 0 {
		first := gs[0]
		if len(first) > 600 {
			first = first[:600]
		}
		t.Errorf("%d goroutine(s) de terminal après Stop, par exemple :\n%s", len(gs), first)
	}
}

// Après Stop : ni goroutine de terminal ni client tmux enfant, borné à 25 s (critical).
func TestStopLeavesNoTerminalBehind221(t *testing.T) {
	f := openFakeProject(t)
	if len(childClients(t)) == 0 {
		t.Fatal("précondition : aucun client tmux -C enfant pendant l'ouverture")
	}
	t0 := time.Now()
	f.a.Stop()
	if d := time.Since(t0); d > stopBudget {
		t.Errorf("Stop : %v, attendu ≤ %v", d, stopBudget)
	}
	assertNoTerminalLeft(t) // immédiatement : Stop a ATTENDU les terminaux, pas de pause d'attente ici
	if !f.sessionAlive() {
		t.Error("la session tmux du projet doit continuer après Stop (inchangé)")
	}
}

// Après Stop, plus aucune écriture vers un terminal : termInput est refusé (contrat).
func TestTermInputRefusedAfterStop221(t *testing.T) {
	f := openFakeProject(t)
	pane := ""
	for _, id := range strings.Fields(f.tmux("list-panes", "-t", "="+hostSession(f.path)+":", "-F", "#{pane_id}")) {
		pane = id
		break
	}
	raw := func() json.RawMessage {
		b, _ := json.Marshal(map[string]any{"path": f.path, "pane": pane, "data": "x"})
		return b
	}
	if res, err := f.a.Dispatch("termInput", raw()); err != nil || (res != nil && res.(map[string]any)["ok"] == false) {
		t.Fatalf("termInput avant Stop : %v %v", res, err)
	}
	f.a.Stop()
	res, err := f.a.Dispatch("termInput", raw())
	if err != nil {
		t.Fatalf("termInput après Stop : %v", err)
	}
	m, _ := res.(map[string]any)
	if m == nil || m["ok"] != false || m["msg"] != "interface arrêtée" {
		t.Errorf("termInput après Stop = %v, attendu {ok:false, msg:\"interface arrêtée\"}", res)
	}
	if pids := childClients(t); len(pids) > 0 {
		t.Errorf("client tmux encore là après Stop : %v", pids)
	}
}

// Arrêt pendant une écriture dans un terminal (à lancer avec -race, comme ci.sh) : aucune course, aucun blocage, aucun
// panic ; les écrivains terminent, plus rien d'orphelin ensuite.
func TestStopDuringTerminalWrites221(t *testing.T) {
	f := openFakeProject(t)
	pane := strings.Fields(f.tmux("list-panes", "-t", "="+hostSession(f.path)+":", "-F", "#{pane_id}"))[0]
	payload, _ := json.Marshal(map[string]any{"path": f.path, "pane": pane, "data": strings.Repeat("ligne de saisie\r", 200)})
	var stop atomic.Bool
	var writes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_, _ = f.a.Dispatch("termInput", payload)
				writes.Add(1)
			}
		}()
	}
	waitUntil(t, "écritures en cours", 5*time.Second, func() bool { return writes.Load() > 5 })
	done := make(chan struct{})
	go func() { f.a.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopBudget):
		t.Fatalf("Stop non terminé en %v pendant des écritures de terminal", stopBudget)
	}
	time.Sleep(100 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
	assertNoTerminalLeft(t)
}

// Stop est idempotent et un second appel ne rouvre rien.
func TestStopTwiceIsHarmless221(t *testing.T) {
	f := openFakeProject(t)
	f.a.Stop()
	t0 := time.Now()
	f.a.Stop()
	if d := time.Since(t0); d > time.Second {
		t.Errorf("second Stop : %v", d)
	}
	assertNoTerminalLeft(t)
}
