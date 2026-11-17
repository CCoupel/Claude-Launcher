//go:build linux

package desktop

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

// Libération du témoin de présence au DÉBUT du SIGTERM (rapport dev-backend-presence-20261117-131646.md, B) : un arrêt qui
// finit en SIGKILL (délai de grâce du pod dépassé) ne laisse plus de témoin « launcher déjà actif ».
// Sous-processus : prend la présence, installe onSignalOnce avec un arrêt LENT (10 s), dit « pret » puis attend.

func TestPresenceSignalHelperProcess(t *testing.T) {
	if os.Getenv("CL_TEST_PRESENCE_SIGNAL") != "1" {
		t.Skip("sous-processus d'aide")
	}
	rel, err := core.AcquirePresence()
	if err != nil {
		os.Stdout.WriteString("echec-presence:" + err.Error() + "\n")
		return
	}
	defer rel()
	slow := make(chan struct{})
	defer onSignalOnce(func() {
		os.Stdout.WriteString("arret-lent\n")
		time.Sleep(10 * time.Second) // arrêt lent : détachement des terminaux
		close(slow)
	}, syscall.SIGTERM)()
	os.Stdout.WriteString("pret\n")
	select {
	case <-slow:
	case <-time.After(40 * time.Second):
	}
}

func TestPresenceReleasedAtStartOfSlowSIGTERMShutdown(t *testing.T) {
	cache := t.TempDir()
	home := t.TempDir()
	witness := filepath.Join(cache, "hub-cl-test-slow.presence")
	c := exec.Command(os.Args[0], "-test.run=^TestPresenceSignalHelperProcess$", "-test.count=1")
	c.Env = append(os.Environ(), "CL_TEST_PRESENCE_SIGNAL=1", "CL_HUB_NAME=cl-test-slow", "CL_CACHE_DIR="+cache, "HOME="+home, "XDG_CACHE_HOME="+home, "CL_IN_CLUSTER=0")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Process.Kill(); _ = c.Wait() }()
	lines := make(chan string, 16)
	go func() {
		rd := bufio.NewReader(out)
		for {
			l, err := rd.ReadString('\n')
			if l != "" {
				lines <- strings.TrimSpace(l)
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	waitLine := func(want string) {
		t.Helper()
		end := time.After(30 * time.Second) // large : machine chargée
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("sous-processus terminé avant %q", want)
				}
				if strings.HasPrefix(l, "echec-presence") {
					t.Fatalf("le sous-processus n'a pas obtenu la présence : %s", l)
				}
				if l == want {
					return
				}
			case <-end:
				t.Fatalf("%q jamais affiché", want)
			}
		}
	}
	waitLine("pret")
	poll := func(what string, d time.Duration, cond func() bool) bool {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			if cond() {
				return true
			}
			time.Sleep(25 * time.Millisecond)
		}
		return false
	}
	exists := func() bool { _, err := os.Stat(witness); return err == nil }
	if !poll("témoin présent", 10*time.Second, exists) {
		t.Fatalf("aucun témoin de présence %s en fonctionnement normal", witness)
	}
	_ = c.Process.Signal(syscall.SIGTERM)
	waitLine("arret-lent") // l'arrêt lent (10 s) a démarré
	// le témoin disparaît PENDANT l'arrêt lent (avant sa fin), pas à la fin
	if !poll("témoin retiré", 6*time.Second, func() bool { return !exists() }) {
		t.Errorf("le témoin de présence est encore là 6 s après le SIGTERM, pendant l'arrêt lent : un SIGKILL à la fin du délai de grâce le laisserait (« launcher déjà actif » au successeur)")
	}
	// le processus est toujours en train de s'arrêter (l'arrêt lent n'est pas terminé)
	exited := make(chan error, 1)
	go func() { exited <- c.Wait() }()
	select {
	case <-exited:
		t.Errorf("le processus s'est arrêté avant la fin attendue de l'arrêt lent : le test ne prouve rien")
	default:
	}
	// SIGKILL (délai de grâce dépassé) : aucun témoin ne doit rester
	_ = c.Process.Signal(syscall.SIGKILL)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("SIGKILL sans effet")
	}
	if exists() {
		t.Errorf("témoin de présence laissé après SIGKILL")
	}
}
