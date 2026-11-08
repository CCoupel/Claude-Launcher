package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if a != "pro-ansible-secagent" || b != "other-ansible-secagent" {
		t.Errorf("ctrName : %q / %q", a, b)
	}
	if k8sPod("/share/CLAUDE_LAUNCHER/Claude-Launcher") != "claude-launcher-claude-launcher-0" {
		t.Errorf("k8sPod : %q", k8sPod("/share/CLAUDE_LAUNCHER/Claude-Launcher"))
	}
	// StatefulSet : son nom + « -<hash de révision> » (label du pod) tient en 63
	if n := ctrName("/" + strings.Repeat("catégorie ", 5) + "/" + strings.Repeat("très-long nom ", 10)); len(n) > 51 || strings.Contains(n, " ") {
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
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  k: {type: lxc}\n"), 0o644)
	if _, err := loadBackends(); err == nil {
		t.Error("type inconnu accepté")
	}
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  lab: {type: k8s, context: k8s, namespace: ns, volumes: {/share/MFS/ROOT: pvc-a}}\n"), 0o644)
	if bs, err := loadBackends(); err != nil || len(bs) != 2 || strings.Join(bs[1].Shared, ",") != "/share/MFS/ROOT" || !bs[1].remote() {
		t.Errorf("k8s : %v %+v", err, bs)
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
	if b := projectBackend(proj); b.Name != "host" || !assigned(proj) { // host explicite : plus de prefer
		t.Errorf("affectation host : %s", b.Name)
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
	want := append(append([]string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, sshMux()...), "srv1",
		`'podman' 'exec' '-i' 'ctr' 'sh' '-c' 'echo '\''a b'\'' $HOME'`)
	if len(sshMux()) != 6 || !strings.Contains(strings.Join(sshMux(), " "), "ControlMaster=auto") {
		t.Errorf("connexion ssh persistante : %q", sshMux())
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ssh cli :\n %q\n %q", got, want)
	}
	if a := ssh.execArgv("ctr", false, true, "tmux", "attach"); a[5+len(sshMux())] != "-t" {
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
	if got := sd.cli("ps")[6+len(sshMux())]; got != "'docker' '-H' 'unix:///run/user/1000/docker.sock' 'ps'" || sd.podman() {
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

func TestK8sArgv(t *testing.T) {
	e := Engine{BackendConf{Name: "lab", Type: "k8s", Context: "k8s", Namespace: "ns", Volumes: map[string]string{"/share/MFS/ROOT": "pvc-a"}}}
	a := e.execArgv("clx-p-1-0", true, false, "sh", "-s")
	if strings.Join(a, " ") != "kubectl --context k8s -n ns exec -i clx-p-1-0 -c main -- sh -s" {
		t.Errorf("exec root : %q", a)
	}
	u := e.execArgv("clx-p-1-0", false, true, "tmux", "attach", "-t", "=cl")
	if j := strings.Join(u, " "); !strings.Contains(j, "exec -i -t clx-p-1-0 -c main -- sh -c if command -v setpriv") ||
		!strings.Contains(j, "exec setpriv --reuid=") || !strings.Contains(j, "; exec su -s /bin/sh") || u[len(u)-1] != "exec env LANG=C.UTF-8 TERM=tmux-256color 'tmux' 'attach' '-t' '=cl'" {
		t.Errorf("exec utilisateur : %q", u)
	}
	// sans terminal : su (pas de SIGWINCH à recevoir)
	if r := e.execArgv("clx-p-1-0", false, false, "true"); !strings.Contains(strings.Join(r, " "), "-- sh -c exec su -s /bin/sh") {
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

func TestExecChoice(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  a-pod: {type: podman}\n  b-dock: {type: docker}\ndefaults: {backend: b-dock}\nimages: [docker.io/library/debian:12, quay.io/x/y:1]\n"), 0o644)
	if b, ok := defaultCtrBackend(); !ok || b.Name != "b-dock" {
		t.Errorf("defaults.backend : %+v", b)
	}
	if im := baseImages(); len(im) != 2 || imageLabel(im[0]) != "debian:12" || imageLabel(im[1]) != "quay.io/x/y:1" {
		t.Errorf("images : %v", im)
	}
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(p, 0o755)
	if err := applyExec(p, newForm{exec: 0, images: baseImages(), ctrBe: "b-dock"}); err != nil || hasEnv(p) || !assigned(p) || projectBackend(p).Name != "host" {
		t.Errorf("hôte : env.yaml créé ou affectation absente (%v)", err)
	}
	if err := applyExec(p, newForm{exec: 1, images: baseImages(), ctrBe: "b-dock"}); err != nil {
		t.Fatal(err)
	}
	if e, err := loadEnv(p); err != nil || e.Image != "docker.io/library/debian:12" || projectBackend(p).Name != "b-dock" {
		t.Errorf("socle : %v %+v %s", err, e, projectBackend(p).Name)
	}
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  a-pod: {type: podman}\n"), 0o644)
	if b, _ := defaultCtrBackend(); b.Name != "a-pod" || len(baseImages()) != len(defaultImages) {
		t.Errorf("défauts : %s %v", b.Name, baseImages())
	}
}

func TestExecDefaultsPreselect(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	write := func(s string) { _ = os.WriteFile(backendsPath(), []byte(s), 0o644) }
	write("backends:\n  pod: {type: podman}\n  lab: {type: k8s, volumes: {/x: p}}\ndefaults: {backend: lab, image: docker.io/library/rockylinux:9}\n")
	f, ok := execForm(newForm{})
	if !ok || f.ctrBe != "lab" || f.exec == 0 || f.images[f.exec-1] != "docker.io/library/rockylinux:9" {
		t.Errorf("présélection lab/rocky : %+v", f)
	}
	f.cycleCtrBe(1)
	if f.ctrBe != "pod" {
		t.Errorf("←→ : %s", f.ctrBe)
	}
	write("backends:\n  pod: {type: podman}\ndefaults: {backend: host, image: quay.io/a/b:2}\n")
	if f, _ := execForm(newForm{}); f.exec != 0 || f.images[0] != "quay.io/a/b:2" {
		t.Errorf("présélection hôte, image hors liste en tête : %+v", f)
	}
	write("backends:\n  pod: {type: podman}\ndefaults: {backend: pod}\n")
	if f, _ := execForm(newForm{}); f.exec != 1 || f.images[0] != defaultImages[0] {
		t.Errorf("backend sans image : premier socle : %+v", f)
	}
	write("backends: {}\n")
	if _, ok := execForm(newForm{}); ok && len(detectedEngines()) == 0 {
		t.Error("aucun backend conteneur : pas d'étape Exécution")
	}
}

func TestReconfigure(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  pod: {type: podman}\n  lab: {type: k8s, volumes: {/x: p}}\ndefaults: {backend: pod}\n"), 0o644)
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
	_ = os.WriteFile(envPath(p), []byte("# commentaire gardé\nimage: docker.io/library/debian:12\npackages: [jq]\n"), 0o644)
	_ = writeAssignment(p, "lab")
	m := model{}.startExecChoice(Node{Path: p, Name: "proj"}, false)
	f := m.form
	if f.ctrBe != "lab" || f.exec == 0 || f.images[f.exec-1] != "docker.io/library/debian:12" {
		t.Fatalf("préremplissage : %+v", f)
	}
	f.exec = indexOf(f.images, "docker.io/library/alpine:3.20") + 1
	f.ctrBe = "pod"
	if err := applyExec(p, f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(envPath(p))
	if !strings.Contains(string(b), "image: docker.io/library/alpine:3.20") || !strings.Contains(string(b), "# commentaire gardé") || !strings.Contains(string(b), "packages: [jq]") || projectBackend(p).Name != "pod" {
		t.Errorf("reconfiguration : %s / %s", b, projectBackend(p).Name)
	}
	f.exec = 0
	_ = applyExec(p, f)
	if projectBackend(p).Name != "host" || !hasEnv(p) {
		t.Error("retour à l'hôte : env.yaml doit être gardé")
	}
	if m := (model{}).startExecChoice(Node{Path: p, Name: "proj"}, false); m.form.exec != 0 {
		t.Errorf("projet sur l'hôte : « hôte » attendu, %+v", m.form)
	}
}

// Un projet configuré se lance sur son backend, jamais ailleurs.
func TestLaunchBackendNoFallback(t *testing.T) {
	h := withHome(t)
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	_ = os.WriteFile(backendsPath(), []byte("backends:\n  pod: {type: podman}\n"), 0o644)
	p := filepath.Join(h, "proj")
	_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
	_ = os.WriteFile(envPath(p), []byte("image: docker.io/library/debian:12\n"), 0o644)

	_ = writeAssignment(p, "pod")
	if b, err := launchBackend(p); err != nil || b.Name != "pod" {
		t.Errorf("backend configuré : %v %s", err, b.Name)
	}
	_ = writeAssignment(p, "host")
	if b, err := launchBackend(p); err != nil || b.Name != "host" {
		t.Errorf("hôte configuré (env.yaml présent) : %v %s", err, b.Name)
	}
	_ = writeAssignment(p, "disparu")
	if _, err := launchBackend(p); err == nil || !strings.Contains(err.Error(), "F2") {
		t.Errorf("backend retiré de backends.yaml : refus attendu, %v", err)
	}
	_ = writeAssignment(p, "pod")
	_ = os.Remove(envPath(p))
	if _, err := launchBackend(p); err == nil || !strings.Contains(err.Error(), "env.yaml") {
		t.Errorf("env.yaml disparu : refus attendu, %v", err)
	}
	_ = os.WriteFile(envPath(p), []byte("packages: [x]\n"), 0o644)
	if _, err := launchBackend(p); err == nil {
		t.Error("env.yaml invalide : refus attendu")
	}
	q := filepath.Join(h, "neuf") // jamais configuré : pas d'erreur (le formulaire a été proposé avant)
	_ = os.MkdirAll(q, 0o755)
	if b, err := launchBackend(q); err != nil || b.Name != "host" {
		t.Errorf("jamais configuré : %v %s", err, b.Name)
	}
}

func TestOwner(t *testing.T) {
	now := time.Now().Unix()
	o := parseOwner(fmt.Sprintf("MSI:1000:claude-ng2|%d", now-5))
	if o.id != "MSI:1000:claude-ng2" || !o.alive() {
		t.Errorf("propriétaire récent : %+v", o)
	}
	if parseOwner(fmt.Sprintf("x|%d", now-60)).alive() || parseOwner("|").alive() || parseOwner("").alive() {
		t.Error("propriétaire parti ou absent vu vivant")
	}
	t.Setenv("CL_INNER_SOCKET", "t-x")
	if id := launcherID(); !strings.HasSuffix(id, ":t-x") || !strings.HasPrefix(id, hostName()+":") {
		t.Errorf("launcherID : %s", id)
	}
}

func TestClaudeConfigDirFromConfig(t *testing.T) {
	d := t.TempDir()
	cfgDir := filepath.Join(d, "mfs", ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(cfgDir, 0o755)
	t.Setenv("CL_CONFIG_DIR", cfgDir)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte(`CLAUDE_CONFIG_DIR="../../.claude"`+"\n"), 0o644)
	if got, want := loadConfig().ClaudeConfigDir, filepath.Join(d, "mfs", ".claude"); got != want {
		t.Errorf("relatif : %s, attendu %s", got, want)
	}
	_ = os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte(`CLAUDE_CONFIG_DIR="/share/x/.claude"`+"\n"), 0o644)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	applyClaudeConfigDir()
	if os.Getenv("CLAUDE_CONFIG_DIR") != "/share/x/.claude" || claudeDir() != "/share/x/.claude" {
		t.Errorf("appliqué : %q", os.Getenv("CLAUDE_CONFIG_DIR"))
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/env/gagne")
	applyClaudeConfigDir()
	if os.Getenv("CLAUDE_CONFIG_DIR") != "/env/gagne" || loadConfig().ClaudeConfigDir != "/share/x/.claude" {
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
	if got := readAssignments()["/p/a"]; got != "pod" {
		t.Fatalf("migration : %q, attendu pod", got)
	}
	if want := filepath.Join(h, ".config", "claude-launcher-ng2", "projects.json"); assignPath() != want {
		t.Fatalf("assignPath = %s, attendu %s", assignPath(), want)
	}
	if _, err := os.Stat(assignPath()); err != nil {
		t.Fatal("projects.json non écrit dans le dossier de config")
	}
	if err := writeAssignment("/p/b", "host"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(legacy, []byte(`{"/p/a": "autre"}`), 0o644) // ignoré désormais
	m := readAssignments()
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
	a, err := e.runArgs(ctrName(proj), proj, EnvSpec{Image: "debian:12"})
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
	old := localCtrHosts
	defer func() { localCtrHosts = old }()
	localCtrHosts = func() map[string]bool { return map[string]bool{"mon-projet": true} }
	if leaderAliveIn(42, "mon-projet#pid:[1]") != leaderDead {
		t.Error("conteneur local arrêté : le leader doit être mort")
	}
	if leaderAliveIn(42, "autre-projet#pid:[1]") != leaderUnknown {
		t.Error("projet inconnu des moteurs locaux : rien à conclure")
	}
}
