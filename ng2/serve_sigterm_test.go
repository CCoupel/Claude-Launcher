package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
			"CL_IN_CLUSTER="+inCluster, "TMUX_TMPDIR="+t.TempDir(),
			"XDG_DATA_HOME="+rbDataHome()) // données de podman hors du TempDir : sinon son stockage, créé sous HOME, entre en course avec l'effacement du dossier de test
		rbOwnGroup(c)
		out := &lockedBuf{}
		c.Stdout, c.Stderr = out, out
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Process.Kill(); rbKillGroup(c) }() // les processus fils (podman…) aussi : rien ne tourne encore à l'effacement du TempDir
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

// lockedBuf : sortie d'un sous-processus lue pendant qu'il écrit (stdout et stderr y vont ensemble).
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// #167 : en cluster, la sortie standard ne porte jamais le jeton ; message propre au mode serveur ; pas d'avertissement
// « 0.0.0.0 sans allow_lan ». Hors cluster : inchangé (lien avec jeton, Ctrl+C).
func TestServerModeOutputInCluster(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	_ = os.MkdirAll(home, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	run := func(inCluster string, extraEnv ...string) string {
		port := rbFreePort(t)
		var env []string
		for _, kv := range os.Environ() {
			k := strings.SplitN(kv, "=", 2)[0]
			if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" {
				env = append(env, kv)
			}
		}
		c := exec.Command(bin, "desktop", "--web", "--no-open", "--addr", "0.0.0.0:"+port)
		c.Env = append(append(env, "HOME="+home, "CL_CONFIG_DIR="+filepath.Join(tmp, "cfg"), "CL_CACHE_DIR="+filepath.Join(tmp, "etat"),
			"CL_NO_UPDATE=1", "CL_IN_CLUSTER="+inCluster, "TMUX_TMPDIR="+rbTmuxDir(t),
			"XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+rbDataHome()), extraEnv...) // données de podman hors du TempDir (runner CI : stockage aux droits restreints créé sous HOME)
		rbOwnGroup(c)
		out := &lockedBuf{}
		c.Stdout, c.Stderr = out, out
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { _ = c.Wait(); close(exited) }()
		defer func() { // arrêt propre d'abord, puis le groupe entier : aucun processus fils n'écrit plus pendant le nettoyage du TempDir
			_ = c.Process.Signal(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(8 * time.Second):
			}
			rbKillGroup(c)
			<-exited
			time.Sleep(200 * time.Millisecond)
		}()
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if r, err := (&http.Client{Timeout: time.Second}).Get("http://127.0.0.1:" + port + "/healthz"); err == nil {
				r.Body.Close()
				break
			}
		}
		return out.String()
	}
	in := run("1")
	tok, _ := os.ReadFile(filepath.Join(tmp, "etat", "desktop-web-token"))
	if t0 := strings.TrimSpace(string(tok)); t0 == "" || strings.Contains(in, t0) {
		t.Errorf("en cluster le jeton (%q) ne doit pas apparaître dans la sortie : %s", t0, in)
	}
	for _, want := range []string{"mode serveur", "desktop-web-token", "SIGTERM"} {
		if !strings.Contains(in, want) {
			t.Errorf("en cluster : %q absent de la sortie : %s", want, in)
		}
	}
	for _, bad := range []string{"Ctrl+C", "allow_lan", "?t=" + strings.TrimSpace(string(tok))} {
		if strings.Contains(in, bad) {
			t.Errorf("en cluster : %q ne doit pas apparaître : %s", bad, in)
		}
	}
	if given := run("1", "CL_DESKTOP_TOKEN=tokFourniParLeChart1234567"); strings.Contains(given, "tokFourni") || !strings.Contains(given, "CL_DESKTOP_TOKEN") {
		t.Errorf("jeton fourni : sortie %s", given)
	}
	out := run("0", "CL_DESKTOP_TOKEN=tokHorsCluster12345678901")
	if !strings.Contains(out, "?t=tokHorsCluster12345678901") || !strings.Contains(out, "Ctrl+C") || !strings.Contains(out, "allow_lan") {
		t.Errorf("hors cluster, sortie v6.2.0 attendue : %s", out)
	}
}
