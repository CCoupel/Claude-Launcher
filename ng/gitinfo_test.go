package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dépôts factices : un « origin » nu et un clone, manipulés pour couvrir
// chaque état des indicateurs local et remote.
func TestGitIndicators(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(dir, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	origin, work := filepath.Join(root, "origin.git"), filepath.Join(root, "work")
	run(root, "init", "-q", "--bare", "-b", "main", origin)
	run(root, "clone", "-q", origin, work)
	write(work, "a")
	run(work, "add", ".")
	run(work, "commit", "-qm", "a")
	run(work, "tag", "v1.0.0")
	run(work, "push", "-q", "--tags", "-u", "origin", "main")
	run(work, "remote", "set-head", "origin", "main")

	check := func(step, wantLocal, wantRemote string) {
		t.Helper()
		g := readGit(work)
		l, _, _ := g.Local()
		r, _, _ := g.Remote()
		if l != wantLocal || r != wantRemote {
			t.Errorf("%s : local %q remote %q, attendu %q %q (%+v)", step, l, r, wantLocal, wantRemote, g)
		}
	}
	check("main propre", "●", "✓")
	write(work, "b")
	check("main modifié", "!", "✓")
	run(work, "checkout", "-qb", "feat/x")
	check("branche modifiée", "✎", "○")
	run(work, "add", ".")
	run(work, "commit", "-qm", "b")
	check("branche commitée", "◆", "○")
	run(work, "push", "-q", "-u", "origin", "feat/x")
	check("poussée", "◆", "⇡")
	write(work, "c")
	run(work, "add", ".")
	run(work, "commit", "-qm", "c")
	check("commit non poussé", "◆", "↑1")
	run(work, "push", "-q")
	if d := readDetail(work); !strings.HasPrefix(d.VerLocal, "v1.0.0-2-g") || d.VerRelease != "v1.0.0" {
		t.Errorf("versions : locale %q release %q", d.VerLocal, d.VerRelease)
	}
	// merge de la PR côté origin, puis fetch --prune
	run(work, "push", "-q", "origin", "feat/x:main")
	run(work, "push", "-q", "origin", "--delete", "feat/x")
	run(work, "fetch", "-q", "--prune")
	check("mergée", "◆", "✓")
	run(work, "checkout", "-q", "main")
	check("main en retard", "●", "↓2")
	// suivi retiré (branche poussée sans -u) : on compare quand même à origin/main
	run(work, "branch", "-q", "--unset-upstream")
	check("main en retard, sans suivi", "●", "↓2")
	run(work, "merge", "-q", "--ff-only", "origin/main")
	check("main à jour, sans suivi", "●", "✓")
	run(work, "checkout", "-qb", "feat/locale")
	check("branche jamais poussée", "◆", "○")
}
