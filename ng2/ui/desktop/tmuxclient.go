package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
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

var errClientClosed = errors.New("client tmux fermé")

type tmuxClient struct {
	cmd *exec.Cmd
	in  io.WriteCloser

	mu      sync.Mutex
	pending []func(Reply) // une entrée par commande envoyée, dans l'ordre
	closed  bool

	notify func(Notif)
	done   chan struct{}
	exitMu sync.Mutex
	exit   string // raison de la fin (%exit, erreur de lecture)
}

// startClient : lance argv (… tmux -C attach-session …) ; notify reçoit les
// notifications, dans l'ordre, depuis la goroutine de lecture.
func startClient(argv []string, notify func(Notif)) (*tmuxClient, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
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
	c.pending = append(c.pending, cb)
	if _, err := io.WriteString(c.in, core.CtlLine(args)+"\n"); err != nil {
		c.pending = c.pending[:len(c.pending)-1]
		return err
	}
	return nil
}

// Send : commande sans attendre sa réponse.
func (c *tmuxClient) Send(args ...string) error { return c.sendCB(args, nil) }

var clientTimeout = 15 * time.Second

// Output : réponse d'une ou plusieurs commandes (« a ; b »), comme core.Target.
func (c *tmuxClient) Output(args ...string) ([]byte, error) {
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
			return out, fmt.Errorf("tmux : pas de réponse en %s", clientTimeout)
		}
	}
	return out, nil
}

func (c *tmuxClient) Run(args ...string) error {
	_, err := c.Output(args...)
	return err
}

// Close : détache le client (la session continue).
func (c *tmuxClient) Close() {
	_ = c.Send("detach-client")
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
	_ = c.in.Close()
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
