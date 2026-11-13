package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/platform"
)

func TestTmuxQuote(t *testing.T) {
	cases := map[string]string{
		"list-panes":   "'list-panes'",
		"#{pane_id}":   "'#{pane_id}'",
		"a;b":          "'a;b'",
		"l'été":        `"l'été"`,
		`a"b$c\d~`:     `'a"b$c\d~'`,
		`it's "$HOME"`: `"it's \"\$HOME\""`,
		"x\ny":         `"x\ny"`,
	}
	for in, want := range cases {
		if got := tmuxQuote(in); got != want {
			t.Errorf("tmuxQuote(%q) = %s, attendu %s", in, got, want)
		}
	}
	if got := splitCommands([]string{"display-message", "-p", "x", ";", "capture-pane", "-p"}); len(got) != 2 || ctlLine(got[1]) != "'capture-pane' '-p'" {
		t.Errorf("splitCommands : %q", got)
	}
}

func TestCtrName(t *testing.T) {
	a := CtrName("/mnt/c/PROJETS/PRO/Ansible_SecAgent")
	b := CtrName("/mnt/c/PROJETS/OTHER/Ansible_SecAgent")
	if a != "pro-ansible-secagent" || b != "other-ansible-secagent" {
		t.Errorf("ctrName : %q / %q", a, b)
	}
	if k8sPod("/share/CLAUDE_LAUNCHER/Claude-Launcher") != "claude-launcher-claude-launcher-0" {
		t.Errorf("k8sPod : %q", k8sPod("/share/CLAUDE_LAUNCHER/Claude-Launcher"))
	}
	// StatefulSet : son nom + « -<hash de révision> » (label du pod) tient en 63
	if n := CtrName("/" + strings.Repeat("catégorie ", 5) + "/" + strings.Repeat("très-long nom ", 10)); len(n) > 51 || strings.Contains(n, " ") {
		t.Errorf("ctrName long : %q (%d)", n, len(n))
	}
	if l := legacyCtrName("/mnt/c/PROJETS/PRO/Ansible_SecAgent"); !strings.HasPrefix(l, "cl-ansible-secagent-") || len(l) != len("cl-ansible-secagent-")+8 {
		t.Errorf("legacyCtrName : %q", l)
	}
	if l := legacyK8sName("/p/x"); !strings.HasPrefix(l, "clx-x-") {
		t.Errorf("legacyK8sName : %q", l)
	}
}

func withHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	winHome(t, h)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return h
}

func TestBackendsAndAssignment(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  pod: {type: podman}\n  dock: {type: docker, context: x}\n"), 0o644)
	bs, err := LoadBackends()
	if err != nil || len(bs) != 3 || bs[0].Name != "host" || bs[1].Name != "dock" || bs[2].Name != "pod" {
		t.Fatalf("loadBackends : %v %+v", err, bs)
	}
	if e, _ := EngineOf(bs[1]); strings.Join(e.cli("ps"), " ") != "docker --context x ps" {
		t.Errorf("cli docker : %v", e.cli("ps"))
	}
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  k: {type: lxc}\n"), 0o644)
	if _, err := LoadBackends(); err == nil {
		t.Error("type inconnu accepté")
	}
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  lab: {type: k8s, context: k8s, namespace: ns, volumes: {/share/MFS/ROOT: pvc-a}}\n"), 0o644)
	if bs, err := LoadBackends(); err != nil || len(bs) != 2 || strings.Join(bs[1].Shared, ",") != "/share/MFS/ROOT" || !bs[1].remote() {
		t.Errorf("k8s : %v %+v", err, bs)
	}
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644)

	proj := filepath.Join(h, "p")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	if b := ProjectBackend(proj); b.Name != "host" {
		t.Errorf("sans env.yaml : %s", b.Name)
	}
	_ = os.WriteFile(EnvPath(proj), []byte("image: debian:12\nprefer: pod\npackages: [jq]\nsetup: ['echo ok']\n"), 0o644)
	if b := ProjectBackend(proj); b.Name != "pod" {
		t.Errorf("prefer : %s", b.Name)
	}
	if err := WriteAssignment(proj, "host"); err != nil {
		t.Fatal(err)
	}
	if b := ProjectBackend(proj); b.Name != "host" || !Assigned(proj) { // host explicite : plus de prefer
		t.Errorf("affectation host : %s", b.Name)
	}
	_ = os.WriteFile(EnvPath(proj), []byte("image: debian:12\n"), 0o644)
	_ = WriteAssignment(proj, "pod")
	if b := ProjectBackend(proj); b.Name != "pod" {
		t.Errorf("affectation : %s", b.Name)
	}
	e, err := LoadEnv(proj)
	if err != nil || e.Image != "debian:12" || e.hash() == "" {
		t.Errorf("loadEnv : %v %+v", err, e)
	}
	_ = os.WriteFile(EnvPath(proj), []byte("packages: [x]\n"), 0o644)
	if _, err := LoadEnv(proj); err == nil {
		t.Error("env.yaml sans image accepté")
	}
}

func TestEnsureSharedConfigCopies(t *testing.T) {
	h := withHome(t)
	_ = os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"a":1}`), 0o600)
	if err := ensureSharedConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(h, ".claude", ".claude.json"))
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("copie : %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(h, ".claude.json")); err != nil {
		t.Error("~/.claude.json déplacé : il doit être copié, jamais déplacé")
	}
	_ = os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"a":2}`), 0o600)
	_ = ensureSharedConfig()
	if b, _ := os.ReadFile(filepath.Join(h, ".claude", ".claude.json")); string(b) != `{"a":1}` {
		t.Error("copie refaite alors qu'elle existait")
	}
}

func TestLeaderAliveIn(t *testing.T) {
	unixOnly(t)
	host := platform.HostPidSpace()
	if leaderAliveIn(os.Getpid(), host) != platform.LeaderAlive || leaderAliveIn(os.Getpid(), "") != platform.LeaderAlive {
		t.Error("PID du launcher non vu vivant")
	}
	if leaderAliveIn(1<<30, host) != platform.LeaderDead {
		t.Error("PID inexistant vu vivant")
	}
	if leaderAliveIn(os.Getpid(), platform.HostName()+"#pid:[1]") != platform.LeaderDead {
		t.Error("namespace d'un démarrage précédent : le leader doit être mort")
	}
	if leaderAliveIn(42, "autre-machine#pid:[1]") != platform.LeaderUnknown {
		t.Error("autre machine : rien à conclure")
	}
	if leaderAliveIn(42, "cl-projet-1234#pid:[1]") != platform.LeaderDead {
		t.Error("conteneur NG2 local arrêté : le leader doit être mort")
	}
}

func TestRemoteEngines(t *testing.T) {
	ssh := Engine{BackendConf{Name: "srv", Type: "ssh", Host: "srv1", Shared: []string{"/share/MFS/ROOT"}}}
	got := ssh.cli("exec", "-i", "ctr", "sh", "-c", "echo 'a b' $HOME")
	want := append(append([]string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, sshMux()...), "srv1",
		`'podman' 'exec' '-i' 'ctr' 'sh' '-c' 'echo '\''a b'\'' $HOME'`)
	if len(sshMux()) != 6 || !strings.Contains(strings.Join(sshMux(), " "), "ControlMaster=auto") {
		t.Errorf("connexion ssh persistante : %q", sshMux())
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ssh cli :\n %q\n %q", got, want)
	}
	if a := ssh.ExecArgv("ctr", false, true, "tmux", "attach"); a[5+len(sshMux())] != "-t" {
		t.Errorf("attache ssh sans -t : %q", a)
	}
	if !ssh.podman() || !ssh.B.remote() {
		t.Error("ssh : podman distant attendu")
	}
	dk := Engine{BackendConf{Type: "docker", Host: "tcp://192.168.1.211:2375"}}
	if strings.Join(dk.cli("ps"), " ") != "docker -H tcp://192.168.1.211:2375 ps" || !dk.B.remote() || dk.podman() {
		t.Errorf("docker distant : %v", dk.cli("ps"))
	}
	if (BackendConf{Type: "docker"}).remote() || (BackendConf{Type: "docker", Host: "unix:///run/docker.sock"}).remote() {
		t.Error("docker local vu distant")
	}
	sd := Engine{BackendConf{Type: "ssh", Host: "srv1", Engine: "docker", EngHost: "unix:///run/user/1000/docker.sock"}}
	if got := sd.cli("ps")[6+len(sshMux())]; got != "'docker' '-H' 'unix:///run/user/1000/docker.sock' 'ps'" || sd.podman() {
		t.Errorf("ssh+docker : %q", got)
	}
	pd := Engine{BackendConf{Type: "podman", Host: "tcp://127.0.0.1:8888"}}
	if strings.Join(pd.cli("ps"), " ") != "podman --url tcp://127.0.0.1:8888 ps" || !pd.podman() || !pd.B.remote() {
		t.Errorf("podman direct : %v", pd.cli("ps"))
	}
	b := ssh.B
	if !underShared(b, "/share/MFS/ROOT/x/y") || underShared(b, "/share/MFS/ROOTX") || underShared(b, "/mnt/c/p") {
		t.Error("underShared")
	}
	if ps := sharedProblems(BackendConf{Name: "x", Type: "ssh", Host: "h"}, ""); len(ps) != 1 || !strings.Contains(ps[0].Fix, "shared") {
		t.Errorf("shared manquant : %v", ps)
	}
	if ps := sharedProblems(BackendConf{Type: "podman"}, "/mnt/c/p"); ps != nil {
		t.Errorf("local : pas de prérequis de partage : %v", ps)
	}
}

func TestSSHBackendNeedsHost(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  srv: {type: ssh}\n"), 0o644)
	if _, err := LoadBackends(); err == nil {
		t.Error("backend ssh sans host accepté")
	}
}

func TestLeaderRemoteContainerUnknown(t *testing.T) {
	if leaderAliveIn(42, "clx-projet-1234#pid:[1]") != platform.LeaderUnknown {
		t.Error("conteneur distant : rien à conclure de l'absence du namespace ici")
	}
}

func TestK8sArgv(t *testing.T) {
	e := Engine{BackendConf{Name: "lab", Type: "k8s", Context: "k8s", Namespace: "ns", Volumes: map[string]string{"/share/MFS/ROOT": "pvc-a"}}}
	a := e.ExecArgv("clx-p-1-0", true, false, "sh", "-s")
	if strings.Join(a, " ") != "kubectl --context k8s -n ns exec -i clx-p-1-0 -c main -- sh -s" {
		t.Errorf("exec root : %q", a)
	}
	u := e.ExecArgv("clx-p-1-0", false, true, "tmux", "attach", "-t", "=cl")
	if j := strings.Join(u, " "); !strings.Contains(j, "exec -i -t clx-p-1-0 -c main -- sh -c if command -v setpriv") ||
		!strings.Contains(j, "exec setpriv --reuid=") || !strings.Contains(j, "; exec su -s /bin/sh") || u[len(u)-1] != "exec env LANG=C.UTF-8 TERM=tmux-256color 'tmux' 'attach' '-t' '=cl'" {
		t.Errorf("exec utilisateur : %q", u)
	}
	// sans terminal : su (pas de SIGWINCH à recevoir)
	if r := e.ExecArgv("clx-p-1-0", false, false, "true"); !strings.Contains(strings.Join(r, " "), "-- sh -c exec su -s /bin/sh") {
		t.Errorf("exec utilisateur sans tty : %q", r)
	}
	if k8sPod("/x/Demo Projet") != "x-demo-projet-0" {
		t.Errorf("nom de pod : %s", k8sPod("/x/Demo Projet"))
	}
	m, err := e.k8sManifest("/share/MFS/ROOT/p", EnvSpec{Image: "debian:12"})
	if err != nil || !strings.Contains(string(m), `"claimName":"pvc-a"`) || !strings.Contains(string(m), `"mountPath":"/share/MFS/ROOT"`) || !strings.Contains(string(m), `"cl.project":"/share/MFS/ROOT/p"`) {
		t.Errorf("manifeste : %v %s", err, m)
	}
}

// Un projet configuré se lance sur son backend, jamais ailleurs.
func TestLaunchBackendNoFallback(t *testing.T) {
	unixOnly(t)
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(BackendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
	_ = os.WriteFile(EnvPath(p), []byte("image: docker.io/library/debian:12\n"), 0o644)

	_ = WriteAssignment(p, "pod")
	if b, err := LaunchBackend(p); err != nil || b.Name != "pod" {
		t.Errorf("backend configuré : %v %s", err, b.Name)
	}
	_ = WriteAssignment(p, "host")
	if b, err := LaunchBackend(p); err != nil || b.Name != "host" {
		t.Errorf("hôte configuré (env.yaml présent) : %v %s", err, b.Name)
	}
	_ = WriteAssignment(p, "disparu")
	if _, err := LaunchBackend(p); err == nil || !strings.Contains(err.Error(), "F2") {
		t.Errorf("backend retiré de backends.yaml : refus attendu, %v", err)
	}
	_ = WriteAssignment(p, "pod")
	_ = os.Remove(EnvPath(p))
	if _, err := LaunchBackend(p); err == nil || !strings.Contains(err.Error(), "env.yaml") {
		t.Errorf("env.yaml disparu : refus attendu, %v", err)
	}
	_ = os.WriteFile(EnvPath(p), []byte("packages: [x]\n"), 0o644)
	if _, err := LaunchBackend(p); err == nil {
		t.Error("env.yaml invalide : refus attendu")
	}
	q := filepath.Join(h, "neuf") // jamais configuré : pas d'erreur (le formulaire a été proposé avant)
	_ = os.MkdirAll(q, 0o755)
	if b, err := LaunchBackend(q); err != nil || b.Name != "host" {
		t.Errorf("jamais configuré : %v %s", err, b.Name)
	}
}

func TestOwner(t *testing.T) {
	now := time.Now().Unix()
	o := ParseOwner(fmt.Sprintf("MSI:1000:claude-ng2|%d", now-5))
	if o.Id != "MSI:1000:claude-ng2" || !o.Alive() {
		t.Errorf("propriétaire récent : %+v", o)
	}
	if ParseOwner(fmt.Sprintf("x|%d", now-60)).Alive() || ParseOwner("|").Alive() || ParseOwner("").Alive() {
		t.Error("propriétaire parti ou absent vu vivant")
	}
	t.Setenv("CL_INNER_SOCKET", "t-x")
	if id := LauncherID(); !strings.HasSuffix(id, ":t-x") || !strings.HasPrefix(id, platform.HostName()+":") {
		t.Errorf("launcherID : %s", id)
	}
}

func TestClaudeConfigDirFromConfig(t *testing.T) {
	unixOnly(t)
	d := t.TempDir()
	cfgDir := filepath.Join(d, "mfs", ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(cfgDir, 0o755)
	t.Setenv("CL_CONFIG_DIR", cfgDir)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte(`CLAUDE_CONFIG_DIR="../../.claude"`+"\n"), 0o644)
	if got, want := LoadConfig().ClaudeConfigDir, filepath.Join(d, "mfs", ".claude"); got != want {
		t.Errorf("relatif : %s, attendu %s", got, want)
	}
	_ = os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte(`CLAUDE_CONFIG_DIR="/share/x/.claude"`+"\n"), 0o644)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	ApplyClaudeConfigDir()
	if os.Getenv("CLAUDE_CONFIG_DIR") != "/share/x/.claude" || ClaudeDir() != "/share/x/.claude" {
		t.Errorf("appliqué : %q", os.Getenv("CLAUDE_CONFIG_DIR"))
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/gagne")
	ApplyClaudeConfigDir()
	if os.Getenv("CLAUDE_CONFIG_DIR") != "/env/gagne" || LoadConfig().ClaudeConfigDir != "/share/x/.claude" {
		t.Error("une variable déjà définie doit l'emporter (et la config lue reste celle du fichier)")
	}
}

// v5.4.3 : projects.json passe du dossier d'état local au dossier de config
// (partagé) ; l'ancien fichier est repris une fois et laissé en place.
func TestAssignmentMigration(t *testing.T) {
	h := withHome(t)
	legacy := filepath.Join(h, "state", "claude-launcher-ng2", "projects.json")
	_ = os.MkdirAll(filepath.Dir(legacy), 0o755)
	_ = os.WriteFile(legacy, []byte(`{"/p/a": "pod"}`), 0o644)
	if got := ReadAssignments()["/p/a"]; got != "pod" {
		t.Fatalf("migration : %q, attendu pod", got)
	}
	if want := filepath.Join(h, ".config", "claude-launcher-ng2", "projects.json"); assignPath() != want {
		t.Fatalf("assignPath = %s, attendu %s", assignPath(), want)
	}
	if _, err := os.Stat(assignPath()); err != nil {
		t.Fatal("projects.json non écrit dans le dossier de config")
	}
	if err := WriteAssignment("/p/b", "host"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(legacy, []byte(`{"/p/a": "autre"}`), 0o644) // ignoré désormais
	m := ReadAssignments()
	if m["/p/a"] != "pod" || m["/p/b"] != "host" {
		t.Fatalf("affectations : %v", m)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("ancien fichier supprimé")
	}
}

// hostname : le nom du projet, pour le conteneur comme pour le pod (k8s)
func TestCtrHostIsProjectName(t *testing.T) {
	withHome(t)
	if h := ctrHost("/mnt/c/PROJETS/PRO/Ansible_SecAgent"); h != "ansible-secagent" {
		t.Errorf("ctrHost : %q", h)
	}
	if h := ctrHost("/x/___"); h != "projet" {
		t.Errorf("ctrHost vide : %q", h)
	}
	if h := ctrHost("/x/" + strings.Repeat("a", 29) + " b"); strings.HasSuffix(h, "-") || len(h) > 30 {
		t.Errorf("ctrHost tronqué : %q", h)
	}
	proj := t.TempDir()
	e := Engine{BackendConf{Name: "podman", Type: "podman"}}
	a, err := e.runArgs(CtrName(proj), proj, EnvSpec{Image: "debian:12"})
	if err != nil || !strings.Contains(strings.Join(a, " "), "--hostname "+ctrHost(proj)+" ") {
		t.Errorf("runArgs hostname : %v %q", err, a)
	}
	k := Engine{BackendConf{Name: "lab", Type: "k8s", Context: "k8s", Namespace: "ns", Volumes: map[string]string{"/share/MFS/ROOT": "pvc-a"}}}
	m, err := k.k8sManifest("/share/MFS/ROOT/Mon_Projet", EnvSpec{Image: "debian:12"})
	if err != nil || !strings.Contains(string(m), `"name":"root-mon-projet"`) {
		t.Errorf("manifeste : StatefulSet <catégorie>-<projet> attendu : %v %s", err, m)
	}
}

// conteneur local arrêté, hostname = nom du projet : reconnu par les moteurs locaux
func TestLeaderLocalStoppedByProjectHost(t *testing.T) {
	unixOnly(t)
	old := LocalCtrHosts
	defer func() { LocalCtrHosts = old }()
	LocalCtrHosts = func() map[string]bool { return map[string]bool{"mon-projet": true} }
	if leaderAliveIn(42, "mon-projet#pid:[1]") != platform.LeaderDead {
		t.Error("conteneur local arrêté : le leader doit être mort")
	}
	if leaderAliveIn(42, "autre-projet#pid:[1]") != platform.LeaderUnknown {
		t.Error("projet inconnu des moteurs locaux : rien à conclure")
	}
}
