package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// LOT-4.1 (#167, #168) — « desktop --web » en mode cluster (CL_IN_CLUSTER=1, racine de base montée, sans
// configuration préalable) : l'interface est servie, les sondes répondent, config.conf existe sous la racine.
// Boîte noire sur le vrai binaire ; racine FACTICE, HOME factice, aucun vrai projet.
func TestClusterServerModeServesAndCreatesConfigUnderRoot(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home, root := filepath.Join(tmp, "home"), filepath.Join(tmp, "montage")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(filepath.Join(root, "PROJETS"), 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "image", "bin"))
	port := rbFreePort(t)
	var env []string
	for _, kv := range os.Environ() {
		k := strings.SplitN(kv, "=", 2)[0]
		if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" && k != "KUBERNETES_SERVICE_HOST" {
			env = append(env, kv)
		}
	}
	c := exec.Command(bin, "desktop", "--web", "--no-open", "--addr", "127.0.0.1:"+port)
	c.Env = append(env, "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+rbDataHome(), "TMUX_TMPDIR="+rbTmuxDir(t),
		"CL_ROOT_BASE="+root, "CL_IN_CLUSTER=1", "CL_UPDATE_MODE=notify", "CL_HUB_NAME=cl-test-cluster", "CL_NO_UPDATE=1")
	rbOwnGroup(c)
	out := &lockedBuf{} // lu pendant que le sous-processus écrit (stdout et stderr y vont ensemble)
	c.Stdout, c.Stderr = out, out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = c.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = c.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(8 * time.Second):
			rbKillGroup(c)
			<-exited
		}
	})
	base := "http://127.0.0.1:" + port
	cl := &http.Client{Timeout: 2 * time.Second}
	up := false
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if r, err := cl.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			up = r.StatusCode == 200
			break
		}
	}
	if !up {
		t.Fatalf("/healthz ne répond pas 200 : %s", out.String())
	}
	if r, err := cl.Get(base + "/readyz"); err != nil || r.StatusCode != 200 {
		t.Errorf("/readyz : %v %v", r, err)
	} else {
		r.Body.Close()
	}
	cfg := filepath.Join(root, ".config", "claude-launcher-ng2", "config.conf")
	created := false
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if _, err := os.Stat(cfg); err == nil {
			created = true
			break
		}
	}
	if !created {
		t.Errorf("config.conf non créé sous la racine montée après 10 s de fonctionnement (%s) — création paresseuse ? à documenter ou à faire au démarrage (constat infra, image du cluster)", cfg)
	}
	if rbExists(filepath.Join(home, ".config")) {
		t.Errorf("« ~/.config » créé dans le dossier personnel de l'image malgré la racine de base")
	}
	if m := out.String(); regexp.MustCompile(`\?t=[A-Za-z0-9_-]{16,}`).MatchString(m) { // « ?t=<jeton> » (espace réservé) est permis
		t.Errorf("le jeton d'accès est imprimé sur la sortie du pod : %q", m)
	}
}

// LOT-4.4 : « desktop --web » en mode d'accès none — la sortie donne l'adresse puis « accès sans jeton », jamais
// l'indication du fichier du jeton ni un lien « ?t= » ; aucun fichier de jeton n'est créé.
func TestServerModeNoneStartupOutputAndNoTokenFile(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	_ = os.MkdirAll(home, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	port := rbFreePort(t)
	cache := filepath.Join(tmp, "cache")
	var env []string
	for _, kv := range os.Environ() {
		k := strings.SplitN(kv, "=", 2)[0]
		if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" {
			env = append(env, kv)
		}
	}
	c := exec.Command(bin, "desktop", "--web", "--no-open", "--addr", "127.0.0.1:"+port)
	c.Env = append(env, "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+rbDataHome(), "TMUX_TMPDIR="+rbTmuxDir(t),
		"CL_WEB_ACCESS=none", "CL_CACHE_DIR="+cache, "CL_NO_UPDATE=1", "CL_IN_CLUSTER=0", "CL_DESKTOP_TOKEN=tokMustBeIgnored1234567890")
	rbOwnGroup(c)
	out := &lockedBuf{}
	c.Stdout, c.Stderr = out, out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = c.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = c.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(8 * time.Second):
			rbKillGroup(c)
			<-exited
		}
	})
	base := "http://127.0.0.1:" + port
	cl := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	up := false
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if r, err := cl.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			up = true
			break
		}
	}
	if !up {
		t.Fatalf("le serveur ne répond pas : %s", out.String())
	}
	if r, err := cl.Get(base + "/"); err != nil || r.StatusCode != 200 || len(r.Cookies()) != 0 {
		t.Errorf("GET / sans jeton : %v %v", r, err)
	} else {
		r.Body.Close()
	}
	m := out.String()
	if !strings.Contains(m, "127.0.0.1:"+port) || !strings.Contains(strings.ToLower(m), "sans jeton") {
		t.Errorf("la sortie doit donner l'adresse puis « accès sans jeton » : %q", m)
	}
	if regexp.MustCompile(`\?t=`).MatchString(m) || strings.Contains(m, "desktop-web-token") || strings.Contains(m, "tokMustBeIgnored") {
		t.Errorf("la sortie du mode none mentionne un jeton, son fichier ou un lien ?t= : %q", m)
	}
	if rbExists(filepath.Join(cache, "desktop-web-token")) {
		t.Errorf("fichier desktop-web-token créé en mode none")
	}
}

// LOT-4.4 : en « desktop --web », le mode none sur une écoute réseau SANS adresse publique https n'est pas démarré :
// arrêt avec un code non nul et un message qui dit pourquoi ; avec --url https : démarre.
func TestServerModeNoneRefusedOnNetworkListenWithoutHTTPSURL(t *testing.T) {
	rbNeedBinary(t)
	home := t.TempDir()
	bin := rbCopyBin(t, filepath.Join(t.TempDir(), "bin"))
	port := rbFreePort(t)
	out, errOut, code := rbRun(t, bin, home, []string{"CL_WEB_ACCESS=none", "CL_IN_CLUSTER=0"}, "desktop", "--web", "--no-open", "--addr", "0.0.0.0:"+port)
	if code == 0 {
		t.Fatalf("code 0 : le mode none sur 0.0.0.0 sans https a démarré\n%s%s", out, errOut)
	}
	if m := out + errOut; !strings.Contains(m, "sans jeton") {
		t.Errorf("le message ne dit pas pourquoi (accès sans jeton refusé) : %q", m)
	}
}
