//go:build linux || darwin

package platform

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type unixTerm struct {
	f    *os.File
	cmd  *exec.Cmd
	once sync.Once
	werr error
	done chan struct{}
}

// StartTerminal : commande lancée dans un pseudo-terminal (session et terminal
// de contrôle à elle, comme dans un vrai terminal).
func StartTerminal(s TermSpec) (Terminal, error) {
	if len(s.Argv) == 0 {
		return nil, ErrNoCommand
	}
	c := exec.Command(s.Argv[0], s.Argv[1:]...)
	c.Env, c.Dir = s.Env, s.Dir
	cols, rows := termSize(s.Cols, s.Rows)
	f, err := pty.StartWithSize(c, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixTerm{f: f, cmd: c, done: make(chan struct{})}, nil
}

func (t *unixTerm) Read(p []byte) (int, error)  { return t.f.Read(p) }
func (t *unixTerm) Write(p []byte) (int, error) { return t.f.Write(p) }
func (t *unixTerm) Pid() int                    { return t.cmd.Process.Pid }

func (t *unixTerm) Resize(cols, rows int) error {
	c, r := termSize(cols, rows)
	return pty.Setsize(t.f, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
}

func (t *unixTerm) Wait() error {
	t.once.Do(func() {
		t.werr = t.cmd.Wait()
		close(t.done)
	})
	<-t.done
	return t.werr
}

func (t *unixTerm) Close() error {
	select {
	case <-t.done:
	default:
		_ = t.cmd.Process.Kill()
	}
	return t.f.Close()
}
