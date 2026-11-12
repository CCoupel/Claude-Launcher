package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// faux docker : un fichier par conteneur (« status|projet|hostname »), journal
// des commandes ; inspect, rename et rm suffisent à ensure avant la création.
const fakeNamingDocker = `#!/bin/sh
d=$FAKE_NAMING; echo "$*" >> $d/log
case $1 in
inspect) f=$d/$4; [ -f $f ] || exit 1; IFS='|' read st pr ho < $f; echo "$st|img|$FAKE_LOCAL|$pr|$ho" ;;
rename) mv $d/$2 $d/$3 ;;
rm) rm -f $d/$4 ;;
*) exit 1 ;;
esac
`

func withNamingDocker(t *testing.T) (dir string, put func(name, status, proj, host string)) {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeNamingDocker), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_NAMING", dir)
	t.Setenv("FAKE_LOCAL", EnvSpec{Image: "img"}.localKey())
	return dir, func(name, status, proj, host string) {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(status+"|"+proj+"|"+host+"\n"), 0o644)
	}
}

// ancien nom en marche : renommé, conservé (sa session continue)
func TestEnsureRenamesRunningLegacy(t *testing.T) {
	withHome(t)
	dir, put := withNamingDocker(t)
	proj := "/p/PRO/Demo"
	put(legacyCtrName(proj), "running", proj, legacyCtrName(proj))
	var log bytes.Buffer
	n, err := Engine{BackendConf{Name: "docker", Type: "docker"}}.Ensure(proj, EnvSpec{Image: "img"}, &log)
	if err != nil || n != "pro-demo" || !strings.Contains(log.String(), "renommé pro-demo") {
		t.Fatalf("ensure : %q %v %s", n, err, log.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "pro-demo")); err != nil {
		t.Error("conteneur non renommé")
	}
}

// arrêté avec l'ancien hostname : recréé (rm puis création)
func TestEnsureRecreatesStaleHost(t *testing.T) {
	withHome(t)
	dir, put := withNamingDocker(t)
	proj := "/p/PRO/Demo"
	put("pro-demo", "exited", proj, legacyCtrName(proj))
	var log bytes.Buffer
	_, _ = Engine{BackendConf{Name: "docker", Type: "docker"}}.Ensure(proj, EnvSpec{Image: "img"}, &log)
	b, _ := os.ReadFile(filepath.Join(dir, "log"))
	if !strings.Contains(log.String(), "hostname "+legacyCtrName(proj)+" → demo") || !strings.Contains(string(b), "rm -f -v pro-demo") {
		t.Errorf("hostname périmé non corrigé :\n%s\n%s", log.String(), b)
	}
}

// même catégorie et même nom sous une autre racine : refusé
func TestEnsureNameCollision(t *testing.T) {
	withHome(t)
	_, put := withNamingDocker(t)
	put("pro-demo", "running", "/autre/PRO/Demo", "demo")
	_, err := Engine{BackendConf{Name: "docker", Type: "docker"}}.Ensure("/p/PRO/Demo", EnvSpec{Image: "img"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "/autre/PRO/Demo") {
		t.Errorf("collision non détectée : %v", err)
	}
}
