package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseBackendsYAML(t *testing.T, s string) backendsFile {
	t.Helper()
	var f backendsFile
	if err := yaml.Unmarshal([]byte(s), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBrowserPathsDeclared(t *testing.T) {
	f := parseBackendsYAML(t, `
defaults:
  browser_paths:
    - { container: /share/MFS/ROOT/, host: '\\nas\MFS\ROOT\' }
    - { container: /share/MFS/ROOT/CLAUDE_PROJETS, host: 'P:\' }
    - { container: /data, host: /mnt/data/, os: macos }
    - { container: relatif, host: /x }
    - { container: /y, host: /y, os: beos }
    - { container: /z, host: 'z:\..\' , os: linux }
    - { container: /w, host: 'nas/partage' }
backends:
  podman: { type: podman }
`)
	ps, src, warns := resolveBrowserPaths(f, "pc-bureau", nil, false)
	if src != "defaults.browser_paths" {
		t.Fatalf("source = %q", src)
	}
	want := []BrowserPath{
		{"/share/MFS/ROOT/CLAUDE_PROJETS", `P:`, "windows"},
		{"/share/MFS/ROOT", `\\nas\MFS\ROOT`, "windows"},
		{"/data", "/mnt/data", "linux"},
		{"/z", `z:\..`, "linux"},
	}
	if fmt.Sprint(ps) != fmt.Sprint(want) {
		t.Fatalf("\n got %+v\nwant %+v", ps, want)
	}
	got := browserPathJSON(ps)
	if strings.Contains(got, "\n") || !strings.HasPrefix(got, `[{"container":"/share/MFS/ROOT/CLAUDE_PROJETS","host":"P:","os":"windows"}`) {
		t.Fatalf("JSON : %s", got)
	}
	if len(warns) != 3 { // relatif, os inconnu, host sans forme absolue
		t.Fatalf("avertissements = %q", warns)
	}
	var back []BrowserPath // le template relit un tableau JSON standard
	if err := json.Unmarshal([]byte(got), &back); err != nil || back[1].Host != `\\nas\MFS\ROOT` {
		t.Fatalf("relecture : %v %+v", err, back)
	}
}

// hubs.<machine> l'emporte (nom court ou complet, casse ignorée) ; une liste
// vide déclarée est gardée telle quelle : « [] », pas le défaut.
func TestBrowserPathsHubOverride(t *testing.T) {
	f := parseBackendsYAML(t, `
defaults:
  browser_paths: [{ container: /share/MFS/ROOT, host: '\\nas\MFS\ROOT' }]
hubs:
  Portable:
    browser_paths: [{ container: /share/MFS/ROOT, host: /mnt/mfs/ROOT }]
  serveur:
    browser_paths: []
`)
	ps, src, _ := resolveBrowserPaths(f, "portable.maison.lan", nil, false)
	if src != "hubs.Portable.browser_paths" || len(ps) != 1 || ps[0].Host != "/mnt/mfs/ROOT" || ps[0].OS != "linux" {
		t.Fatalf("portable : %s %+v", src, ps)
	}
	ps, src, _ = resolveBrowserPaths(f, "serveur", nil, false)
	if src != "hubs.serveur.browser_paths" || browserPathJSON(ps) != "[]" {
		t.Fatalf("serveur : %s %s", src, browserPathJSON(ps))
	}
	ps, src, _ = resolveBrowserPaths(f, "autre", nil, false)
	if src != "defaults.browser_paths" || ps[0].OS != "windows" {
		t.Fatalf("autre : %s %+v", src, ps)
	}
}

// Sans browser_paths ni serveur web (browser_server off) : même chemin des deux
// côtés pour chaque racine connue, dédoublonnée et triée ; sous WSL, wslpath -w
// et os windows.
func TestBrowserPathsDefault(t *testing.T) {
	roots := []string{"/share/MFS/ROOT/", "/home/u/PROJETS", "/share/MFS/ROOT", "relatif", ""}
	off := parseBackendsYAML(t, "defaults:\n  browser_server: { listen: off }\n")
	ps, src, warns := resolveBrowserPaths(off, "pc", roots, false)
	if !strings.Contains(src, "même chemin") || len(warns) != 0 {
		t.Fatalf("source %q, %v", src, warns)
	}
	if got := browserPathJSON(ps); got != `[{"container":"/home/u/PROJETS","host":"/home/u/PROJETS","os":"linux"},{"container":"/share/MFS/ROOT","host":"/share/MFS/ROOT","os":"linux"}]` {
		t.Fatalf("défaut : %s", got)
	}

	old := wslWinPath
	t.Cleanup(func() { wslWinPath = old })
	wslWinPath = func(p string) (string, error) {
		if p == "/home/u/PROJETS" {
			return "", fmt.Errorf("hors WSL")
		}
		return `\\wsl.localhost\Ubuntu` + strings.ReplaceAll(p, "/", `\`), nil
	}
	ps, src, _ = resolveBrowserPaths(off, "pc", roots, true)
	// même longueur : ordre alphabétique (/home avant /share)
	if !strings.Contains(src, "WSL") || ps[1].Host != `\\wsl.localhost\Ubuntu\share\MFS\ROOT` || ps[1].OS != "windows" ||
		ps[0].Host != "/home/u/PROJETS" || ps[0].OS != "linux" { // wslpath en échec : identité
		t.Fatalf("WSL : %s %+v", src, ps)
	}
}

func TestBrowserPathsNeverNull(t *testing.T) {
	if browserPathJSON(nil) != "[]" {
		t.Fatal("tableau vide attendu, pas null")
	}
}

// Projets sur l'hôte : la variable est posée dans l'environnement global du
// tmux du hub (serveur isolé), donc héritée par toute fenêtre créée ensuite.
func TestExportBrowserPathsHub(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CL_INNER_SOCKET", fmt.Sprintf("cl-test-bp-%d", os.Getpid()))
	t.Setenv("TMUX", "")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	t.Cleanup(func() { _ = dtmux("kill-server").Run() })
	if err := os.MkdirAll(ng2ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backendsPath(), []byte("defaults:\n  browser_paths: [{ container: /share/MFS/ROOT, host: '\\\\nas\\MFS\\ROOT' }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := dtmux("-f", "/dev/null", "new-session", "-d", "-s", "hub", "sleep 600").CombinedOutput(); err != nil {
		t.Skipf("tmux inutilisable ici : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	exportBrowserPathsHub()
	out, err := dtmux("new-window", "-P", "-F", "#{pane_id}", "-t", "hub:", "sh -c 'printf %s \"$CL_BROWSER_PATH_MAP\" > "+cfg+"/seen; sleep 5'").Output()
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	var seen []byte
	for i := 0; i < 50 && len(seen) == 0; i++ {
		seen, _ = os.ReadFile(cfg + "/seen")
		if len(seen) == 0 {
			exec.Command("sleep", "0.1").Run()
		}
	}
	var back []BrowserPath
	if err := json.Unmarshal(seen, &back); err != nil || len(back) != 1 || back[0].Host != `\\nas\MFS\ROOT` {
		t.Fatalf("vu dans le pane : %q", seen)
	}
}

// Projets en conteneur : la variable est dans la session tmux du conteneur dès
// sa création (premier pane : claude), et une config modifiée est reprise au
// rattachement par les panes créés ensuite (teammates), sans recréation.
// Faux podman : « exec … NOM cmd » exécuté en local (tmux isolé).
func TestCtrSessionBrowserPaths(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	bin, cfg := t.TempDir(), t.TempDir()
	fake := `#!/bin/bash
shift
while [ $# -gt 0 ]; do case $1 in -i|-t) shift;; -e|-u) shift 2;; *) shift; break;; esac; done
args=(); for a in "$@"; do [ "$a" = /etc/cl-tmux.conf ] && a=/dev/null; args+=("$a"); done
exec "${args[@]}"
`
	if err := os.WriteFile(bin+"/podman", []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	if d, err := os.MkdirTemp("", "cltmux"); err == nil {
		t.Setenv("TMUX_TMPDIR", d)
		t.Cleanup(func() { _ = os.RemoveAll(d) })
	}
	e := Engine{b: BackendConf{Name: "podman", Type: "podman"}}
	t.Cleanup(func() { _ = e.exec("x", false, nil, nil, tmuxIn("kill-server")...) })
	_ = os.MkdirAll(ng2ConfigDir(), 0o755)
	setMap := func(host string) {
		y := "defaults:\n  browser_paths: [{ container: /share/MFS/ROOT, host: " + host + " }]\n"
		if err := os.WriteFile(backendsPath(), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seen := func(cmdTarget []string) string {
		f := cfg + "/seen"
		_ = os.Remove(f)
		if err := e.exec("x", false, nil, nil, tmuxIn(append(cmdTarget, `printf %s "$CL_BROWSER_PATH_MAP" > `+f+`; sleep 5`)...)...); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			if b, _ := os.ReadFile(f); len(b) > 0 {
				return string(b)
			}
			exec.Command("sleep", "0.1").Run()
		}
		return ""
	}

	setMap("/mnt/a")
	if err := e.newCtrSession("x", cfg, 80, 24); err != nil {
		t.Skipf("tmux inutilisable ici : %v", err)
	}
	first := seen([]string{"respawn-pane", "-k", "-t", "=" + ctrSession + ":"})
	if !strings.Contains(first, `"host":"/mnt/a"`) {
		t.Fatalf("premier pane : %q", first)
	}
	setMap("/mnt/b") // config modifiée, conteneur intact
	e.setCtrBrowserPaths("x")
	mate := seen([]string{"split-window", "-t", "=" + ctrSession + ":"})
	if !strings.Contains(mate, `"host":"/mnt/b"`) {
		t.Fatalf("teammate après rattachement : %q", mate)
	}
}
