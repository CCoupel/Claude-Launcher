//go:build relaunch219 && linux

package core

// #219 : état du launcher et processus anciens (contracts/desktop-dispatch.md « launcherInfo », « relaunch »).
// Tag provisoire `relaunch219` (API posée à l'écriture, dev-backend en parallèle) ; à retirer. Noms ATTENDUS :
//
//	type StaleProc struct{ Role string; PID int }                          // Role : "mirror" | "layout-watch"
//	func FindStaleProcs(procRoot, curExe, innerSocket string) []StaleProc  // procRoot = "/proc" ; même socket (CL_INNER_SOCKET de l'environnement) ET /proc/<pid>/exe != curExe (« (deleted) » compris)
//	func StopProcs(procs []StaleProc, grace time.Duration)                 // SIGTERM, puis SIGKILL des survivants après grace ; rend la main quand tous sont partis
//	type FileSig struct{ ... }                                             // inode, taille, date
//	func StatSig(path string) (FileSig, error)
//	func BinChanged(start FileSig, path string) bool                       // fichier remplacé (inode/date/taille) ou disparu
//	func InstalledVersion(exe string, timeout time.Duration) string        // dernier mot de « <exe> version » ; "" si illisible ou délai dépassé
//
// Les « binaires » sont de vrais exécutables Go compilés par le test (un script aurait /proc/<pid>/exe = l'interpréteur).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fakeSrc219 = `package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var ver = "v0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		if os.Getenv("FAKE_HANG") != "" {
			time.Sleep(time.Minute)
		}
		fmt.Println("claude-launcher-ng2", ver)
		return
	}
	if os.Getenv("FAKE_IGNORE_TERM") != "" {
		signal.Ignore(syscall.SIGTERM)
	}
	time.Sleep(time.Minute)
}
`

func buildFake219(t *testing.T, dir, name, ver string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(fakeSrc219), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "go.mod"), []byte("module fake219\n\ngo 1.20\n"), 0o644)
	out := filepath.Join(dir, name)
	c := exec.Command("go", "build", "-ldflags", "-X main.ver="+ver, "-o", out, ".")
	c.Dir = src
	c.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		t.Skipf("go build indisponible : %v %s", err, b)
	}
	return out
}

func spawn219(t *testing.T, exe, sock string, env []string, args ...string) *exec.Cmd {
	t.Helper()
	c := exec.Command(exe, args...)
	c.Env = append(append(os.Environ(), "CL_INNER_SOCKET="+sock), env...)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }() // récolte : pas de zombie
	t.Cleanup(func() { _ = c.Process.Kill(); <-done })
	return c
}

func waitExe219(t *testing.T, pid int) { // le processus a fini son exec
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe"); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d sans /proc/exe", pid)
}

func gone219(pid int) bool {
	_, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	return err != nil
}

func TestFindStaleProcsMemeServeurSeulementEtRolesExacts219(t *testing.T) {
	d := t.TempDir()
	v1 := buildFake219(t, d, "ng-v1", "v1.0.0")
	v2 := buildFake219(t, d, "ng-v2", "v2.0.0")
	a := spawn219(t, v1, "sockA219", nil, "mirror", "@1", "alpha")
	b := spawn219(t, v1, "sockA219", nil, "layout-watch", "@1", "%1", "/p")
	autreSrv := spawn219(t, v1, "sockB219", nil, "mirror", "@9", "zeta") // autre serveur tmux : jamais touché
	courant := spawn219(t, v2, "sockA219", nil, "mirror", "@2", "beta")  // déjà le binaire courant
	nav := spawn219(t, v1, "sockA219", nil, "nav", "--launch")           // autre rôle : relancé par les panes, pas ici
	for _, c := range []*exec.Cmd{a, b, autreSrv, courant, nav} {
		waitExe219(t, c.Process.Pid)
	}
	got := map[int]string{}
	for _, p := range FindStaleProcs("/proc", v2, "sockA219") {
		got[p.PID] = p.Role
	}
	if len(got) != 2 || got[a.Process.Pid] != "mirror" || got[b.Process.Pid] != "layout-watch" {
		t.Errorf("anciens = %v, attendu mirror %d et layout-watch %d seulement", got, a.Process.Pid, b.Process.Pid)
	}
	for name, c := range map[string]*exec.Cmd{"autre serveur": autreSrv, "binaire courant": courant, "nav": nav} {
		if _, bad := got[c.Process.Pid]; bad {
			t.Errorf("%s listé à tort", name)
		}
	}
}

func TestFindStaleProcsBinaireRemplaceSurPlace219(t *testing.T) {
	d := t.TempDir()
	cur := buildFake219(t, d, "claude-launcher-ng2", "v1.0.0")
	p := spawn219(t, cur, "sockR219", nil, "mirror", "@1", "alpha")
	waitExe219(t, p.Process.Pid)
	if n := len(FindStaleProcs("/proc", cur, "sockR219")); n != 0 {
		t.Fatalf("avant mise à jour : %d ancien(s)", n)
	}
	nv := buildFake219(t, t.TempDir(), "claude-launcher-ng2", "v2.0.0")
	if err := os.Rename(nv, cur); err != nil { // mise à jour : même chemin, autre fichier ; l'ancien processus tient « (deleted) »
		t.Skipf("rename entre systèmes : %v", err)
	}
	got := FindStaleProcs("/proc", cur, "sockR219")
	if len(got) != 1 || got[0].PID != p.Process.Pid {
		t.Errorf("anciens = %v, attendu le mirror %d (exe supprimé, même chemin)", got, p.Process.Pid)
	}
}

func TestStopProcsTermPuisKillApresDelai219(t *testing.T) {
	d := t.TempDir()
	v1 := buildFake219(t, d, "ng-v1", "v1.0.0")
	poli := spawn219(t, v1, "sockS219", nil, "mirror", "@1", "a")
	tetu := spawn219(t, v1, "sockS219", []string{"FAKE_IGNORE_TERM=1"}, "layout-watch", "@1", "%1", "/p")
	temoin := spawn219(t, v1, "sockT219", nil, "mirror", "@5", "z") // pas dans la liste : doit survivre
	for _, c := range []*exec.Cmd{poli, tetu, temoin} {
		waitExe219(t, c.Process.Pid)
	}
	time.Sleep(150 * time.Millisecond) // le processus têtu a installé son handler
	grace := 400 * time.Millisecond
	t0 := time.Now()
	StopProcs([]StaleProc{{"mirror", poli.Process.Pid}, {"layout-watch", tetu.Process.Pid}}, grace)
	el := time.Since(t0)
	if !gone219(poli.Process.Pid) || !gone219(tetu.Process.Pid) {
		t.Errorf("survivants : poli=%v tetu=%v", !gone219(poli.Process.Pid), !gone219(tetu.Process.Pid))
	}
	if el < grace {
		t.Errorf("rendu en %v < délai de grâce %v : le processus qui ignore SIGTERM n'a pas pu être tué après le délai", el, grace)
	}
	if el > 5*time.Second {
		t.Errorf("trop long : %v", el)
	}
	if gone219(temoin.Process.Pid) {
		t.Error("un processus hors liste a été tué")
	}
	StopProcs(nil, grace)                                       // liste vide : sans effet
	StopProcs([]StaleProc{{"mirror", poli.Process.Pid}}, grace) // déjà parti : sans erreur ni blocage
	_ = syscall.Kill(0, 0)
}

func TestBinChangedDetecteRemplacementDateEtDisparition219(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "ng")
	write := func(s string) {
		tmp := p + ".new"
		if err := os.WriteFile(tmp, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, p); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaa")
	sig, err := StatSig(p)
	if err != nil {
		t.Fatal(err)
	}
	if BinChanged(sig, p) {
		t.Error("binaire inchangé jugé changé")
	}
	write("aaaa") // même contenu, autre fichier (inode)
	if !BinChanged(sig, p) {
		t.Error("remplacement par un autre fichier non détecté")
	}
	sig, _ = StatSig(p)
	write("bbbbbbbb")
	if !BinChanged(sig, p) {
		t.Error("taille changée non détectée")
	}
	sig, _ = StatSig(p)
	mt := time.Now().Add(-time.Hour)
	_ = os.Chtimes(p, mt, mt)
	if !BinChanged(sig, p) {
		t.Error("date changée non détectée")
	}
	sig, _ = StatSig(p)
	_ = os.Remove(p)
	if !BinChanged(sig, p) {
		t.Error("binaire supprimé non détecté")
	}
	if _, err := StatSig(filepath.Join(d, "absent")); err == nil {
		t.Error("StatSig d'un fichier absent sans erreur")
	}
}

func TestInstalledVersionLitLaSortieEtBorneLeDelai219(t *testing.T) {
	d := t.TempDir()
	v2 := buildFake219(t, d, "ng-v2", "v6.6.0")
	if got := InstalledVersion(v2, 3*time.Second); got != "v6.6.0" {
		t.Errorf("version = %q, attendu v6.6.0", got)
	}
	if got := InstalledVersion(filepath.Join(d, "absent"), time.Second); got != "" {
		t.Errorf("binaire absent : %q", got)
	}
	t.Setenv("FAKE_HANG", "1")
	t0 := time.Now()
	if got := InstalledVersion(v2, 300*time.Millisecond); got != "" || time.Since(t0) > 2*time.Second {
		t.Errorf("binaire bloqué : %q après %v (vide et borné attendus)", got, time.Since(t0))
	}
	_ = strings.TrimSpace
}
