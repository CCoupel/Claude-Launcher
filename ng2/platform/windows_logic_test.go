package platform

// Logique de la couche Windows (#110), testée sur tous les systèmes : les
// fichiers _windows.go n'en sont que des enveloppes.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestIsUNCPath(t *testing.T) {
	yes := []string{`\\wsl.localhost\Ubuntu\share\MFS`, `\\wsl$\Ubuntu`, `\\nas\partage\x`, `//nas/partage`, `\\?\UNC\nas\x`, `\\.\UNC\nas\x`}
	no := []string{`C:\Users`, `C:/x`, `/share/MFS`, `\\?\C:\long`, `\\.\pipe\docker_engine`, `\x`, `\\`, `\\\x`, ``, `relatif`}
	for _, p := range yes {
		if !IsUNCPath(p) {
			t.Errorf("IsUNCPath(%q) = false", p)
		}
	}
	for _, p := range no {
		if IsUNCPath(p) {
			t.Errorf("IsUNCPath(%q) = true", p)
		}
	}
}

func TestWinNetworkPath(t *testing.T) {
	drives := map[string]uint32{`C:\`: DriveFixed, `Z:\`: DriveRemote, `E:\`: DriveRemovable}
	dt := func(root string) uint32 { return drives[root] }
	cases := map[string]bool{
		`\\wsl.localhost\Ubuntu\share\MFS\ROOT`: true, // 9P : relu périodiquement
		`\\nas\projets`:                         true,
		`Z:\projets`:                            true, // lecteur réseau
		`z:/projets`:                            true,
		`C:\Users\moi\GITHUB`:                   false,
		`E:\x`:                                  false,
		`/share/MFS`:                            false,
	}
	for p, want := range cases {
		if got := winNetworkPath(p, dt); got != want {
			t.Errorf("winNetworkPath(%q) = %v", p, got)
		}
	}
	if DriveRoot(`d:\x`) != `D:\` || DriveRoot(`\\nas\x`) != "" || DriveRoot("1:") != "" {
		t.Error("DriveRoot")
	}
}

func TestLeaderVerdictRemoteOnly(t *testing.T) {
	if leaderVerdictRemoteOnly(0) != LeaderDead || leaderVerdictRemoteOnly(-3) != LeaderDead {
		t.Error("PID invalide : mort")
	}
	if leaderVerdictRemoteOnly(4242) != LeaderUnknown {
		t.Error("leader de conteneur : invérifiable")
	}
}

// remplacement d'un exe en cours d'exécution : renommé .old, nouveau posé
func TestSwapRunningExe(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "claude-launcher-ng2.exe")
	_ = os.WriteFile(exe, []byte("v1"), 0o755)
	write := func(body string) func(string) error {
		return func(dst string) error { return os.WriteFile(dst, []byte(body), 0o755) }
	}
	if err := swapRunningExe(exe, write("v2")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "v2" {
		t.Errorf("nouveau : %q", b)
	}
	if b, _ := os.ReadFile(OldExe(exe)); string(b) != "v1" {
		t.Errorf("ancien mis de côté : %q", b)
	}
	// deuxième mise à jour : le .old précédent est remplacé
	if err := swapRunningExe(exe, write("v3")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(OldExe(exe)); string(b) != "v2" {
		t.Errorf(".old : %q", b)
	}
	// échec de l'écriture : l'exe en place est restauré
	err := swapRunningExe(exe, func(dst string) error {
		_ = os.WriteFile(dst, []byte("à moitié"), 0o755)
		return errors.New("disque plein")
	})
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if b, _ := os.ReadFile(exe); string(b) != "v3" {
		t.Errorf("restauration : %q", b)
	}
	// .old impossible à supprimer (encore verrouillé) : nom unique
	_ = os.Remove(OldExe(exe))
	_ = os.MkdirAll(filepath.Join(OldExe(exe), "verrou"), 0o755)
	if err := swapRunningExe(exe, write("v4")); err != nil {
		t.Fatal(err)
	}
	olds := oldExes(exe)
	if len(olds) != 2 {
		t.Fatalf("binaires mis de côté : %q", olds)
	}
	_ = os.RemoveAll(OldExe(exe)) // le « verrou » est levé
	_ = os.WriteFile(OldExe(exe), []byte("v3"), 0o755)
	if left := removeOldExes(exe); len(left) != 0 || len(oldExes(exe)) != 0 {
		t.Errorf("nettoyage : restent %q / %q", left, oldExes(exe))
	}
	if b, _ := os.ReadFile(exe); string(b) != "v4" {
		t.Errorf("exe touché par le nettoyage : %q", b)
	}
	// premier lancement : pas d'exe en place
	fresh := filepath.Join(d, "neuf.exe")
	if err := swapRunningExe(fresh, write("v1")); err != nil || len(oldExes(fresh)) != 0 {
		t.Errorf("sans exe en place : %v %q", err, oldExes(fresh))
	}
}

func TestGlobEscapeAndRelaunch(t *testing.T) {
	d := filepath.Join(t.TempDir(), "a[1]b") // métacaractère de Glob (« * » et « ? » : interdits sous Windows)
	_ = os.MkdirAll(d, 0o755)
	exe := filepath.Join(d, "x.exe")
	_ = os.WriteFile(exe+".123.old", nil, 0o644)
	_ = os.WriteFile(filepath.Join(d, "y.exe.1.old"), nil, 0o644)
	if o := oldExes(exe); len(o) != 1 || !strings.HasSuffix(o[0], "x.exe.123.old") {
		t.Errorf("oldExes : %q", o)
	}
	name, args := relaunchArgs(`C:\bin\new.exe`, []string{`C:\bin\old.exe`, "shell", "--launch"})
	if name != `C:\bin\new.exe` || strings.Join(args, " ") != "shell --launch" {
		t.Errorf("relaunchArgs : %q %q", name, args)
	}
	if _, args := relaunchArgs("x", nil); len(args) != 0 {
		t.Error("relaunchArgs sans argv")
	}
}

// ExecBinary (Windows) attend le nouveau processus et reprend son code
func TestExitCode(t *testing.T) {
	if exitCode(nil) != 0 || exitCode(errors.New("attente impossible")) != 1 {
		t.Error("nil → 0, autre erreur → 1")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	c := exec.Command(self, "-test.run=^TestHelperExit3$")
	c.Env = append(os.Environ(), "CL_HELPER_EXIT3=1")
	if got := exitCode(c.Run()); got != 3 {
		t.Errorf("code de l'enfant : %d", got)
	}
}

// TestHelperExit3 : processus enfant de TestExitCode.
func TestHelperExit3(t *testing.T) {
	if os.Getenv("CL_HELPER_EXIT3") == "1" {
		os.Exit(3)
	}
}

func TestEnvBlockAndSize(t *testing.T) {
	b := envBlock([]string{"A=1", "TERM=xterm-256color", "BAD=\x00x", "É=é"})
	want := utf16.Encode([]rune("A=1\x00TERM=xterm-256color\x00É=é\x00\x00"))
	if string(utf16.Decode(b)) != string(utf16.Decode(want)) {
		t.Errorf("envBlock : %q", string(utf16.Decode(b)))
	}
	if e := envBlock(nil); len(e) != 2 || e[0] != 0 || e[1] != 0 {
		t.Errorf("bloc vide : %v", e)
	}
	for _, c := range [][4]int{{0, 0, 80, 24}, {120, 40, 120, 40}, {-1, 99999, 80, 32767}} {
		if x, y := termSize(c[0], c[1]); x != c[2] || y != c[3] {
			t.Errorf("termSize(%d, %d) = %d, %d", c[0], c[1], x, y)
		}
	}
}

func TestLockFile(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "l.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := LockFile(f); err != nil {
		t.Fatal(err)
	}
}
