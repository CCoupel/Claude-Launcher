package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLauncherEnv(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfg)
	proj := t.TempDir()
	_ = os.MkdirAll(filepath.Join(proj, ".claude-launcher"), 0o755)
	_ = os.WriteFile(EnvPath(proj), []byte("image: debian:12\npackages: [git, make]\npackages_by_manager: {apt: [libssl-dev]}\nsetup: [\"echo projet\"]\non_start: [\"echo start\"]\n"), 0o644)

	// sans env.yaml du launcher : rien ne change
	base, _ := LoadEnv(proj)
	full, err := LoadEnvFull(proj)
	if err != nil || full.hash() != base.hash() || !reflect.DeepEqual(full.Setup, base.Setup) {
		t.Fatalf("sans launcher : %v %+v", err, full)
	}

	write := func(s string) { _ = os.WriteFile(filepath.Join(cfg, "env.yaml"), []byte(s), 0o644) }
	write("packages: [python3, git]\npackages_by_manager: {apt: [jq], apk: [py3-pip]}\nsetup: [\"echo launcher\"]\non_start: [\"echo l-start\"]\n")
	full, err = LoadEnvFull(proj)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(full.Packages, " "); got != "python3 git make" {
		t.Errorf("paquets : %q", got)
	}
	if got := full.PackagesByManager; strings.Join(got["apt"], " ") != "jq libssl-dev" || strings.Join(got["apk"], " ") != "py3-pip" {
		t.Errorf("paquets par gestionnaire : %v", got)
	}
	if strings.Join(full.Setup, "|") != "echo launcher|echo projet" || strings.Join(full.OnStart, "|") != "echo l-start|echo start" {
		t.Errorf("étapes : %v %v", full.Setup, full.OnStart)
	}
	if full.Image != "debian:12" || full.hash() == base.hash() || full.setupHash(proj) == base.setupHash(proj) {
		t.Errorf("image / empreintes : %q %s %s", full.Image, full.hash(), base.hash())
	}
	// le projet lui-même n'est pas modifié (fusion sur une copie)
	if again, _ := LoadEnv(proj); strings.Join(again.Packages, " ") != "git make" {
		t.Errorf("projet modifié : %v", again.Packages)
	}

	// clés réservées au projet : refusées avec un message clair
	for _, k := range []string{"image: x", "local: [a]", "watch: [a]", "mounts: []", "prefer: k8s"} {
		write(k + "\n")
		if _, err := LoadEnvFull(proj); err == nil || !strings.Contains(err.Error(), "réservée") {
			t.Errorf("%s accepté : %v", k, err)
		}
	}
	write("packages: [\n")
	if _, err := LoadEnvFull(proj); err == nil {
		t.Error("yaml invalide accepté")
	}
}
