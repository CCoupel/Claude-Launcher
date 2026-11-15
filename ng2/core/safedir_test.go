package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// GATE LOT-2 (#158) : « dubious ownership » (projets sur un partage appartenant à un autre
// compte, typiquement Windows → WSL). Les appels git et gh DU LAUNCHER autorisent le seul
// dossier du projet via GIT_CONFIG_COUNT/KEY/VALUE, sans toucher à la config de l'utilisateur.

// dubiousGit : faux git qui refuse le dépôt (« dubious ownership ») sauf si safe.directory
// est fourni par l'environnement de l'appel pour ce dossier précis.
func dubiousGit(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux git en shell : hors Windows")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
ok=0
n=${GIT_CONFIG_COUNT:-0}
i=0
while [ "$i" -lt "$n" ]; do
  eval k=\$GIT_CONFIG_KEY_$i; eval v=\$GIT_CONFIG_VALUE_$i
  [ "$k" = "safe.directory" ] && [ "$v" = "*" ] && { echo "interdit : safe.directory=*" >&2; exit 99; }
  [ "$k" = "safe.directory" ] && [ "$v" = '` + dir + `' ] && ok=1
  i=$((i+1))
done
if [ "$ok" = 0 ]; then
  echo "fatal: detected dubious ownership in repository at '` + dir + `'" >&2
  exit 128
fi
exec '` + real + `' "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// jamais d'écriture dans la config git de l'utilisateur
func isolateGitGlobal(t *testing.T) (global string) {
	t.Helper()
	home := t.TempDir()
	global = filepath.Join(home, "gitconfig-global")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return global
}

func TestGitReadsAuthorizeOnlyTheProjectDir(t *testing.T) {
	dir := fakeGitRepo(t)
	global := isolateGitGlobal(t)
	dubiousGit(t, dir)
	g := ReadGit(dir)
	if !g.IsRepo || g.Branch != "main" {
		t.Fatalf("ReadGit refusé malgré safe.directory par appel : %+v", g)
	}
	if d := ReadDetail(dir); d.LastMsg != "init" {
		t.Errorf("ReadDetail refusé : %+v", d)
	}
	if _, err := os.Stat(global); err == nil {
		t.Error("la config git globale a été écrite")
	}
}

func TestWithSafeDirectoryKeepsExistingEntries(t *testing.T) {
	env := []string{"A=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=false"}
	out := WithSafeDirectory(env, "/p/alpha")
	m := map[string]string{}
	for _, kv := range out {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	if m["GIT_CONFIG_KEY_0"] != "core.quotepath" || m["GIT_CONFIG_VALUE_0"] != "false" || m["A"] != "1" {
		t.Errorf("entrées existantes écrasées : %v", out)
	}
	found := false
	for i := 1; i < 5; i++ {
		if m["GIT_CONFIG_KEY_"+string(rune('0'+i))] == "safe.directory" && m["GIT_CONFIG_VALUE_"+string(rune('0'+i))] == "/p/alpha" {
			found = true
		}
	}
	if !found || m["GIT_CONFIG_COUNT"] == "1" {
		t.Errorf("safe.directory du projet absent ou compteur inchangé : %v", out)
	}
	for k, v := range m {
		if strings.HasPrefix(k, "GIT_CONFIG_VALUE_") && v == "*" {
			t.Errorf("safe.directory=* interdit : %s", k)
		}
	}
}

// Formes de chemin que git voit : barres obliques ; partage réseau Windows aussi sous la forme
// %(prefix)/… que git recommande pour les chemins UNC.
func TestSafeDirValuesForms(t *testing.T) {
	has := func(vs []string, w string) bool {
		for _, v := range vs {
			if v == w {
				return true
			}
		}
		return false
	}
	if vs := safeDirValues(`C:\proj\alpha`, true); !has(vs, "C:/proj/alpha") {
		t.Errorf("lettre de lecteur : %v", vs)
	}
	vs := safeDirValues(`\\wsl.localhost\Ubuntu\home\u\alpha`, true)
	if !has(vs, "//wsl.localhost/Ubuntu/home/u/alpha") || !has(vs, "%(prefix)///wsl.localhost/Ubuntu/home/u/alpha") {
		t.Errorf("chemin UNC : %v", vs)
	}
	for _, v := range vs {
		if v == "*" {
			t.Error("safe.directory=* interdit")
		}
	}
}

func TestGHOwnershipRedWithHelpCommand(t *testing.T) {
	dir := t.TempDir()
	fakeGH(t, `echo "fatal: detected dubious ownership in repository at '`+dir+`'" >&2; exit 128`)
	_, err := ReadMilestones(dir, "")
	if err == nil {
		t.Fatal("erreur attendue")
	}
	kind, benign := GHKind(err)
	if kind != GHOwnership || benign {
		t.Errorf("GHKind = %q, %v ; attendu %q rouge (pas un cas légitime)", kind, benign, GHOwnership)
	}
	if !strings.Contains(err.Error(), "git config --global --add safe.directory '"+filepath.ToSlash(dir)+"'") {
		t.Errorf("message = %q, attendu la commande d'aide avec le chemin du projet", err)
	}
}

// gh reçoit le safe.directory du projet dans son environnement (il appelle git pour trouver le remote).
func TestGHGetsSafeDirectoryEnv(t *testing.T) {
	dir := t.TempDir()
	fakeGH(t, `n=${GIT_CONFIG_COUNT:-0}; i=0; ok=0
while [ "$i" -lt "$n" ]; do eval k=\$GIT_CONFIG_KEY_$i; eval v=\$GIT_CONFIG_VALUE_$i
  [ "$k" = "safe.directory" ] && [ "$v" = '`+filepath.ToSlash(dir)+`' ] && ok=1; i=$((i+1)); done
if [ "$ok" = 1 ]; then echo '[]'; else echo "fatal: detected dubious ownership in repository at '`+dir+`'" >&2; exit 128; fi`)
	if _, err := ReadMilestones(dir, ""); err != nil {
		t.Fatalf("lecture refusée malgré safe.directory par appel : %v", err)
	}
}
