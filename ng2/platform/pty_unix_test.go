//go:build linux || darwin

package platform

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// readUntil : lit t jusqu'à voir want (ou délai dépassé).
func readUntil(t *testing.T, term Terminal, want string, out *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 4096)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("%q non vu ; reçu : %q", want, out.String())
		}
		n, err := term.Read(buf)
		out.Write(buf[:n])
		if err != nil && !strings.Contains(out.String(), want) {
			t.Fatalf("lecture : %v ; reçu : %q", err, out.String())
		}
	}
}

// commande interactive dans un pseudo-terminal redimensionnable
func TestStartTerminalResize(t *testing.T) {
	term, err := StartTerminal(TermSpec{Argv: []string{"sh", "-c", `stty size; read x; stty size; echo "lu:$x"`}, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if term.Pid() <= 0 {
		t.Error("pid")
	}
	var out bytes.Buffer
	readUntil(t, term, "30 100", &out)
	if err := term.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Write([]byte("bonjour\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, term, "lu:bonjour", &out)
	if !strings.Contains(out.String(), "40 120") {
		t.Errorf("taille après Resize : %q", out.String())
	}
	if err := term.Wait(); err != nil {
		t.Errorf("Wait : %v", err)
	}
}

func TestStartTerminalErrors(t *testing.T) {
	if _, err := StartTerminal(TermSpec{}); err != ErrNoCommand {
		t.Errorf("commande vide : %v", err)
	}
	if _, err := StartTerminal(TermSpec{Argv: []string{"commande-introuvable-cl"}}); err == nil {
		t.Error("commande introuvable : erreur attendue")
	}
	term, err := StartTerminal(TermSpec{Argv: []string{"sh", "-c", "exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := term.Wait(); err == nil {
		t.Error("statut 3 : erreur attendue")
	}
	_ = term.Close()
	// Close tue un processus encore en marche
	term, err = StartTerminal(TermSpec{Argv: []string{"sleep", "60"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = term.Close()
	done := make(chan error, 1)
	go func() { done <- term.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Close n'a pas arrêté le processus")
	}
}
