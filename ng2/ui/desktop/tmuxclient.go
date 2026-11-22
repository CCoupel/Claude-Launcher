package desktop

import (
	"bufio"
	"claude-launcher-ng2/platform"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-launcher-ng2/core"
)

// ── Client tmux en mode contrôle ──────────────────────────────────────────
//
// Une attache « tmux -C attach-session » au tmux du projet (dans son conteneur,
// par docker/podman/kubectl exec -i ; sur l'hôte, directement), par de simples
// tuyaux : pas de pseudo-terminal, le protocole est fait de lignes. Le client
// reçoit la sortie de tous les panes (%output) et les changements de mise en
// page ; les commandes (send-keys, refresh-client -C, list-panes…) passent par
// le même canal. Il est dimensionné (refresh-client -C) à la taille de la zone
// des terminaux de l'interface. Fermer le client = se détacher : la session
// continue dans tmux.

// transportError : le canal vers tmux a échoué (fermé, sans réponse, écriture) — pas une réponse d'erreur de tmux.
type transportError struct{ msg string }

func (e *transportError) Error() string   { return e.msg }
func (e *transportError) Transport() bool { return true }

var errClientClosed error = &transportError{"client tmux fermé"}

type tmuxClient struct {
	cmd *exec.Cmd
	in  io.WriteCloser

	mu      sync.Mutex
	pending []func(Reply) // une entrée par commande envoyée, dans l'ordre
	closed  bool

	rounds atomic.Int64 // attentes de réponse (un Output ou un Batch = une) : coût en aller-retour sur le canal
	cmds   atomic.Int64 // commandes envoyées

	notify func(Notif)
	done   chan struct{}
	exitMu sync.Mutex
	exit   string // raison de la fin (%exit, erreur de lecture)
}

// startClient : lance argv (… tmux -C attach-session …) ; notify reçoit les
// notifications, dans l'ordre, depuis la goroutine de lecture.
func startClient(argv []string, notify func(Notif)) (*tmuxClient, error) {
	cmd := platform.NoWindow(exec.Command(argv[0], argv[1:]...))
	platform.SetProcessGroup(cmd) // arrêt forcé de tout l'arbre (KillTree) sans toucher au launcher
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var errBuf strings.Builder
	cmd.Stderr = &limitedWriter{w: &errBuf, n: 4096}
	c := &tmuxClient{cmd: cmd, in: in, notify: notify, done: make(chan struct{})}
	c.pending = []func(Reply){func(Reply) {}} // réponse à l'attache elle-même
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go c.read(bufio.NewReaderSize(out, 1<<20), &errBuf)
	return c, nil
}

func (c *tmuxClient) read(r *bufio.Reader, errBuf *strings.Builder) {
	var d decoder
	why := ""
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			rep, n := d.feed(strings.TrimRight(line, "\n"))
			if rep != nil {
				c.mu.Lock()
				var cb func(Reply)
				if len(c.pending) > 0 {
					cb, c.pending = c.pending[0], c.pending[1:]
				}
				c.mu.Unlock()
				if cb != nil {
					cb(*rep)
				}
			}
			if n != nil {
				if n.Name == "exit" {
					why = strings.Join(n.Args, " ")
					if why == "" {
						why = "session fermée"
					}
				}
				if c.notify != nil {
					c.notify(*n)
				}
			}
		}
		if err != nil {
			break
		}
	}
	_ = c.cmd.Wait()
	if why == "" {
		why = strings.TrimSpace(errBuf.String())
		if why == "" {
			why = "connexion au tmux du projet perdue"
		}
	}
	c.exitMu.Lock()
	c.exit = why
	c.exitMu.Unlock()
	c.mu.Lock()
	c.closed = true
	pend := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, cb := range pend {
		cb(Reply{Err: true, Body: []byte(why)})
	}
	close(c.done)
}

// Done : fermé quand le client s'arrête ; Why : pourquoi.
func (c *tmuxClient) Done() <-chan struct{} { return c.done }

func (c *tmuxClient) Why() string {
	c.exitMu.Lock()
	defer c.exitMu.Unlock()
	return c.exit
}

// sendCB : envoie UNE commande ; cb reçoit sa réponse, depuis la goroutine de
// lecture (donc avant toute notification arrivée après la réponse).
func (c *tmuxClient) sendCB(args []string, cb func(Reply)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errClientClosed
	}
	if cb == nil {
		cb = func(Reply) {}
	}
	c.cmds.Add(1)
	c.pending = append(c.pending, cb)
	if _, err := io.WriteString(c.in, core.CtlLine(args)+"\n"); err != nil {
		c.pending = c.pending[:len(c.pending)-1]
		return &transportError{"tmux : écriture impossible : " + err.Error()}
	}
	return nil
}

// Send : commande sans attendre sa réponse.
func (c *tmuxClient) Send(args ...string) error { return c.sendCB(args, nil) }

// batchFail : tests, échec de transport des lots de commandes.
var batchFail atomic.Bool

var clientTimeout = 15 * time.Second

// Output : réponse d'une ou plusieurs commandes (« a ; b »), comme core.Target.
func (c *tmuxClient) Output(args ...string) ([]byte, error) {
	c.rounds.Add(1)
	var out []byte
	for _, one := range core.SplitCommands(args) {
		ch := make(chan Reply, 1)
		if err := c.sendCB(one, func(r Reply) { ch <- r }); err != nil {
			return out, err
		}
		select {
		case r := <-ch:
			out = append(out, r.Body...)
			if r.Err {
				return out, fmt.Errorf("tmux : %s", strings.TrimSpace(string(r.Body)))
			}
		case <-time.After(clientTimeout):
			return out, &transportError{fmt.Sprintf("tmux : pas de réponse en %s", clientTimeout)}
		}
	}
	return out, nil
}

// Batch : plusieurs commandes indépendantes envoyées d'un coup (pipeline), réponses attendues ensemble :
// un seul aller-retour au lieu d'un par commande (conteneur distant, wsl.exe : plusieurs centaines de ms
// chacun). Une erreur d'une commande n'empêche pas les suivantes (comme des appels séparés) ; errs[i] est
// celle de cmds[i]. Implémente core.Batcher.
func (c *tmuxClient) Batch(cmds [][]string) ([][]byte, []error) {
	c.rounds.Add(1)
	if batchFail.Load() { // tests : le transport (wsl.exe, kubectl exec) échoue
		errs := make([]error, len(cmds))
		for i := range errs {
			errs[i] = errClientClosed
		}
		return make([][]byte, len(cmds)), errs
	}
	outs := make([][]byte, len(cmds))
	errs := make([]error, len(cmds))
	chans := make([]chan Reply, len(cmds))
	for i, one := range cmds {
		chans[i] = make(chan Reply, 1)
		ch := chans[i]
		if err := c.sendCB(one, func(r Reply) { ch <- r }); err != nil {
			errs[i] = err
			chans[i] = nil
		}
	}
	deadline := time.After(clientTimeout)
	for i, ch := range chans {
		if ch == nil {
			continue
		}
		select {
		case r := <-ch:
			outs[i] = r.Body
			if r.Err {
				errs[i] = fmt.Errorf("tmux : %s", strings.TrimSpace(string(r.Body)))
			}
		case <-deadline:
			errs[i] = &transportError{fmt.Sprintf("tmux : pas de réponse en %s", clientTimeout)}
		}
	}
	return outs, errs
}

func (c *tmuxClient) Run(args ...string) error {
	_, err := c.Output(args...)
	return err
}

// Close : détache le client (la session continue) ; arrêt forcé au bout de 2 s, attente brève de la fin.
func (c *tmuxClient) Close() {
	now := time.Now()
	c.CloseBy(now.Add(2*time.Second), now.Add(3*time.Second))
}

// CloseBy : détache le client (la session continue) puis ATTEND sa fin : à force, arrêt de l'arbre de processus du client
// (tmux -C, wsl.exe, kubectl exec, podman exec : platform.KillTree) ; l'attente de la goroutine de lecture finit à end au
// plus (#221). Rend vrai si le client est bien sorti.
func (c *tmuxClient) CloseBy(force, end time.Time) bool {
	go func() { _ = c.Send("detach-client") }() // l'écriture sur un canal bloqué ne retient pas la fermeture
	wait := func(until time.Time) bool {
		d := time.Until(until)
		if d < 0 {
			d = 0
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-c.done:
			return true
		case <-t.C:
			return false
		}
	}
	ok := wait(force)
	if !ok {
		_ = platform.KillTree(c.cmd)
		ok = wait(end)
	}
	_ = c.in.Close()
	return ok
}

type limitedWriter struct {
	mu sync.Mutex
	w  *strings.Builder
	n  int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rest := l.n - l.w.Len(); rest > 0 {
		l.w.Write(p[:min(len(p), rest)])
	}
	return len(p), nil
}
