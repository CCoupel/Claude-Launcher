//go:build relaunch219 && linux

package tmux

// #219 (critical) : relance de la coque sans perte de session. tmux isolé (isoInner), « binaires » v1/v2 = vrais
// exécutables Go compilés par le test. Tag provisoire `relaunch219`, à retirer. Nom ATTENDU :
//
//	func relaunchShellWith(exe string) error   // relaunchShell() = relaunchShellWith(platform.SelfExe())
//
// Attendu : vignettes mirror (@mirror_win) relancées par respawn-pane -k avec exe ; layout-watch anciens (même
// serveur tmux, exe différent) SIGTERM→SIGKILL puis run-shell -b avec les mêmes arguments ; jamais kill-session /
// kill-server ; panes de projet inchangés ; processus d'un autre serveur tmux non touchés.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

const fakeSrc219 = `package main

import (
	"fmt"
	"os"
	"time"
)

var ver = "v0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("claude-launcher-ng2", ver)
		return
	}
	time.Sleep(time.Minute)
}
`

func buildFake219(t *testing.T, dir, name, ver string) string {
	t.Helper()
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "main.go"), []byte(fakeSrc219), 0o644)
	_ = os.WriteFile(filepath.Join(src, "go.mod"), []byte("module fake219\n\ngo 1.20\n"), 0o644)
	out := filepath.Join(dir, name)
	c := exec.Command("go", "build", "-ldflags", "-X main.ver="+ver, "-o", out, ".")
	c.Dir = src
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		t.Skipf("go build indisponible : %v %s", err, b)
	}
	return out
}

// procsOf219 : pids dont /proc/<pid>/exe == exe, environnement contenant CL_INNER_SOCKET=sock, et cmdline[1] == role.
func procsOf219(exe, sock, role string) []int {
	var out []int
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if l, err := os.Readlink("/proc/" + e.Name() + "/exe"); err != nil || strings.TrimSuffix(l, " (deleted)") != exe {
			continue
		}
		env, _ := os.ReadFile("/proc/" + e.Name() + "/environ")
		cmd, _ := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		args := strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00")
		if strings.Contains("\x00"+string(env), "\x00CL_INNER_SOCKET="+sock+"\x00") && len(args) > 1 && args[1] == role {
			out = append(out, pid)
		}
	}
	return out
}

func eventually219(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 150; i++ {
		if ok() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Errorf("%s : condition non atteinte en 6 s", what)
}

func TestRelaunchShellSansPerteDeSession219(t *testing.T) {
	marker := isoInner(t)
	_, win, _, proj, leader, mates := visProject(t, marker)
	sock := core.InnerSocket()
	d := t.TempDir()
	v1 := buildFake219(t, d, "ng-v1", "v1.0.0")
	v2 := buildFake219(t, d, "ng-v2", "v2.0.0")
	env := "CL_HUB=" + core.Shq(hubSession) + " CL_INNER_SOCKET=" + core.Shq(sock) + " "

	// fenêtre des vignettes : une vignette v1 « mirror » (@mirror_win = fenêtre du projet)
	mirrorCmd := env + core.Shq(v1) + " mirror " + core.Shq(win) + " proj"
	mp := dt(t, "new-window", "-d", "-P", "-F", "#{pane_id}", "-n", teamWindow, "-t", exactS(hubSession), mirrorCmd)
	dt(t, "set-option", "-p", "-t", mp, "@mirror_win", win)
	// surveillant de mise en page v1, lancé comme openProject (run-shell -b)
	dt(t, "run-shell", "-b", env+core.Shq(v1)+" layout-watch "+core.Shq(win)+" "+core.Shq(leader)+" "+core.Shq(proj))
	// processus d'un AUTRE serveur tmux (autre socket) : jamais touché
	other := exec.Command(v1, "layout-watch", "@9", "%9", "/x")
	other.Env = append(os.Environ(), "CL_INNER_SOCKET=autre-serveur-219")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = other.Wait(); close(done) }()
	defer func() { _ = other.Process.Kill(); <-done }()

	eventually219(t, "mirror et layout-watch v1 démarrés", func() bool {
		return len(procsOf219(v1, sock, "mirror")) == 1 && len(procsOf219(v1, sock, "layout-watch")) == 1
	})
	projPids := map[string]string{}
	for _, p := range append([]string{leader}, mates...) {
		projPids[p] = dt(t, "display-message", "-p", "-t", p, "#{pane_pid}")
	}
	serverPid := dt(t, "display-message", "-p", "#{pid}")
	oldMirror := procsOf219(v1, sock, "mirror")[0]

	if err := relaunchShellWith(v2); err != nil {
		t.Fatalf("relaunchShellWith : %v", err)
	}

	eventually219(t, "plus aucun processus v1 du serveur", func() bool {
		return len(procsOf219(v1, sock, "mirror"))+len(procsOf219(v1, sock, "layout-watch")) == 0
	})
	eventually219(t, "mirror et layout-watch v2 relancés", func() bool {
		return len(procsOf219(v2, sock, "mirror")) == 1 && len(procsOf219(v2, sock, "layout-watch")) == 1
	})
	if len(procsOf219(v2, sock, "mirror")) == 1 && procsOf219(v2, sock, "mirror")[0] == oldMirror {
		t.Error("même pid avant/après : la vignette n'a pas été relancée")
	}
	// mêmes arguments pour le surveillant relancé
	if pids := procsOf219(v2, sock, "layout-watch"); len(pids) == 1 {
		cmd, _ := os.ReadFile("/proc/" + strconv.Itoa(pids[0]) + "/cmdline")
		want := strings.Join([]string{v2, "layout-watch", win, leader, proj}, "\x00")
		if got := strings.TrimRight(string(cmd), "\x00"); got != want {
			t.Errorf("arguments du layout-watch relancé : %q, attendu %q", got, want)
		}
	}
	// la vignette reste une vignette (même pane, @mirror_win conservé)
	if got := dt(t, "display-message", "-p", "-t", mp, "#{@mirror_win}"); got != win {
		t.Errorf("@mirror_win = %q après relance, attendu %q", got, win)
	}
	// sessions et panes de projet intacts
	if got := dt(t, "display-message", "-p", "#{pid}"); got != serverPid {
		t.Errorf("serveur tmux redémarré : %s → %s", serverPid, got)
	}
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		t.Fatal("session " + hubSession + " disparue")
	}
	for p, pid := range projPids {
		if got := dt(t, "display-message", "-p", "-t", p, "#{pane_pid}"); got != pid {
			t.Errorf("pane de projet %s : pid %s → %s", p, pid, got)
		}
		if _, err := os.Stat("/proc/" + pid); err != nil {
			t.Errorf("processus du pane %s (pid %s) mort", p, pid)
		}
	}
	// autre serveur : toujours vivant, toujours v1
	if _, err := os.Readlink("/proc/" + strconv.Itoa(other.Process.Pid) + "/exe"); err != nil {
		t.Error("processus d'un autre serveur tmux tué")
	}
	// idempotent : relancer encore ne fait rien de plus ni ne casse
	if err := relaunchShellWith(v2); err != nil {
		t.Errorf("seconde relance : %v", err)
	}
	if dtmux("has-session", "-t", exactS(hubSession)).Run() != nil {
		t.Error("session disparue à la seconde relance")
	}
}
