package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// #165 (LOT-3.1) — racine de base configurable : CL_ROOT_BASE / --root-base. Contrat : contracts/launcher-env.md
// (« Racine de base », « Dossier de configuration — ordre de priorité », « Racine invalide », « backends »).
// Écrit depuis le contrat (TDD), en boîte noire : le vrai binaire est compilé une fois puis lancé sur des
// racines FACTICES (dossiers temporaires, HOME factice) — jamais la racine réelle ni un vrai projet.

var (
	rbBin    string // binaire compilé une fois
	rbBinErr error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cl-rootbase-bin-")
	if err != nil {
		os.Exit(1)
	}
	name := "claude-launcher-ng2"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	rbBin = filepath.Join(dir, name)
	if out, e := exec.Command("go", "build", "-o", rbBin, ".").CombinedOutput(); e != nil {
		rbBinErr = e
		_ = os.WriteFile(filepath.Join(dir, "build.log"), out, 0o644)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// rbKillTmux : arrête tout serveur tmux dont le socket est dans dir (TMUX_TMPDIR d'un appel).
func rbKillTmux(dir string) {
	socks, _ := filepath.Glob(filepath.Join(dir, "tmux-*", "*"))
	for _, sk := range socks {
		_ = exec.Command("tmux", "-S", sk, "kill-server").Run()
	}
}

// rbTmuxDir : dossier de sockets tmux d'un sous-processus de longue durée, tué en fin de test.
func rbTmuxDir(t *testing.T) string {
	d, _ := os.MkdirTemp("", "clrb")
	t.Cleanup(func() { rbKillTmux(d); _ = os.RemoveAll(d) })
	return d
}

func rbDataHome() string { return filepath.Join(os.TempDir(), "cl-rootbase-data") }

func rbNeedBinary(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("scénarios Linux : le démarrage sous Windows est dans la procédure humaine")
	}
	if rbBinErr != nil {
		t.Fatalf("compilation du binaire : %v", rbBinErr)
	}
}

// rbRun : lance bin avec args dans un environnement propre (aucune variable CL_* héritée) : HOME factice, extra en plus.
func rbRun(t *testing.T, bin string, home string, extra []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		k := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(k, "CL_") || k == "CLAUDE_CONFIG_DIR" || k == "XDG_CONFIG_HOME" || k == "XDG_CACHE_HOME" || k == "HOME" || k == "EDITOR" || k == "TMUX" {
			continue
		}
		env = append(env, kv)
	}
	// XDG_DATA_HOME hors du HOME factice : un podman présent (runner de CI) y crée son stockage de conteneurs, aux droits
	// restreints, que le nettoyage de t.TempDir ne peut pas effacer
	// tout tmux lancé par le binaire (une option inconnue d'une version non encore livrée ouvrait le vrai hub : « nav » et
	// « state » restaient 3 h) vit dans un dossier de sockets PROPRE à l'appel, tué en fin de test ; le groupe de
	// processus entier est tué à la fin ou au dépassement du délai
	tmuxDir, _ := os.MkdirTemp("", "clrb")
	t.Cleanup(func() { rbKillTmux(tmuxDir); _ = os.RemoveAll(tmuxDir) })
	env = append(env, "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+rbDataHome(), "TMUX_TMPDIR="+tmuxDir, "EDITOR=true", "CL_NO_UPDATE=1")
	env = append(env, extra...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	c.Env = env
	rbOwnGroup(c)
	rbCancel(c)
	var so, se strings.Builder
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	rbKillGroup(c) // fils éventuels (nav, state…) du groupe
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("lancement : %v", err)
	}
	return so.String(), se.String(), code
}

var rbCreated = regexp.MustCompile(`Config créée : (.+)`)

// rbConfigCreated : dossier où « configure » a créé config.conf ("" : aucun).
func rbConfigCreated(out string) string {
	if m := rbCreated.FindStringSubmatch(out); m != nil {
		return filepath.Dir(strings.TrimSpace(m[1]))
	}
	return ""
}

func rbCopyBin(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, filepath.Base(rbBin))
	b, err := os.ReadFile(rbBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return dst
}

func rbExists(p string) bool { _, err := os.Stat(p); return err == nil }

const rbCfgSub = ".config/claude-launcher-ng2"

// ── CRITICAL : matrice de priorité du dossier de configuration ───────────────

func TestRootBaseConfigDirPriority(t *testing.T) {
	rbNeedBinary(t)
	for _, tc := range []struct {
		name string
		args func(root, cfg string) (extra []string, args []string)
		want func(home, root, cfg, bindir string) string
		// portable : un config.conf existe à côté du binaire
		portable bool
	}{
		{"CL_CONFIG_DIR l'emporte sur tout",
			func(root, cfg string) ([]string, []string) {
				return []string{"CL_CONFIG_DIR=" + cfg, "CL_ROOT_BASE=" + root}, []string{"configure"}
			},
			func(home, root, cfg, bin string) string { return cfg }, true},
		{"racine de base (variable) > dossier du binaire",
			func(root, cfg string) ([]string, []string) {
				return []string{"CL_ROOT_BASE=" + root}, []string{"configure"}
			},
			func(home, root, cfg, bin string) string { return filepath.Join(root, rbCfgSub) }, true},
		{"--root-base (option) > variable",
			func(root, cfg string) ([]string, []string) {
				return []string{"CL_ROOT_BASE=/chemin/qui/n/existe/pas"}, []string{"--root-base", root, "configure"}
			},
			func(home, root, cfg, bin string) string { return filepath.Join(root, rbCfgSub) }, false},
		{"--root-base=forme collée, après la sous-commande",
			func(root, cfg string) ([]string, []string) {
				return nil, []string{"configure", "--root-base=" + root}
			},
			func(home, root, cfg, bin string) string { return filepath.Join(root, rbCfgSub) }, false},
		{"racine de base retenue même sans config.conf existant",
			func(root, cfg string) ([]string, []string) {
				return []string{"CL_ROOT_BASE=" + root}, []string{"configure"}
			},
			func(home, root, cfg, bin string) string { return filepath.Join(root, rbCfgSub) }, false},
		{"sans racine : dossier du binaire (mode portable, inchangé)",
			func(root, cfg string) ([]string, []string) { return nil, []string{"backends"} },
			func(home, root, cfg, bin string) string { return filepath.Join(bin, rbCfgSub) }, true},
		{"sans racine ni portable : dossier personnel (inchangé)",
			func(root, cfg string) ([]string, []string) { return nil, []string{"configure"} },
			func(home, root, cfg, bin string) string { return filepath.Join(home, rbCfgSub) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			home, root, cfg, bindir := filepath.Join(tmp, "home"), filepath.Join(tmp, "root"), filepath.Join(tmp, "cfg"), filepath.Join(tmp, "bin")
			for _, d := range []string{home, root, cfg} {
				_ = os.MkdirAll(d, 0o755)
			}
			bin := rbCopyBin(t, bindir)
			if tc.portable {
				p := filepath.Join(bindir, rbCfgSub)
				_ = os.MkdirAll(p, 0o755)
				_ = os.WriteFile(filepath.Join(p, "config.conf"), []byte("# portable\nGITHUB_DIR=\"/x\"\n"), 0o600)
			}
			extra, args := tc.args(root, cfg)
			want := tc.want(home, root, cfg, bindir)
			out, errOut, code := rbRun(t, bin, home, extra, args...)
			if code != 0 {
				t.Fatalf("code %d : %s %s", code, out, errOut)
			}
			if args[0] == "configure" || len(args) > 1 && args[1] == "configure" || args[len(args)-1] == "configure" {
				// création de la configuration : où ?
				if tc.portable && want == filepath.Join(bindir, rbCfgSub) {
					return
				}
				if got := rbConfigCreated(out); got != "" && got != want {
					t.Errorf("config.conf créé dans %q, attendu %q", got, want)
				}
				if got := rbConfigCreated(out); got == "" && !rbExists(filepath.Join(want, "config.conf")) {
					t.Errorf("aucun config.conf sous %q (sortie : %s)", want, out)
				}
				// rien créé dans les autres emplacements possibles
				for _, other := range []string{filepath.Join(home, rbCfgSub), filepath.Join(root, rbCfgSub), cfg} {
					if other != want && rbExists(filepath.Join(other, "config.conf")) && !tc.portable {
						t.Errorf("config.conf créé aussi dans %q", other)
					}
				}
			} else if !strings.Contains(out, want) {
				t.Errorf("« backends » ne cite pas %q :\n%s", want, out)
			}
		})
	}
}

// ── racine invalide : message clair, code non nul, rien créé ailleurs ────────

func TestRootBaseInvalidRefusedWithNoSideEffects(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	file := filepath.Join(tmp, "un-fichier")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	for _, tc := range []struct {
		name   string
		root   string
		viaOpt bool
	}{
		{"absente (variable)", filepath.Join(tmp, "absente"), false},
		{"pas un dossier (variable)", file, false},
		{"absente (option)", filepath.Join(tmp, "absente2"), true},
		{"pas un dossier (option)", file, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			bin := rbCopyBin(t, filepath.Join(t.TempDir(), "bin"))
			var extra, args []string
			source := "CL_ROOT_BASE"
			if tc.viaOpt {
				args, source = []string{"configure", "--root-base", tc.root}, "--root-base"
			} else {
				extra, args = []string{"CL_ROOT_BASE=" + tc.root}, []string{"configure"}
			}
			out, errOut, code := rbRun(t, bin, home, extra, args...)
			if code == 0 {
				t.Fatalf("code 0 pour une racine invalide : %s", out)
			}
			msg := errOut + out
			if !strings.Contains(msg, tc.root) || !strings.Contains(msg, source) {
				t.Errorf("le message doit nommer le chemin %q et sa source %s : %q", tc.root, source, msg)
			}
			if rbConfigCreated(out) != "" {
				t.Errorf("une configuration a été créée malgré une racine invalide : %s", out)
			}
			if rbExists(filepath.Join(home, ".config")) {
				t.Errorf("« .config » créé dans le dossier personnel malgré une racine invalide")
			}
			if rbExists(tc.root) && tc.root != file { // la racine absente n'est pas créée
				t.Errorf("la racine invalide %q a été créée", tc.root)
			}
			if fi, err := os.Stat(tc.root); err == nil && tc.root == file && !fi.Mode().IsRegular() {
				t.Errorf("le fichier a été remplacé")
			}
		})
	}
}

// ── config.conf sous la racine, projets et config de claude relatifs à elle ──

func TestRootBaseConfigProposedRelativeToRoot(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home, root := filepath.Join(tmp, "home"), filepath.Join(tmp, "base")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(root, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	out, errOut, code := rbRun(t, bin, home, []string{"CL_ROOT_BASE=" + root}, "configure")
	if code != 0 {
		t.Fatalf("code %d : %s %s", code, out, errOut)
	}
	b, err := os.ReadFile(filepath.Join(root, rbCfgSub, "config.conf"))
	if err != nil {
		t.Fatalf("config.conf absent sous la racine : %v", err)
	}
	var active []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			active = append(active, l)
		}
	}
	joined := strings.Join(active, "\n")
	if strings.Contains(joined, "$HOME/GITHUB") {
		t.Errorf("la racine de projets proposée reste $HOME/GITHUB :\n%s", joined)
	}
	if !strings.Contains(joined, "PROJETS") {
		t.Errorf("<racine>/PROJETS non proposé :\n%s", joined)
	}
	if !strings.Contains(joined, ".claude") {
		t.Errorf("<racine>/.claude (CLAUDE_CONFIG_DIR) non proposé :\n%s", joined)
	}
	if strings.Contains(string(b), home) {
		t.Errorf("le dossier personnel factice apparaît dans une configuration sous la racine")
	}
}

// ── section « Racine de base » de « backends » ───────────────────────────────

func TestBackendsShowsRootBaseWithSource(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home, root := filepath.Join(tmp, "home"), filepath.Join(tmp, "base")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(root, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	for _, tc := range []struct {
		name   string
		extra  []string
		args   []string
		source string
	}{
		{"variable", []string{"CL_ROOT_BASE=" + root}, []string{"backends"}, "(CL_ROOT_BASE)"},
		{"option", nil, []string{"backends", "--root-base", root}, "(--root-base)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := rbRun(t, bin, home, tc.extra, tc.args...)
			if code != 0 {
				t.Fatalf("code %d : %s %s", code, out, errOut)
			}
			if !strings.HasPrefix(strings.TrimLeft(out, "\n"), "Racine de base : "+root+" "+tc.source) {
				t.Errorf("la sortie doit COMMENCER par « Racine de base : %s %s » :\n%s", root, tc.source, out)
			}
			for _, want := range []string{
				filepath.Join(root, rbCfgSub),
				filepath.Join(root, ".claude"),
				filepath.Join(root, "PROJETS"),
			} {
				if !strings.Contains(out, want) {
					t.Errorf("« %s » absent de la section :\n%s", want, out)
				}
			}
		})
	}
	// sans racine : « aucune »
	out, _, code := rbRun(t, bin, home, nil, "backends")
	if code != 0 || !strings.Contains(out, "Racine de base : aucune") {
		t.Errorf("sans racine : code %d, attendu « Racine de base : aucune » :\n%s", code, out)
	}
}

// ── non-régression : sans la variable, comportement de la v6.2.0 ─────────────

func TestWithoutRootBaseBehavesAsV620(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	_ = os.MkdirAll(home, 0o755)
	bin := rbCopyBin(t, filepath.Join(tmp, "bin"))
	out, errOut, code := rbRun(t, bin, home, nil, "configure")
	if code != 0 {
		t.Fatalf("code %d : %s %s", code, out, errOut)
	}
	if got := rbConfigCreated(out); got != filepath.Join(home, rbCfgSub) {
		t.Errorf("configuration créée dans %q, attendu le dossier personnel %q (v6.2.0)", got, filepath.Join(home, rbCfgSub))
	}
	b, _ := os.ReadFile(filepath.Join(home, rbCfgSub, "config.conf"))
	if !strings.Contains(string(b), `GITHUB_DIR="$HOME/GITHUB"`) {
		t.Errorf("la configuration par défaut n'est plus celle de la v6.2.0 :\n%s", b)
	}
	// une variable VIDE vaut « absente »
	home2 := filepath.Join(tmp, "home2")
	_ = os.MkdirAll(home2, 0o755)
	out2, _, code2 := rbRun(t, bin, home2, []string{"CL_ROOT_BASE="}, "configure")
	if code2 != 0 || rbConfigCreated(out2) != filepath.Join(home2, rbCfgSub) {
		t.Errorf("CL_ROOT_BASE vide : code %d, créée dans %q", code2, rbConfigCreated(out2))
	}
	// version et aide : inchangées, sans effet de bord
	if o, _, c := rbRun(t, bin, home, nil, "version"); c != 0 || !strings.Contains(o, "Claude-Launcher-NG2") {
		t.Errorf("version : %d %q", c, o)
	}
}

// ── binaire hors de la racine = binaire dans la racine ───────────────────────

func TestBinaryOutsideEqualsInsideRoot(t *testing.T) {
	rbNeedBinary(t)
	tmp := t.TempDir()
	home, root := filepath.Join(tmp, "home"), filepath.Join(tmp, "base")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(filepath.Join(root, "PROJETS"), 0o755)
	// configuration et backends propres à la racine
	cfg := filepath.Join(root, rbCfgSub)
	_ = os.MkdirAll(cfg, 0o755)
	_ = os.WriteFile(filepath.Join(cfg, "backends.yaml"), []byte("defaults:\n  browser_server: { listen: off }\n"), 0o644)
	_ = os.WriteFile(filepath.Join(cfg, "config.conf"), []byte("GITHUB_DIR=\""+filepath.Join(root, "PROJETS")+"\"\n"), 0o600)
	outside := rbCopyBin(t, filepath.Join(tmp, "ailleurs", "bin"))
	inside := rbCopyBin(t, filepath.Join(root, "bin"))
	extra := []string{"CL_ROOT_BASE=" + root}
	o1, e1, c1 := rbRun(t, outside, home, extra, "backends")
	o2, e2, c2 := rbRun(t, inside, home, extra, "backends")
	if c1 != 0 || c2 != 0 {
		t.Fatalf("codes %d/%d : %s %s %s %s", c1, c2, o1, e1, o2, e2)
	}
	if o1 != o2 {
		t.Errorf("« backends » diffère selon l'emplacement du binaire :\n--- hors racine ---\n%s\n--- dans la racine ---\n%s", o1, o2)
	}
	if !strings.Contains(o1, filepath.Join(cfg, "backends.yaml")) {
		t.Errorf("les backends ne viennent pas de la racine de base : %s", o1)
	}
	if rbExists(filepath.Join(home, ".config")) {
		t.Errorf("le dossier personnel a servi malgré la racine de base")
	}
}

// ── installation complète sur une racine factice : « desktop --web » ─────────

func TestFullInstallOnFakeRoot(t *testing.T) {
	rbNeedBinary(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	tmp := t.TempDir()
	home, root := filepath.Join(tmp, "home"), filepath.Join(tmp, "base")
	_ = os.MkdirAll(home, 0o755)
	proj := filepath.Join(root, "PROJETS", "TEST", "alpha")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	bin := rbCopyBin(t, filepath.Join(tmp, "ailleurs"))
	// première exécution : la configuration est créée sous la racine
	if out, errOut, code := rbRun(t, bin, home, []string{"CL_ROOT_BASE=" + root}, "configure"); code != 0 {
		t.Fatalf("configure : %d %s %s", code, out, errOut)
	}
	port := rbFreePort(t)
	env := []string{"CL_ROOT_BASE=" + root, "CL_DESKTOP_TOKEN=tokRootBaseInstall12345", "TMUX_TMPDIR=" + rbTmuxDir(t), "XDG_DATA_HOME=" + rbDataHome()}
	var cmdEnv []string
	for _, kv := range os.Environ() {
		k := strings.SplitN(kv, "=", 2)[0]
		if !strings.HasPrefix(k, "CL_") && k != "HOME" && k != "XDG_CACHE_HOME" && k != "TMUX" && k != "CLAUDE_CONFIG_DIR" {
			cmdEnv = append(cmdEnv, kv)
		}
	}
	c := exec.Command(bin, "desktop", "--web", "--no-open", "--addr", "127.0.0.1:"+port)
	c.Env = append(append(cmdEnv, "HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "CL_NO_UPDATE=1"), env...)
	stdout, _ := c.StdoutPipe()
	var se strings.Builder
	c.Stderr = &se
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, bufio.NewReader(stdout)) }()
	stopped := false
	exited := make(chan struct{})
	go func() { _ = c.Wait(); close(exited) }()
	var quit func()
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if quit != nil { // arrêt propre d'abord : tâches de fond finies avant l'effacement des dossiers
			quit()
		}
		select {
		case <-exited:
		case <-time.After(8 * time.Second):
			_ = c.Process.Kill()
			<-exited
		}
		time.Sleep(300 * time.Millisecond)
	}
	defer stop()
	base := "http://127.0.0.1:" + port
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 10 * time.Second}
	var resp *http.Response
	var err error
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if resp, err = cl.Get(base + "/?t=tokRootBaseInstall12345"); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("le launcher ne répond pas : %v (stderr : %s)", err, se.String())
	}
	resp.Body.Close()
	var ck *http.Cookie
	for _, k := range resp.Cookies() {
		if strings.Contains(k.Name, "cl_session") {
			ck = k
		}
	}
	if ck == nil {
		t.Fatalf("pas de session (%d)", resp.StatusCode)
	}
	quit = func() {
		q, _ := http.NewRequest("POST", base+"/api/call", strings.NewReader(`{"method":"quit","args":{"mode":"l"}}`))
		q.Header.Set("Content-Type", "application/json")
		q.AddCookie(ck)
		if r, err := cl.Do(q); err == nil {
			r.Body.Close()
		}
	}
	req, _ := http.NewRequest("POST", base+"/api/call", strings.NewReader(`{"method":"init","args":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(ck)
	r2, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r2.Body)
	r2.Body.Close()
	var v any
	_ = json.Unmarshal(b, &v)
	if !strings.Contains(string(b), "alpha") {
		t.Errorf("le projet factice de la racine de base n'est pas listé (%d) : %.300s", r2.StatusCode, b)
	}
	stop()
	// rien hors de la racine : le dossier personnel n'a ni configuration ni liste de projets
	if rbExists(filepath.Join(home, ".config")) {
		t.Errorf("« ~/.config » créé malgré la racine de base")
	}
	if !rbExists(filepath.Join(root, rbCfgSub, "config.conf")) {
		t.Errorf("config.conf absent sous la racine")
	}
}

func rbFreePort(t *testing.T) string {
	t.Helper()
	c := exec.Command("sh", "-c", "python3 -c 'import socket;s=socket.socket();s.bind((\"127.0.0.1\",0));print(s.getsockname()[1])'")
	out, err := c.Output()
	if err != nil {
		t.Skip("port libre indisponible : ", err)
	}
	return strings.TrimSpace(string(out))
}
