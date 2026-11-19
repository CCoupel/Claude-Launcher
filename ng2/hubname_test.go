package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #169 / #176 (LOT-3.2) — boîte noire (binaire compilé par TestMain de rootbase_test.go) : nom de hub invalide,
// second launcher du même nom refusé, backend host refusé en cluster. HOME FACTICE systématique (rbRun).
// Contrat : contracts/launcher-env.md (« Identité et état », « Launcher dans un cluster »).

func TestInvalidHubNameRefused(t *testing.T) {
	rbNeedBinary(t)
	for _, bad := range []string{"-debut", "a b", "nom/slash", strings.Repeat("a", 64), "é"} {
		t.Run(bad, func(t *testing.T) {
			home := t.TempDir()
			out, errOut, code := rbRun(t, rbBin, home, []string{"CL_HUB_NAME=" + bad}, "backends")
			if code == 0 {
				t.Fatalf("CL_HUB_NAME=%q accepté : %s", bad, out)
			}
			if !strings.Contains(errOut+out, "CL_HUB_NAME") {
				t.Errorf("le message doit nommer CL_HUB_NAME : %q", errOut+out)
			}
		})
	}
	home := t.TempDir()
	if _, e, code := rbRun(t, rbBin, home, []string{"CL_HUB_NAME=cluster-qualif.1_ok"}, "backends"); code != 0 {
		t.Errorf("nom valide refusé : %s", e)
	}
}

// Deux launchers de même nom sur un état partagé : le second refuse de démarrer avec un message ; un nom
// différent démarre.
func TestSecondLauncherSameNameRefused(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	cache := filepath.Join(tmp, "partage")
	_ = os.MkdirAll(cache, 0o755)
	start := func(name, port string) (*exec.Cmd, *lockedBuf) {
		home := t.TempDir()
		var env []string
		for _, kv := range os.Environ() {
			k := strings.SplitN(kv, "=", 2)[0]
			if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" {
				env = append(env, kv)
			}
		}
		c := exec.Command(rbBin, "desktop", "--web", "--no-open", "--addr", "127.0.0.1:"+port)
		c.Env = append(env, "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "CL_CONFIG_DIR="+filepath.Join(tmp, "cfg"),
			"XDG_DATA_HOME="+rbDataHome(), "CL_CACHE_DIR="+cache, "CL_HUB_NAME="+name, "CL_NO_UPDATE=1", "CL_IN_CLUSTER=0", "TMUX_TMPDIR="+rbTmuxDir(t), "CL_DESKTOP_TOKEN=tokSameName123456789012")
		se := &lockedBuf{}
		c.Stderr, c.Stdout = se, se
		rbOwnGroup(c)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); rbKillGroup(c); _ = c.Wait() })
		return c, se
	}
	waitUp := func(port string) bool {
		cl := &http.Client{Timeout: time.Second}
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if r, err := cl.Get("http://127.0.0.1:" + port + "/healthz"); err == nil {
				r.Body.Close()
				return true
			}
		}
		return false
	}
	p1, p2, p3 := rbFreePort(t), rbFreePort(t), rbFreePort(t)
	first, _ := start("cl-test-a", p1)
	if !waitUp(p1) {
		t.Fatal("le premier launcher ne démarre pas")
	}
	second, out2 := start("cl-test-a", p2) // même nom
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("le second launcher du même nom a quitté avec le code 0")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("le second launcher de même nom ne refuse pas de démarrer")
	}
	if m := out2.String(); !strings.Contains(m, "cl-test-a") {
		t.Errorf("le message de refus doit nommer le launcher (cl-test-a) : %q", m)
	}
	if first.ProcessState != nil {
		t.Errorf("le premier launcher s'est arrêté")
	}
	// nom différent : démarre normalement
	_, _ = start("cl-test-b", p3)
	if !waitUp(p3) {
		t.Error("un launcher de nom différent ne démarre pas")
	}
}

// Backend « host » refusé dans un launcher en cluster (message du contrat), sauf CL_ALLOW_HOST_BACKEND=1 ;
// hors cluster (CL_IN_CLUSTER=0) : inchangé.
func TestHostBackendRefusedInCluster(t *testing.T) {
	rbNeedBinary(t)
	const msg = "backend host refusé dans le pod du launcher"
	for _, tc := range []struct {
		name    string
		env     []string
		refused bool
	}{
		{"en cluster", []string{"CL_IN_CLUSTER=1"}, true},
		{"en cluster, host autorisé", []string{"CL_IN_CLUSTER=1", "CL_ALLOW_HOST_BACKEND=1"}, false},
		{"hors cluster", []string{"CL_IN_CLUSTER=0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, _ := rbRun(t, rbBin, t.TempDir(), tc.env, "backends")
			if got := strings.Contains(out+errOut, msg); got != tc.refused {
				t.Errorf("message « %s » présent = %v, attendu %v :\n%s%s", msg, got, tc.refused, out, errOut)
			}
		})
	}
}
