package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// #154 : aucune lecture git du launcher ne laisse de `.git/index.lock`, même
// interrompue au délai ; aucune lecture n'écrit dans `.git`. Dépôts temporaires
// factices uniquement, jamais le dépôt réel.

// fakeGitRepo : dépôt temporaire avec un commit, un tag et une modification.
func fakeGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	// pas de maintenance automatique (gc, `git maintenance run --auto`) : après un commit, git peut lancer en arrière-plan
	// une maintenance qui crée puis supprime .git/objects/maintenance.lock pendant les instantanés de
	// TestReadsDoNotWriteGitDir (runner CI) — un faux « fichier supprimé par une lecture »
	run("config", "gc.auto", "0")
	run("config", "gc.autoDetach", "false")
	run("config", "maintenance.auto", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "init")
	run("tag", "v1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("modifié"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeGitOnPath : script `git` placé en tête du PATH ; il consigne
// « GIT_OPTIONAL_LOCKS|arguments » dans log, puis appelle le vrai git, sauf si
// hook (code shell, reçoit "$@") décide autrement.
func fakeGitOnPath(t *testing.T, hook string) (log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux git en shell : hors Windows")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	bin := t.TempDir()
	log = filepath.Join(t.TempDir(), "calls.log")
	script := "#!/bin/sh\n" +
		"echo \"${GIT_OPTIONAL_LOCKS:-unset}|$*\" >> '" + log + "'\n" +
		hook + "\n" +
		"exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("aucun appel git consigné : %v", err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Les lectures ne lancent jamais `describe --dirty` (il rafraîchit l'index et
// prend index.lock malgré --no-optional-locks) : le suffixe se déduit du
// `status --porcelain` déjà lu.
func TestReadDetailNeverDescribeDirty(t *testing.T) {
	dir := fakeGitRepo(t)
	log := fakeGitOnPath(t, "")
	ReadGit(dir)
	d := ReadDetail(dir)
	for _, l := range readLog(t, log) {
		if strings.Contains(l, "--dirty") {
			t.Errorf("appel git interdit (#154) : %s", l)
		}
	}
	// version de travail : tag + suffixe de copie modifiée, sans --dirty
	if !strings.HasPrefix(d.VerLocal, "v1.0.0") || !strings.Contains(d.VerLocal, "dirty") {
		t.Errorf("VerLocal = %q, attendu v1.0.0 suivi du suffixe dirty (copie modifiée)", d.VerLocal)
	}
}

// Toutes les lectures posent GIT_OPTIONAL_LOCKS=0 (en plus de --no-optional-locks).
func TestGitReadsSetOptionalLocksZero(t *testing.T) {
	dir := fakeGitRepo(t)
	log := fakeGitOnPath(t, "")
	ReadGit(dir)
	ReadDetail(dir)
	calls := readLog(t, log)
	if len(calls) == 0 {
		t.Fatal("aucun appel git")
	}
	for _, l := range calls {
		if !strings.HasPrefix(l, "0|") {
			t.Errorf("GIT_OPTIONAL_LOCKS=0 absent de l'appel : %s", l)
		}
	}
}

// Lecture interrompue au délai : un `describe --dirty` qui prend le verrou puis
// pend est tué par le délai de 5 s ; aucun `index.lock` ne doit rester (soit
// l'appel n'est plus fait, soit git est arrêté par un signal d'arrêt et non
// par SIGKILL). Dure ~5 s tant que l'appel existe.
func TestInterruptedReadLeavesNoIndexLock(t *testing.T) {
	dir := fakeGitRepo(t)
	lock := filepath.Join(dir, ".git", "index.lock")
	hook := `case "$*" in *--dirty*)
  : > '` + lock + `'
  trap "rm -f '` + lock + `'; kill \$p 2>/dev/null; exit 143" TERM INT
  sleep 8 & p=$!
  wait $p
  exit 1 ;;
esac`
	fakeGitOnPath(t, hook)
	start := time.Now()
	ReadDetail(dir)
	if _, err := os.Stat(lock); err == nil {
		t.Errorf("index.lock laissé par une lecture interrompue (%v) — #154", time.Since(start).Round(time.Second))
	}
}

// Aucune écriture dans .git par les lectures : contenu de chaque fichier (hash
// SHA-256, pas la date : granularité mtime trop grossière sur MooseFS/9p) inchangé,
// aucun fichier créé ou supprimé, aucun verrou, même avec un fichier dont la date a
// changé (index à rafraîchir).
func TestReadsDoNotWriteGitDir(t *testing.T) {
	dir := fakeGitRepo(t)
	// fichier suivi, contenu identique mais date changée : un rafraîchissement de
	// l'index en réécrit les octets (a.txt, lui, est modifié : son entrée ne change pas)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "b.txt"}, {"commit", "-q", "-m", "b"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	stale := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "b.txt"), stale, stale)
	gitDir := filepath.Join(dir, ".git")
	snap := func() map[string]string {
		m := map[string]string{}
		_ = filepath.Walk(gitDir, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			sum := sha256.Sum256(b)
			m[p] = hex.EncodeToString(sum[:])
			return nil
		})
		return m
	}
	b := snap()
	idx := filepath.Join(gitDir, "index")
	if b[idx] == "" {
		t.Fatal("index absent avant les lectures")
	}
	ReadGit(dir)
	ReadDetail(dir)
	a := snap()
	for p, h := range a {
		if old, ok := b[p]; !ok {
			t.Errorf("fichier créé dans .git par une lecture : %s", p)
		} else if h != old {
			t.Errorf("fichier modifié dans .git par une lecture (hash différent) : %s", p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			t.Errorf("fichier supprimé de .git par une lecture : %s", p)
		}
	}
	if a[idx] != b[idx] {
		t.Error("index modifié par une lecture (hash SHA-256 avant/après différent)")
	}
	if _, err := os.Stat(filepath.Join(gitDir, "index.lock")); err == nil {
		t.Error("index.lock présent après les lectures")
	}
}
