package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// #136 — hypothèses techniques de contracts/tmux-options.md vérifiées sur un
// serveur tmux isolé (boîte noire : tmux brut, sans l'API de core) :
//   - break-pane -d met un pane de côté dans la session de réserve cl-hidden,
//     join-pane -d le ramène : %id et processus conservés, rien n'est tué ;
//   - un pane caché n'apparaît plus dans list-panes de la fenêtre du projet,
//     mais se retrouve par son option de pane @cl_home ;
//   - les options de pane (@cl_home, @cl_idle_at) suivent le pane ;
//   - un pane caché survit à kill-window du projet (d'où l'obligation, pour la
//     fermeture, de tuer aussi la réserve) ; tuer la réserve ne laisse aucun
//     processus.
// Faux claude = `sleep` (jamais le vrai) ; serveur dédié, jamais claude-hub.

const hiddenSession = "cl-hidden"

func isoServer(t *testing.T) (sock string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	sock = fmt.Sprintf("cl-test-hide-%d", os.Getpid())
	t.Setenv("CL_OUTER_SOCKET", sock)
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	// dernier enregistré = premier exécuté, avant la suppression de TMUX_TMPDIR (#144)
	t.Cleanup(func() {
		stopOuterShell()
		if left := leftovers(sock, 5*time.Second); left != "" {
			t.Errorf("processus restants après le test (%s) :\n%s", sock, left)
		}
	})
	return sock
}

func tm(t *testing.T, args ...string) string {
	t.Helper()
	return strings.TrimSpace(run(t, otmux(args...)))
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func has(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// alive : le processus du pane tourne encore.
func alive(pid string) bool {
	n, err := strconv.Atoi(pid)
	if err != nil {
		return false
	}
	return exec.Command("kill", "-0", strconv.Itoa(n)).Run() == nil
}

// projet factice : fenêtre « proj » avec leader + 3 « teammates » (sleep).
func fakeProject(t *testing.T) (win string, leader string, mates []string) {
	t.Helper()
	if out, err := otmux("-f", "/dev/null", "new-session", "-d", "-s", "hub", "-n", "proj", "-x", "200", "-y", "50", "sleep 600").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	win = tm(t, "display-message", "-p", "-t", "hub:proj", "#{window_id}")
	leader = tm(t, "display-message", "-p", "-t", "hub:proj.0", "#{pane_id}")
	for i := 0; i < 3; i++ {
		mates = append(mates, tm(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", win, "sleep 600"))
	}
	return
}

func winPanes(t *testing.T, win string) []string {
	return lines(tm(t, "list-panes", "-t", win, "-F", "#{pane_id}"))
}

// hide / show : la technique du contrat (créer la réserve à la demande).
func hide(t *testing.T, pane, win string) {
	t.Helper()
	if otmux("has-session", "-t", "="+hiddenSession).Run() != nil {
		tm(t, "new-session", "-d", "-s", hiddenSession, "-x", "200", "-y", "50", "sleep 600")
	}
	tm(t, "set-option", "-p", "-t", pane, "@cl_home", win)
	tm(t, "break-pane", "-d", "-s", pane, "-t", hiddenSession+":")
}

func show(t *testing.T, pane, win string) {
	t.Helper()
	tm(t, "join-pane", "-d", "-s", pane, "-t", win)
	tm(t, "set-option", "-p", "-u", "-t", pane, "@cl_home")
}

// critical : cacher puis ramener ne tue aucun pane et conserve les %id.
func TestHidePaneKeepsIdAndProcess(t *testing.T) {
	isoServer(t)
	win, leader, mates := fakeProject(t)
	before := winPanes(t, win)
	pid := tm(t, "display-message", "-p", "-t", mates[1], "#{pane_pid}")
	tm(t, "set-option", "-p", "-t", mates[1], "@cl_idle_at", "1700000000")

	hide(t, mates[1], win)
	if has(winPanes(t, win), mates[1]) {
		t.Fatal("pane caché encore dans list-panes de la fenêtre du projet")
	}
	if got := winPanes(t, win); len(got) != len(before)-1 || !has(got, leader) {
		t.Fatalf("fenêtre après masquage : %v (avant %v)", got, before)
	}
	if !alive(pid) {
		t.Fatal("processus du pane caché tué")
	}
	// retrouvé par la réserve et @cl_home
	all := lines(tm(t, "list-panes", "-a", "-F", "#{pane_id} #{session_name} #{@cl_home}"))
	if !has(all, mates[1]+" "+hiddenSession+" "+win) {
		t.Fatalf("pane absent de la réserve ou @cl_home perdu : %v", all)
	}
	if v := tm(t, "show-options", "-pqv", "-t", mates[1], "@cl_idle_at"); v != "1700000000" {
		t.Errorf("@cl_idle_at perdu dans la réserve : %q", v)
	}

	show(t, mates[1], win)
	after := winPanes(t, win)
	if len(after) != len(before) || !has(after, mates[1]) {
		t.Fatalf("après retour : %v, attendu les mêmes %%id que %v", after, before)
	}
	if got := tm(t, "display-message", "-p", "-t", mates[1], "#{pane_pid}"); got != pid || !alive(pid) {
		t.Errorf("processus changé ou mort : %s → %s", pid, got)
	}
	if v := tm(t, "show-options", "-pqv", "-t", mates[1], "@cl_home"); v != "" {
		t.Errorf("@cl_home non retiré au retour : %q", v)
	}
	if v := tm(t, "show-options", "-pqv", "-t", mates[1], "@cl_idle_at"); v != "1700000000" {
		t.Errorf("@cl_idle_at doit suivre le pane : %q", v)
	}
}

// Plusieurs panes cachés : un seul serveur, une fenêtre de réserve par pane,
// tous recensés par @cl_home ; la fenêtre du projet ne garde que les visibles.
func TestHideSeveralPanesAreCounted(t *testing.T) {
	isoServer(t)
	win, leader, mates := fakeProject(t)
	hide(t, mates[0], win)
	hide(t, mates[2], win)
	if got := winPanes(t, win); len(got) != 2 || !has(got, leader) || !has(got, mates[1]) {
		t.Fatalf("visibles : %v", got)
	}
	n := 0
	for _, l := range lines(tm(t, "list-panes", "-a", "-F", "#{session_name} #{@cl_home}")) {
		if l == hiddenSession+" "+win {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d panes recensés dans la réserve pour le projet, attendu 2", n)
	}
	show(t, mates[0], win)
	show(t, mates[2], win)
	if got := winPanes(t, win); len(got) != 4 {
		t.Errorf("retour de tous : %v", got)
	}
}

// critical : fermer un projet ne laisse aucun processus dans la réserve. La
// fenêtre du projet tuée, le pane caché survit (limite du contrat) ; la
// fermeture doit donc tuer la réserve du projet puis la session si vide.
func TestHiddenPaneSurvivesWindowKillThenReserveKillLeavesNoProcess(t *testing.T) {
	isoServer(t)
	win, _, mates := fakeProject(t)
	pid := tm(t, "display-message", "-p", "-t", mates[0], "#{pane_pid}")
	hide(t, mates[0], win)

	tm(t, "kill-window", "-t", win)
	if !alive(pid) {
		t.Fatal("le pane caché ne survit pas à kill-window : hypothèse du contrat fausse")
	}
	if !has(lines(tm(t, "list-panes", "-a", "-F", "#{pane_id}")), mates[0]) {
		t.Fatal("pane caché disparu avec la fenêtre du projet")
	}

	tm(t, "kill-pane", "-t", mates[0])
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(pid) {
		t.Fatal("processus du pane caché encore vivant après kill-pane")
	}
	// tmux ne ferme pas la session de réserve : new-session crée une fenêtre
	// d'amorce. La fermeture doit donc retirer la session (kill-session) quand
	// elle ne contient plus de pane caché d'aucun projet ; elle ne doit alors
	// laisser aucun processus (contrôlé par le nettoyage du test).
	left := lines(tm(t, "list-panes", "-t", "="+hiddenSession, "-a", "-F", "#{session_name} #{@cl_home}"))
	for _, l := range left {
		if strings.HasPrefix(l, hiddenSession+" ") && strings.TrimPrefix(l, hiddenSession+" ") != "" {
			t.Errorf("pane de la réserve encore rattaché à un projet : %q", l)
		}
	}
	tm(t, "kill-session", "-t", "="+hiddenSession)
	if otmux("has-session", "-t", "="+hiddenSession).Run() == nil {
		t.Error("session cl-hidden toujours présente après kill-session")
	}
}
