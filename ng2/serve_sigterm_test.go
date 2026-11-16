package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// #167 : SIGTERM arrête « desktop --web » proprement (code 0, port libéré) ; en cluster le jeton est conservé
// d'un démarrage à l'autre, hors cluster il change.
func TestDesktopWebStopsOnSIGTERMAndKeepsTokenInCluster(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	_ = os.MkdirAll(home, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	cache := filepath.Join(tmp, "etat")
	run := func(inCluster string) (token string, code int) {
		port := rbFreePort(t)
		var env []string
		for _, kv := range os.Environ() {
			k := strings.SplitN(kv, "=", 2)[0]
			if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" {
				env = append(env, kv)
			}
		}
		c := exec.Command(bin, "desktop", "--web", "--no-open", "--addr", "127.0.0.1:"+port)
		c.Env = append(env, "HOME="+home, "CL_CONFIG_DIR="+filepath.Join(tmp, "cfg"), "CL_CACHE_DIR="+cache, "CL_NO_UPDATE=1",
			"CL_IN_CLUSTER="+inCluster, "TMUX_TMPDIR="+t.TempDir())
		var out strings.Builder
		c.Stdout, c.Stderr = &out, &out
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Process.Kill() }()
		up := false
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if r, err := (&http.Client{Timeout: time.Second}).Get("http://127.0.0.1:" + port + "/healthz"); err == nil {
				r.Body.Close()
				up = true
				break
			}
		}
		if !up {
			t.Fatalf("le serveur ne démarre pas : %s", out.String())
		}
		if i := strings.Index(out.String(), "?t="); i >= 0 {
			rest := out.String()[i+3:]
			token = strings.Fields(rest)[0]
		}
		_ = c.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- c.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else {
					code = -1
				}
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("SIGTERM n'arrête pas le serveur : %s", out.String())
		}
		return
	}
	t1, c1 := run("1")
	t2, c2 := run("1")
	if c1 != 0 || c2 != 0 {
		t.Errorf("arrêt sur SIGTERM : codes %d et %d, attendu 0", c1, c2)
	}
	if t1 == "" || t1 != t2 {
		t.Errorf("en cluster le jeton doit être conservé : %q puis %q", t1, t2)
	}
	t3, _ := run("0")
	if t3 == "" || t3 == t1 {
		t.Errorf("hors cluster le jeton doit être nouveau : %q (précédent %q)", t3, t1)
	}
}
