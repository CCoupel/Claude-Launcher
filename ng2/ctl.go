package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── Cible tmux par canal de contrôle (tmux -C) ────────────────────────────
//
// Un client « control mode » attaché au tmux d'un projet en conteneur, ouvert
// une fois par processus du launcher (layout-watch, vignette, vue État) et
// réutilisé : une commande = une ligne envoyée, une réponse encadrée par
// %begin … %end (ou %error). Les notifications (%layout-change…) sont ignorées.
// Le client est attaché avec -f ignore-size,no-output : il ne change pas la
// taille des fenêtres et ne reçoit pas la sortie des panes.

type ctlTarget struct {
	argv []string // commande qui ouvre le canal (… tmux -C attach)

	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	dead time.Time // dernier échec d'ouverture (pas de nouvel essai avant 2 s)
}

var errCtlDown = errors.New("canal de contrôle indisponible")

func (c *ctlTarget) Run(args ...string) error {
	_, err := c.Output(args...)
	return err
}

func (c *ctlTarget) Output(args ...string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.open(); err != nil {
		return nil, err
	}
	// une ligne par commande : en mode contrôle, tmux répond par un bloc
	// %begin…%end PAR commande d'une liste « a ; b » — n'en lire qu'un décalerait
	// toutes les réponses suivantes
	var out []byte
	for _, cmd := range splitCommands(args) {
		b, err := c.send(ctlLine(cmd))
		out = append(out, b...)
		if err != nil {
			if errors.Is(err, errCtlDown) {
				c.close()
			}
			return out, err
		}
	}
	return out, nil
}

// splitCommands : « a x ; b y » → [[a x] [b y]].
func splitCommands(args []string) [][]string {
	var out [][]string
	cur := []string{}
	for _, a := range args {
		if a == ";" {
			out = append(out, cur)
			cur = []string{}
			continue
		}
		cur = append(cur, a)
	}
	return append(out, cur)
}

func (c *ctlTarget) open() error {
	if c.cmd != nil {
		return nil
	}
	if time.Since(c.dead) < 2*time.Second {
		return errCtlDown
	}
	cmd := exec.Command(c.argv[0], c.argv[1:]...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		c.dead = time.Now()
		return fmt.Errorf("%w : %v", errCtlDown, err)
	}
	c.cmd, c.in, c.out = cmd, in, bufio.NewReaderSize(out, 1<<20)
	// réponse à l'attach lui-même
	if _, err := c.read(); err != nil {
		c.close()
		c.dead = time.Now()
		return err
	}
	return nil
}

func (c *ctlTarget) close() {
	if c.cmd == nil {
		return
	}
	_ = c.in.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	c.cmd, c.in, c.out = nil, nil, nil
}

func (c *ctlTarget) send(line string) ([]byte, error) {
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		return nil, fmt.Errorf("%w : %v", errCtlDown, err)
	}
	return c.read()
}

// read : prochain bloc %begin … %end|%error ; les notifications sont sautées.
func (c *ctlTarget) read() ([]byte, error) {
	var body []byte
	in := false
	for {
		l, err := c.out.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("%w : %v", errCtlDown, err)
		}
		t := strings.TrimRight(l, "\r\n")
		switch {
		case !in && strings.HasPrefix(t, "%begin "):
			in, body = true, nil
		case in && strings.HasPrefix(t, "%end "):
			return body, nil
		case in && strings.HasPrefix(t, "%error "):
			return body, fmt.Errorf("tmux : %s", strings.TrimSpace(string(body)))
		case in:
			body = append(body, t...)
			body = append(body, '\n')
		case strings.HasPrefix(t, "%exit"):
			return nil, fmt.Errorf("%w : session fermée", errCtlDown)
		}
	}
}

// ctlLine : arguments d'UNE commande → ligne de commande tmux.
func ctlLine(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a == ";" {
			q[i] = ";"
		} else {
			q[i] = tmuxQuote(a)
		}
	}
	return strings.Join(q, " ")
}

// tmuxQuote : un argument pour l'analyseur de commandes de tmux. Entre
// apostrophes rien n'est interprété ; un argument qui en contient passe entre
// guillemets, avec \ " $ ~ échappés. Les sauts de ligne n'ont pas d'échappement
// dans une commande : ils sont transmis en \n (send-keys les accepte ainsi).
func tmuxQuote(a string) string {
	if !strings.Contains(a, "'") && !strings.Contains(a, "\n") {
		return "'" + a + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, `~`, `\~`, "\n", `\n`)
	return `"` + r.Replace(a) + `"`
}

// ctlCache : un canal par conteneur et par processus.
var ctlCache = struct {
	sync.Mutex
	m map[string]*ctlTarget
}{m: map[string]*ctlTarget{}}

func CtlFor(e Engine, name string) *ctlTarget {
	key := e.B.Name + "/" + name
	ctlCache.Lock()
	defer ctlCache.Unlock()
	if t, ok := ctlCache.m[key]; ok {
		return t
	}
	attach := TmuxIn("-C", "attach-session", "-f", "ignore-size,no-output", "-t", "="+CtrSession)
	t := &ctlTarget{argv: e.ExecArgv(name, false, false, attach...)}
	ctlCache.m[key] = t
	return t
}
