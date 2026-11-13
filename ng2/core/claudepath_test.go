package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudePathFor(t *testing.T) {
	m, _ := NewPathMapper([]PathMap{{Local: "/mnt/mfs", Remote: "/share/MFS"}})
	if p := claudePathFor(m, "/mnt/mfs/ROOT/p", false); p != "/mnt/mfs/ROOT/p" {
		t.Errorf("hôte : chemin local attendu : %s", p)
	}
	if p := claudePathFor(m, "/mnt/mfs/ROOT/p", true); p != "/share/MFS/ROOT/p" {
		t.Errorf("conteneur : chemin des backends attendu : %s", p)
	}
	var none PathMapper
	if p := claudePathFor(none, "/x/p", true); p != "/x/p" {
		t.Errorf("sans correspondance : %s", p)
	}
}

// Hub Linux à correspondance déclarée (partage monté sous /mnt/mfs ici, sous
// /share/MFS dans les conteneurs) : un projet de l'hôte garde son chemin local
// pour la reprise, le marqueur et la team ; un projet en conteneur, celui des
// backends.
func TestClaudePathHostVsContainer(t *testing.T) {
	unixOnly(t) // chemins locaux de forme Linux
	h := withHome(t)
	local := filepath.Join(h, "mnt-mfs")
	_ = os.MkdirAll(filepath.Join(h, ".config", "claude-launcher-ng2"), 0o755)
	y := "backends:\n  dock: { type: docker, host: tcp://srv:2375, shared: [/share/MFS] }\n" +
		"hubs:\n  " + hubName() + ":\n    paths:\n      - { local: '" + local + "', remote: /share/MFS }\n"
	_ = os.WriteFile(BackendsPath(), []byte(y), 0o644)
	cd := filepath.Join(local, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", cd)

	hostP := filepath.Join(local, "ROOT", "CAT", "hote")
	ctrP := filepath.Join(local, "ROOT", "CAT", "ctr")
	for _, p := range []string{hostP, ctrP} {
		_ = os.MkdirAll(filepath.Join(p, ".claude-launcher"), 0o755)
		_ = os.WriteFile(EnvPath(p), []byte("image: debian:12\n"), 0o644)
	}
	_ = WriteAssignment(hostP, "host")
	_ = WriteAssignment(ctrP, "dock")

	if ClaudePath(hostP) != hostP || ClaudePath(ctrP) != "/share/MFS/ROOT/CAT/ctr" {
		t.Fatalf("ClaudePath : %s / %s", ClaudePath(hostP), ClaudePath(ctrP))
	}
	// marqueur : local et nom local pour l'hôte ; chemins des backends pour le conteneur
	if s := CleanExitShell(hostP); !strings.Contains(s, cd+"/claude-launcher-ng2/clean-exit/"+ProjectDirName(hostP)) {
		t.Errorf("marqueur hôte : %s", s)
	}
	if s := CleanExitShell(ctrP); !strings.Contains(s, "'/share/MFS/.claude/claude-launcher-ng2/clean-exit/"+ProjectDirName("/share/MFS/ROOT/CAT/ctr")+"'") {
		t.Errorf("marqueur conteneur : %s", s)
	}
	if CleanExitMarker(ctrP) != CleanExitPath(ProjectDirName("/share/MFS/ROOT/CAT/ctr")) || CleanExitMarker(hostP) != CleanExitPath(ProjectDirName(hostP)) {
		t.Error("CleanExitMarker : même nom que CleanExitShell")
	}
	// reprise : conversation du projet de l'hôte rangée sous son chemin local
	d := filepath.Join(cd, "projects", ProjectDirName(hostP))
	_ = os.MkdirAll(d, 0o755)
	f := filepath.Join(d, "aaaaaaaa-1.jsonl")
	_ = os.WriteFile(f, []byte("{}\n"), 0o644)
	mt := time.Now().Add(-time.Hour)
	_ = os.Chtimes(f, mt, mt)
	if r, ok := LoadConfig().PendingSession(hostP); !ok || r.ID != "aaaaaaaa-1" {
		t.Errorf("reprise d'un projet de l'hôte : %+v %v", r, ok)
	}
}

// sans correspondance : marqueur inchangé (chemins locaux, filepath)
func TestCleanExitShellUnchanged(t *testing.T) {
	withHome(t)
	p := "/p/PRO/Demo"
	want := "_ce=$?; (( _ce < 128 || _ce == 130 )) && mkdir -p " + Shq(filepath.Dir(CleanExitPath(ProjectDirName(p)))) + " && touch " + Shq(CleanExitPath(ProjectDirName(p)))
	if s := CleanExitShell(p); s != want {
		t.Errorf("CleanExitShell : %s", s)
	}
}

// « mounts » : « /… » est un chemin Linux, contrôlé ici à son chemin local
func TestMountPaths(t *testing.T) {
	withHubPaths(t, "")
	win := wslMFS + `\ROOT\CAT\proj`
	l, r := mountPaths(win, "/share/MFS/data")
	if l != wslMFS+`\data` || r != "/share/MFS/data" {
		t.Errorf("Linux absolu : %q %q", l, r)
	}
	if l, r := mountPaths("/p/proj", "/ailleurs/x"); l != "/ailleurs/x" || r != "/ailleurs/x" {
		t.Errorf("hors correspondance : %q %q", l, r)
	}
	if l, r := mountPaths("/p/proj", "../autre"); l != filepath.Clean(filepath.Join("/p/proj", "../autre")) || r != l {
		t.Errorf("relatif : %q %q", l, r)
	}
}
