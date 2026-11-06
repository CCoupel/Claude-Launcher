package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	a := ctrName("/mnt/c/PROJETS/PRO/Ansible_SecAgent")
	b := ctrName("/mnt/c/PROJETS/OTHER/Ansible_SecAgent")
	if !strings.HasPrefix(a, "cl-ansible-secagent-") || a == b || len(a) > 63 {
		t.Errorf("ctrName : %q / %q", a, b)
	}
	if n := ctrName("/x/" + strings.Repeat("très-long nom ", 10)); len(n) > 63 || strings.Contains(n, " ") {
		t.Errorf("ctrName long : %q", n)
	}
}

func withHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return h
}

func TestBackendsAndAssignment(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  pod: {type: podman}\n  dock: {type: docker, context: x}\n"), 0o644)
	bs, err := loadBackends()
	if err != nil || len(bs) != 3 || bs[0].Name != "host" || bs[1].Name != "dock" || bs[2].Name != "pod" {
		t.Fatalf("loadBackends : %v %+v", err, bs)
	}
	if e, _ := engineOf(bs[1]); strings.Join(e.cli("ps"), " ") != "docker --context x ps" {
		t.Errorf("cli docker : %v", e.cli("ps"))
	}
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  k: {type: k8s}\n"), 0o644)
	if _, err := loadBackends(); err == nil {
		t.Error("type k8s accepté")
	}
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644)

	proj := filepath.Join(h, "p")
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	if b := projectBackend(proj); b.Name != "host" {
		t.Errorf("sans env.yaml : %s", b.Name)
	}
	_ = os.WriteFile(envPath(proj), []byte("image: debian:12\nprefer: pod\npackages: [jq]\nsetup: ['echo ok']\n"), 0o644)
	if b := projectBackend(proj); b.Name != "pod" {
		t.Errorf("prefer : %s", b.Name)
	}
	if err := writeAssignment(proj, "host"); err != nil {
		t.Fatal(err)
	}
	if b := projectBackend(proj); b.Name != "pod" { // host = pas d'entrée → prefer
		t.Errorf("affectation host puis prefer : %s", b.Name)
	}
	_ = os.WriteFile(envPath(proj), []byte("image: debian:12\n"), 0o644)
	_ = writeAssignment(proj, "pod")
	if b := projectBackend(proj); b.Name != "pod" {
		t.Errorf("affectation : %s", b.Name)
	}
	e, err := loadEnv(proj)
	if err != nil || e.Image != "debian:12" || e.hash() == "" {
		t.Errorf("loadEnv : %v %+v", err, e)
	}
	_ = os.WriteFile(envPath(proj), []byte("packages: [x]\n"), 0o644)
	if _, err := loadEnv(proj); err == nil {
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
	host := hostPidSpace()
	if leaderAliveIn(os.Getpid(), host) != leaderAlive || leaderAliveIn(os.Getpid(), "") != leaderAlive {
		t.Error("PID du launcher non vu vivant")
	}
	if leaderAliveIn(1<<30, host) != leaderDead {
		t.Error("PID inexistant vu vivant")
	}
	if leaderAliveIn(os.Getpid(), hostName()+"#pid:[1]") != leaderDead {
		t.Error("namespace d'un démarrage précédent : le leader doit être mort")
	}
	if leaderAliveIn(42, "autre-machine#pid:[1]") != leaderUnknown {
		t.Error("autre machine : rien à conclure")
	}
	if leaderAliveIn(42, "cl-projet-1234#pid:[1]") != leaderDead {
		t.Error("conteneur NG2 local arrêté : le leader doit être mort")
	}
}

func TestRemoteEngines(t *testing.T) {
	ssh := Engine{BackendConf{Name: "srv", Type: "ssh", Host: "srv1", Shared: []string{"/share/MFS/ROOT"}}}
	got := ssh.cli("exec", "-i", "ctr", "sh", "-c", "echo 'a b' $HOME")
	want := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "srv1",
		`'podman' 'exec' '-i' 'ctr' 'sh' '-c' 'echo '\''a b'\'' $HOME'`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ssh cli :\n %q\n %q", got, want)
	}
	if a := ssh.execArgv("ctr", false, true, "tmux", "attach"); a[5] != "-t" {
		t.Errorf("attache ssh sans -t : %q", a)
	}
	if !ssh.podman() || !ssh.b.remote() {
		t.Error("ssh : podman distant attendu")
	}
	dk := Engine{BackendConf{Type: "docker", Host: "tcp://192.168.1.211:2375"}}
	if strings.Join(dk.cli("ps"), " ") != "docker -H tcp://192.168.1.211:2375 ps" || !dk.b.remote() || dk.podman() {
		t.Errorf("docker distant : %v", dk.cli("ps"))
	}
	if (BackendConf{Type: "docker"}).remote() || (BackendConf{Type: "docker", Host: "unix:///run/docker.sock"}).remote() {
		t.Error("docker local vu distant")
	}
	sd := Engine{BackendConf{Type: "ssh", Host: "srv1", Engine: "docker", EngHost: "unix:///run/user/1000/docker.sock"}}
	if got := sd.cli("ps")[6]; got != "'docker' '-H' 'unix:///run/user/1000/docker.sock' 'ps'" || sd.podman() {
		t.Errorf("ssh+docker : %q", got)
	}
	pd := Engine{BackendConf{Type: "podman", Host: "tcp://127.0.0.1:8888"}}
	if strings.Join(pd.cli("ps"), " ") != "podman --url tcp://127.0.0.1:8888 ps" || !pd.podman() || !pd.b.remote() {
		t.Errorf("podman direct : %v", pd.cli("ps"))
	}
	b := ssh.b
	if !underShared(b, "/share/MFS/ROOT/x/y") || underShared(b, "/share/MFS/ROOTX") || underShared(b, "/mnt/c/p") {
		t.Error("underShared")
	}
	if ps := sharedProblems(BackendConf{Name: "x", Type: "ssh", Host: "h"}, ""); len(ps) != 1 || !strings.Contains(ps[0].fix, "shared") {
		t.Errorf("shared manquant : %v", ps)
	}
	if ps := sharedProblems(BackendConf{Type: "podman"}, "/mnt/c/p"); ps != nil {
		t.Errorf("local : pas de prérequis de partage : %v", ps)
	}
}

func TestSSHBackendNeedsHost(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  srv: {type: ssh}\n"), 0o644)
	if _, err := loadBackends(); err == nil {
		t.Error("backend ssh sans host accepté")
	}
}

func TestLeaderRemoteContainerUnknown(t *testing.T) {
	if leaderAliveIn(42, "clx-projet-1234#pid:[1]") != leaderUnknown {
		t.Error("conteneur distant : rien à conclure de l'absence du namespace ici")
	}
}
