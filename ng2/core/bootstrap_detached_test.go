//go:build !windows

package core

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Bootstrap détaché du launcher (#198) : il va jusqu'au bout même si le client (kubectl exec) meurt, n'est pas relancé
// s'il tourne déjà, et un pod resté à moitié installé est détecté puis réparé. Aucun vrai conteneur : le « conteneur »
// est un dossier, le faux kubectl exécute localement la commande reçue.

func startStarter(t *testing.T, dir, script string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	c := exec.Command("sh", "-c", bootstrapStarter(dir))
	c.Stdin, c.Stdout, c.Stderr = strings.NewReader(script), &out, &out
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	return c, &out
}

func waitFile(t *testing.T, p string, d time.Duration) string {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s absent après %s", p, d)
	return ""
}

func TestBootstrapSurvitALaMortDuClient(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	c, _ := startStarter(t, dir, "echo début\nsleep 1\necho fait > "+filepath.Join(dir, "out")+"\n")
	time.Sleep(300 * time.Millisecond)
	_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL) // le launcher (et son kubectl exec) disparaît
	_ = c.Wait()
	if rc := waitFile(t, filepath.Join(dir, "boot.rc"), 6*time.Second); rc != "0" {
		t.Errorf("code de sortie du bootstrap détaché = %q", rc)
	}
	if got := waitFile(t, filepath.Join(dir, "out"), 2*time.Second); got != "fait" {
		t.Errorf("le script n'est pas allé au bout : %q", got)
	}
}

func TestBootstrapSuitLaSortieEtLeCode(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	c, out := startStarter(t, dir, "echo bonjour\n")
	if err := c.Wait(); err != nil || !strings.Contains(out.String(), "bonjour") {
		t.Errorf("succès : %v %q", err, out.String())
	}
	c, out = startStarter(t, t.TempDir(), "echo boum\nexit 3\n")
	err := c.Wait()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 || !strings.Contains(out.String(), "boum") {
		t.Errorf("échec : %v %q", err, out.String())
	}
}

// Un bootstrap déjà en cours (launcher disparu) n'est pas relancé : le suivant le suit jusqu'au bout.
func TestBootstrapEnCoursNEstPasRelance(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	cnt := filepath.Join(dir, "runs")
	first, _ := startStarter(t, dir, "echo 1 >> "+cnt+"\nsleep 1\n")
	time.Sleep(400 * time.Millisecond)
	_ = syscall.Kill(-first.Process.Pid, syscall.SIGKILL)
	_ = first.Wait()
	second, out := startStarter(t, dir, "echo 2 >> "+cnt+"\n")
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		if err != nil || !strings.Contains(out.String(), "déjà en cours") {
			t.Errorf("suivi : %v %q", err, out.String())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("le suivi du bootstrap en cours ne finit pas")
	}
	if b, _ := os.ReadFile(cnt); strings.TrimSpace(string(b)) != "1" {
		t.Errorf("le script a été lancé plusieurs fois : %q", b)
	}
}

// Bootstrap tué en cours de route (pas de code de sortie) : le suivi échoue clairement, la prochaine ouverture repart.
func TestBootstrapInterromputEstRepris(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	c, out := startStarter(t, dir, "sleep 30\n")
	pid, _ := strconv.Atoi(waitFile(t, filepath.Join(dir, "boot.pid"), 3*time.Second))
	_ = syscall.Kill(pid, syscall.SIGKILL) // le processus détaché meurt (OOM, nœud…)
	if err := c.Wait(); err == nil || !strings.Contains(out.String(), "interrompu") {
		t.Errorf("suivi d'un bootstrap tué : %v %q", err, out.String())
	}
	c, out = startStarter(t, dir, "echo repris\n")
	if err := c.Wait(); err != nil || !strings.Contains(out.String(), "repris") {
		t.Errorf("reprise : %v %q", err, out.String())
	}
}

// fakeExecKubectl : « kubectl exec -i pod -c main -- cmd… » exécute cmd localement.
func fakeExecKubectl(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nwhile [ \"$1\" != \"--\" ] && [ $# -gt 0 ]; do shift; done\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(InClusterVar, "0")
}

func TestBootstrapIncompleteEtReparation(t *testing.T) {
	unixOnly(t)
	fakeExecKubectl(t)
	d := t.TempDir()
	oldDir, oldScript := bootDir, bootstrapScript
	bootDir = d
	defer func() { bootDir, bootstrapScript = oldDir, oldScript }()
	e := Engine{BackendConf{Name: "k", Type: "k8s", Namespace: "ns"}}

	if !e.BootstrapIncomplete("pod-0") {
		t.Error("pod neuf sans marqueur : installation à faire")
	}
	// installation interrompue : journal sans code de sortie, processus mort
	_ = os.WriteFile(filepath.Join(d, "boot.log"), []byte("apt…\n"), 0o600)
	_ = os.WriteFile(filepath.Join(d, "boot.pid"), []byte("999999"), 0o600)
	if !e.BootstrapIncomplete("pod-0") {
		t.Error("installation interrompue non détectée")
	}
	// réparation : le script (marqueur écrit à la fin, comme bootstrap.sh) rejoué
	bootstrapScript = "echo ok > " + filepath.Join(d, "bootstrap") + "\n"
	if err := e.Bootstrap("pod-0", "/p", EnvSpec{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("bootstrap : %v", err)
	}
	if e.BootstrapIncomplete("pod-0") {
		t.Error("pod réparé encore signalé incomplet")
	}
	// échec du script : erreur remontée
	bootstrapScript = "exit 4\n"
	_ = os.Remove(filepath.Join(d, "bootstrap"))
	if err := e.Bootstrap("pod-0", "/p", EnvSpec{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Errorf("échec attendu : %v", err)
	}
}
