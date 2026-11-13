package core

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Les tests de correspondance (pathmap_test.go…) simulent un poste Windows
// sous Linux : toutes les entrées de hubs.<machine>.paths y sont retenues,
// quelle que soit leur forme. Le filtre par système (pathsForOS) est testé
// explicitement ci-dessous.
func init() { pathsGOOS = "" }

// configuration portable partagée par le hub Linux (WSL) et l'exe Windows du
// même poste, sous le même nom de machine (#110)
const sharedBackendsYAML = `backends:
  podman: {type: podman}
  srv1-docker: {type: docker, host: tcp://192.168.1.211:2375, shared: [/share/MFS/ROOT/CLAUDE_PROJETS]}
  k8s: {type: k8s, context: k8s, namespace: claude-ng2, volumes: {/share/MFS/ROOT/CLAUDE_PROJETS: claude-projets}}
hubs:
  MSI:
    browser_server: {listen: 127.0.0.1:8080}
    paths:
      - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
`

const sharedConfigConf = `PROJECT_BASE="/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/"
GITHUB_DIRS=("$PROJECT_BASE")
CLAUDE_CONFIG_DIR="$PROJECT_BASE/../.claude"
`

// hubs.<machine>.paths : une entrée de forme Windows ne vaut que sous Windows,
// une entrée Linux qu'ailleurs (même config, même nom de machine)
func TestPathsForOSSharedConfig(t *testing.T) {
	var f backendsFile
	if err := yaml.Unmarshal([]byte(sharedBackendsYAML), &f); err != nil {
		t.Fatal(err)
	}
	if e := resolveHubSettingsFor(f, "MSI", "linux"); !e.paths.Empty() || e.src != "" || e.skipped != 1 || len(e.warns) != 0 {
		t.Errorf("hub Linux : entrée Windows retenue : %+v", e)
	}
	e := resolveHubSettingsFor(f, "MSI", "windows")
	if es := e.paths.Entries(); len(es) != 1 || es[0].Local != wslMFS || es[0].Remote != "/share/MFS" || e.src != "hubs.MSI.paths" || e.skipped != 0 {
		t.Fatalf("Windows : %+v", e)
	}
	// les deux formes dans la même machine : chacune pour son système
	f.Hubs["MSI"] = hubConf{Paths: []PathMap{{Local: wslMFS, Remote: "/share/MFS"}, {Local: "/mnt/mfs", Remote: "/share/MFS"}}}
	if es := resolveHubSettingsFor(f, "MSI", "linux").paths.Entries(); len(es) != 1 || es[0].Local != "/mnt/mfs" {
		t.Errorf("Linux, deux formes : %+v", es)
	}
	if es := resolveHubSettingsFor(f, "MSI", "windows").paths.Entries(); len(es) != 1 || es[0].Local != wslMFS {
		t.Errorf("Windows, deux formes : %+v", es)
	}
	// "" : toutes (comportement de resolveHubSettings)
	if es := resolveHubSettings(f, "MSI").paths.Entries(); len(es) != 2 {
		t.Errorf("sans filtre : %+v", es)
	}
}

// hubSettings applique le filtre du système (pathsGOOS)
func TestHubSettingsFiltersByOS(t *testing.T) {
	withHubPaths(t, "")
	defer func() { pathsGOOS = "" }()
	pathsGOOS = "linux"
	if m, src, _ := HubPaths(); !m.Empty() || src != "" || LocalPath("/share/MFS/x") != "/share/MFS/x" {
		t.Errorf("linux : %+v %q", m.Entries(), src)
	}
	pathsGOOS = "windows"
	if l := LocalPath("/share/MFS/x"); l != wslMFS+`\x` {
		t.Errorf("windows : %q", l)
	}
}

// chemins Linux de config.conf sous Windows : chemins des backends, convertis
// pour l'accès local, jamais « \share\… » (logique pure)
func TestConfLocalPath(t *testing.T) {
	m, _ := NewPathMapper([]PathMap{{Local: wslMFS, Remote: "/share/MFS"}})
	cases := []struct {
		goos string
		m    PathMapper
		in   string
		want string
		ok   bool
	}{
		{"windows", m, "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/", wslMFS + `\ROOT\CLAUDE_PROJETS\PROJETS`, true},
		{"windows", m, "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS//../.claude", wslMFS + `\ROOT\CLAUDE_PROJETS\.claude`, true},
		{"windows", m, "/share/MFS", wslMFS, true},
		{"windows", m, "/opt/projets", "/opt/projets", false},
		{"windows", PathMapper{}, "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS//../.claude", "/share/MFS/ROOT/CLAUDE_PROJETS/.claude", false},
		{"windows", m, `C:\Users\moi\GITHUB`, `C:\Users\moi\GITHUB`, true},
		{"windows", m, wslMFS + `\ROOT`, wslMFS + `\ROOT`, true},
		{"linux", m, "/share/MFS/ROOT/PROJETS//../.claude", "/share/MFS/ROOT/PROJETS//../.claude", true},
		{"linux", PathMapper{}, "/share/MFS/ROOT", "/share/MFS/ROOT", true},
	}
	for _, c := range cases {
		if got, ok := confLocalPath(c.goos, c.m, c.in); got != c.want || ok != c.ok {
			t.Errorf("%s %q : %q %v, attendu %q %v", c.goos, c.in, got, ok, c.want, c.ok)
		}
	}
}

// config.conf partagé (lu sans bash comme sous Windows) puis rendu local
func TestSharedConfigOnWindows(t *testing.T) {
	cfg := configFromLines(confLite(sharedConfigConf, `C:\Users\moi`))
	if len(cfg.Roots) != 1 || cfg.Roots[0] != "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS/" || cfg.ClaudeConfigDir != "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS//../.claude" {
		t.Fatalf("confLite : %+v", cfg)
	}
	m, _ := NewPathMapper([]PathMap{{Local: wslMFS, Remote: "/share/MFS"}})
	w := cfg
	w.Roots = append([]string(nil), cfg.Roots...)
	applyConfPaths("windows", m, &w)
	if w.Roots[0] != wslMFS+`\ROOT\CLAUDE_PROJETS\PROJETS` || w.ClaudeConfigDir != wslMFS+`\ROOT\CLAUDE_PROJETS\.claude` || len(w.Unmapped) != 0 {
		t.Fatalf("Windows, correspondance : %+v", w)
	}
	// transmis aux backends : le chemin Linux d'origine
	if r, _ := m.ToRemote(w.ClaudeConfigDir); r != "/share/MFS/ROOT/CLAUDE_PROJETS/.claude" {
		t.Errorf("ToRemote : %q", r)
	}
	if r, _ := m.ToRemote(w.Roots[0]); r != "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS" {
		t.Errorf("ToRemote racine : %q", r)
	}
	// sans correspondance : forme Linux gardée, signalée
	n := cfg
	n.Roots = append([]string(nil), cfg.Roots...)
	applyConfPaths("windows", PathMapper{}, &n)
	if n.Roots[0] != "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS" || n.ClaudeConfigDir != "/share/MFS/ROOT/CLAUDE_PROJETS/.claude" || len(n.Unmapped) != 2 {
		t.Fatalf("Windows, sans correspondance : %+v", n)
	}
	ls := strings.Join(unmappedLines("MSI", "B.yaml", n.Unmapped), "\n")
	if !strings.Contains(ls, `hubs: { MSI: { paths: [{ local: '\\wsl.localhost\<distribution>\share\MFS', remote: /share/MFS }] } }`) || strings.Count(ls, "B.yaml") != 1 {
		t.Errorf("avertissement :\n%s", ls)
	}
	if unmappedLines("MSI", "B.yaml", nil) != nil {
		t.Error("avertissement sans chemin")
	}
	// hors Windows : rien ne change
	l := cfg
	l.Roots = append([]string(nil), cfg.Roots...)
	applyConfPaths("linux", m, &l)
	if l.Roots[0] != cfg.Roots[0] || l.ClaudeConfigDir != cfg.ClaudeConfigDir || l.Unmapped != nil {
		t.Errorf("Linux : %+v", l)
	}
}

// configFromLines : racines et CLAUDE_CONFIG_DIR des lignes de confLite.
func configFromLines(s string) Config {
	var c Config
	for _, l := range strings.Split(s, "\n") {
		k, v, _ := strings.Cut(l, "\t")
		switch k {
		case "root":
			c.Roots = append(c.Roots, v)
		case "claudedir":
			c.ClaudeConfigDir = v
		}
	}
	return c
}

// backend host : implicite masqué sous Windows, déclaré gardé (et refusé)
func TestImplicitHost(t *testing.T) {
	for _, c := range []struct {
		goos     string
		declared bool
		shown    bool
	}{{"linux", false, true}, {"linux", true, true}, {"windows", false, false}, {"windows", true, true}} {
		bs := map[string]BackendConf{"k": {Type: "k8s"}}
		if c.declared {
			bs["host"] = BackendConf{Type: "host"}
		}
		addImplicitHost(c.goos, bs)
		var list []BackendConf
		for n, b := range bs {
			b.Name = n
			list = append(list, b)
		}
		if _, ok := bs["host"]; !ok {
			t.Errorf("%+v : host absent de la liste chargée", c)
		}
		shown := false
		for _, b := range Shown(list) {
			shown = shown || b.Name == "host"
		}
		if shown != c.shown {
			t.Errorf("%+v : affiché %v", c, shown)
		}
	}
}

// contexte k8s inconnu sous Windows : conseil kubeconfig ; Linux inchangé
func TestK8sReachFix(t *testing.T) {
	msg := `error: context "k8s" does not exist`
	if f := k8sReachFix("linux", msg); f != "vérifier kubectl config get-contexts / ~/.kube/config" {
		t.Errorf("linux : %q", f)
	}
	if f := k8sReachFix("windows", msg); !strings.Contains(f, `%USERPROFILE%\.kube\config`) || !strings.Contains(f, "KUBECONFIG") {
		t.Errorf("windows : %q", f)
	}
	if f := k8sReachFix("windows", "dial tcp: i/o timeout"); strings.Contains(f, "USERPROFILE") {
		t.Errorf("windows, autre erreur : %q", f)
	}
}

// serveur web : le launcher qui tient le port dit son système ; sous Windows,
// celui du hub Linux (localhost partagé par WSL) n'est pas pris pour le sien
func TestBrowserServerHolder(t *testing.T) {
	for _, c := range []struct {
		me, holder string
		foreign    bool
	}{{"linux", "linux", false}, {"linux", "", false}, {"windows", "windows", false},
		{"windows", "linux", true}, {"windows", "", true}, {"linux", "windows", true}} {
		who, f := foreignHolder(c.me, c.holder)
		if f != c.foreign || f && who == "" {
			t.Errorf("%s / %q : %v %q", c.me, c.holder, f, who)
		}
	}
	if who, _ := foreignHolder("windows", "linux"); !strings.Contains(who, "hub linux") {
		t.Errorf("libellé : %q", who)
	}
	srv := httptest.NewServer(fileServer{hosts: map[string]bool{"localhost": true}, roots: func() []string { return nil }})
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		t.Fatal(err)
	}
	v, hos, err := probeBrowserServerOS(serverConf{Listen: addr})
	if err != nil || !strings.HasPrefix(v, NgName) || hos != runtime.GOOS {
		t.Fatalf("sonde : %q %q %v", v, hos, err)
	}
	if v2, err := probeBrowserServer(serverConf{Listen: addr}); err != nil || v2 != v {
		t.Errorf("probeBrowserServer : %q %v", v2, err)
	}
	// un launcher antérieur (sans en-tête) : système inconnu
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(NgName + " v5.10.0\n"))
	}))
	defer old.Close()
	if _, hos, err := probeBrowserServerOS(serverConf{Listen: strings.TrimPrefix(old.URL, "http://")}); err != nil || hos != "" {
		t.Errorf("antérieur : %q %v", hos, err)
	}
}

// Windows réel (CI) : config partagée aux chemins Linux, correspondance de la
// machine déclarée → racines et CLAUDE_CONFIG_DIR locaux ; sans elle, chemins
// Linux gardés et signalés ; host implicite masqué.
func TestWindowsSharedConfigLoad(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows seulement (simulé ailleurs par TestSharedConfigOnWindows)")
	}
	defer func() { pathsGOOS = "" }()
	pathsGOOS = "windows"
	h := withHome(t)
	dir := filepath.Join(h, "cfg")
	_ = os.MkdirAll(dir, 0o755)
	t.Setenv("CL_CONFIG_DIR", dir)
	t.Setenv("CL_ROOTS", "")
	_ = os.WriteFile(filepath.Join(dir, "config.conf"), []byte(sharedConfigConf), 0o644)
	y := strings.Replace(sharedBackendsYAML, "  MSI:", "  "+hubName()+":", 1)
	_ = os.WriteFile(filepath.Join(dir, "backends.yaml"), []byte(y), 0o644)
	cfg := LoadConfig()
	if len(cfg.Roots) != 1 || cfg.Roots[0] != wslMFS+`\ROOT\CLAUDE_PROJETS\PROJETS` || cfg.ClaudeConfigDir != wslMFS+`\ROOT\CLAUDE_PROJETS\.claude` || len(cfg.Unmapped) != 0 {
		t.Fatalf("avec correspondance : %+v", cfg)
	}
	if RemotePath(cfg.ClaudeConfigDir) != "/share/MFS/ROOT/CLAUDE_PROJETS/.claude" {
		t.Errorf("RemotePath : %q", RemotePath(cfg.ClaudeConfigDir))
	}
	bs, err := LoadBackends()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range Shown(bs) {
		if b.Name == "host" {
			t.Error("host implicite affiché sous Windows")
		}
	}
	// sans correspondance
	y = strings.Replace(y, "    paths:\n      - { local: '\\\\wsl.localhost\\Ubuntu\\share\\MFS', remote: /share/MFS }\n", "", 1)
	_ = os.WriteFile(filepath.Join(dir, "backends.yaml"), []byte(y+"\n"), 0o644)
	hubCache.Lock()
	hubCache.e.loaded = false
	hubCache.Unlock()
	cfg = LoadConfig()
	if cfg.Roots[0] != "/share/MFS/ROOT/CLAUDE_PROJETS/PROJETS" || cfg.ClaudeConfigDir != "/share/MFS/ROOT/CLAUDE_PROJETS/.claude" || len(cfg.Unmapped) != 2 {
		t.Fatalf("sans correspondance : %+v", cfg)
	}
}
