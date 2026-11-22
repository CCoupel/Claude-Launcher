package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #223 — script de simulation de la procédure HUMAIN (tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh) :
// il écrit l'état que poseraient les hooks, que le launcher lit (ReadAttentionOf). Serveur tmux isolé, projet factice.
func TestSimulateAttentionScript223(t *testing.T) {
	unixOnly(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	script, _ := filepath.Abs("../../tests/procedures/ui-desktop/attente-teamleader-223/simulate-attention.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script absent : %v", err)
	}
	tmp, err := os.MkdirTemp("", "cltmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	t.Setenv("TMUX_TMPDIR", tmp)
	t.Setenv("TMUX", "")
	sock := fmt.Sprintf("cl-sim223-%d", os.Getpid())
	t.Setenv("CL_DESK_SOCKET", sock)
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", sock}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })

	proj := filepath.Join(t.TempDir(), "PROJETS", "CAT", "alpha")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", "p-cat-alpha", "-c", proj, "-x", "120", "-y", "40", "sleep 600").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable : %v %s", err, out)
	}
	leader := tm("display-message", "-p", "-t", "=p-cat-alpha:", "#{pane_id}")
	mate := tm("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "=p-cat-alpha:", "-c", proj, "sleep 600")
	if leader == "" || mate == "" {
		t.Fatalf("panes : %q %q", leader, mate)
	}

	run := func(args ...string) (string, error) {
		out, err := exec.Command("sh", append([]string{script, proj}, args...)...).CombinedOutput()
		return string(out), err
	}
	if out, err := run("question"); err != nil || !strings.Contains(out, "leader "+leader) {
		t.Fatalf("question : %v\n%s", err, out)
	}
	if a, ok := ReadAttentionOf(proj, leader); !ok || a.Kind != "question" || !strings.Contains(a.Text, "Valider le plan ?") {
		t.Errorf("après question : %+v %v", a, ok)
	}
	if out, err := run("permission"); err != nil {
		t.Fatalf("permission : %v\n%s", err, out)
	}
	time.Sleep(1100 * time.Millisecond)
	if a, ok := ReadAttentionOf(proj, leader); !ok || a.Kind != "permission" || !strings.Contains(a.Text, "permission pour utiliser Bash") {
		t.Errorf("après permission : %+v %v", a, ok)
	}
	if out, err := run("fin"); err != nil || !strings.Contains(out, "1 fichier") {
		t.Fatalf("fin : %v\n%s", err, out)
	}
	if a, ok := ReadAttentionOf(proj, leader); ok {
		t.Errorf("après fin : attente encore lue %+v", a)
	}
	// teammate : écrit par un autre pane, ignoré quand on filtre sur le leader
	if out, err := run("teammate"); err != nil || !strings.Contains(out, "TEAMMATE") {
		t.Fatalf("teammate : %v\n%s", err, out)
	}
	if a, ok := ReadAttentionOf(proj, leader); ok {
		t.Errorf("attente d'un teammate prise en compte : %+v", a)
	}
	if _, err := os.Stat(filepath.Join(AttentionDir(proj), strings.TrimPrefix(mate, "%")+".json")); err != nil {
		t.Errorf("état du teammate absent : %v", err)
	}
	_, _ = run("fin")
	// erreurs : action inconnue, projet fermé (aucune session)
	if _, err := run("n-importe-quoi"); err == nil {
		t.Error("action inconnue acceptée")
	}
	other := t.TempDir()
	if out, err := exec.Command("sh", script, other, "question").CombinedOutput(); err == nil || !strings.Contains(string(out), "aucune session tmux") {
		t.Errorf("projet sans session : %v %s", err, out)
	}
	if ents, _ := os.ReadDir(AttentionDir(other)); len(ents) > 0 {
		t.Errorf("fichier écrit pour un projet sans session : %v", ents)
	}
}
