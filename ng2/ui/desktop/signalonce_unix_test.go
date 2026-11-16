//go:build linux

package desktop

import (
	"bufio"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// Sous-processus d'aide : installe onSignalOnce(SIGTERM) avec un arrêt LENT (10 s), dit « prêt » puis attend.
func TestSignalOnceHelperProcess(t *testing.T) {
	if os.Getenv("CL_TEST_SIGNALONCE") != "1" {
		t.Skip("sous-processus d'aide")
	}
	slow := make(chan struct{})
	defer onSignalOnce(func() {
		os.Stdout.WriteString("arret-lent\n")
		time.Sleep(10 * time.Second) // arrêt lent : seul un second signal peut l'abréger
		close(slow)
	}, syscall.SIGTERM)()
	os.Stdout.WriteString("pret\n")
	select {
	case <-slow:
	case <-time.After(30 * time.Second):
	}
}

// Revue M2 : le premier SIGTERM lance l'arrêt (lent) ; le traitement par défaut est ensuite rétabli : un SECOND
// SIGTERM tue le processus tout de suite au lieu d'attendre la fin de l'arrêt.
func TestOnSignalOnceSecondSignalKills(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^TestSignalOnceHelperProcess$", "-test.count=1")
	c.Env = append(os.Environ(), "CL_TEST_SIGNALONCE=1")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Process.Kill(); _ = c.Wait() }()
	rd := bufio.NewReader(out)
	waitLine := func(want string) {
		t.Helper()
		got := make(chan string, 1)
		go func() {
			for {
				l, err := rd.ReadString('\n')
				if err != nil || l == want+"\n" {
					got <- l
					return
				}
			}
		}()
		select {
		case l := <-got:
			if l != want+"\n" {
				t.Fatalf("attendu %q, lu %q", want, l)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%q jamais affiché", want)
		}
	}
	waitLine("pret")
	_ = c.Process.Signal(syscall.SIGTERM)
	waitLine("arret-lent") // premier signal : l'arrêt (lent) a démarré, le processus est vivant
	time.Sleep(200 * time.Millisecond)
	exited := make(chan error, 1)
	go func() { exited <- c.Wait() }()
	select {
	case <-exited:
		t.Fatal("le processus s'est arrêté avant le second signal (l'arrêt lent devait durer 10 s)")
	default:
	}
	_ = c.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("un second SIGTERM n'a pas tué le processus : le traitement par défaut n'est pas rétabli")
	}
}
