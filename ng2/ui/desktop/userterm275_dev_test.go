package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 jalon 4 (dev) : Ouvrir un projet d'utilisateur — le MÊME projTerm s'attache (tmux -C) par « docker exec » sous l'uid ; le faux docker
// relaie les « exec … tmux … » vers un VRAI tmux sur une socket privée. Fermer arrête le conteneur.

func TestOuvrirEtFermerProjetUtilisateur275(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	u := ub257Start(t)
	sock := fmt.Sprintf("cl-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	st := filepath.Join(u.dir, "state")
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2
echo "ARGV: $*" >> "` + u.dir + `/calls.log"
S="` + st + `"; touch "$S"
case "$1" in
  create)
    name=""; labels=""
    for a in "$@"; do case "$a" in --name=*) name="${a#--name=}" ;; --label=*) labels="$labels,${a#--label=}" ;; esac; done
    echo "$name|created|img|${labels#,}" >> "$S"; echo id ;;
  ps) case "$*" in *State*) cat "$S" ;; *) awk -F"|" '{print $1" "$4}' "$S" ;; esac ;;
  start) sed -i "s/^$2|created|/$2|running|/; s/^$2|exited|/$2|running|/" "$S" ;;
  stop) sed -i "s/^$2|running|/$2|exited|/" "$S" ;;
  inspect) shift; [ "$1" = "-f" ] && { n="$3"; f="$2"; } || n="$1"; grep -q "^$n|" "$S" || exit 1
    case "$f" in *Labels*) grep "^$n|" "$S" | head -1 | tr ',' '\n' | grep '^cl.user=' | cut -d= -f2 ;; *) echo true ;; esac ;;
  exec)
    case "$*" in
      *" tmux "*)
        args=""; seen=0; skip=0
        for a in "$@"; do
          if [ $seen -eq 0 ]; then [ "$a" = tmux ] && seen=1; continue; fi
          if [ $skip -eq 1 ]; then skip=0; continue; fi
          if [ $skip -eq 2 ]; then skip=0; args="$args ` + sock + `"; continue; fi
          case "$a" in -f) skip=1; continue ;; -L) skip=2 ;; esac
          args="$args $a"
        done
        eval "exec env -u TMUX tmux $args" ;;
      *tee*) cat >/dev/null ;;
      *"head -c"*) exit 1 ;;
      *) : ;;
    esac ;;
  *) : ;;
esac
`
	if err := os.WriteFile(filepath.Join(u.dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CL_CTR_SKIP_CLAUDE", "1")
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	call := func(m string, a any) map[string]any {
		t.Helper()
		_, res, e := u.call(t, "alice", m, a)
		if e != "" {
			t.Fatalf("%s : %s", m, e)
		}
		return res
	}
	if r := call("create", map[string]any{"backend": "d", "category": "cat", "name": "proj"}); r["ok"] != true {
		t.Fatalf("create : %v", r)
	}
	if r := call("open", map[string]any{"path": "d/cat/proj"}); r["tab"] != "d/cat/proj" {
		t.Fatalf("open : %v", r)
	}
	tabState := func() string {
		for _, tb := range call("init", map[string]any{})["state"].(map[string]any)["tabs"].([]any) {
			if m := tb.(map[string]any); m["path"] == "d/cat/proj" {
				return fmt.Sprint(m["state"])
			}
		}
		return ""
	}
	deadline := time.Now().Add(40 * time.Second)
	for tabState() != "ready" && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if s := tabState(); s != "ready" {
		t.Fatalf("onglet %q (ready attendu) :\n%s", s, u.calls())
	}
	call("termResize", map[string]any{"path": "d/cat/proj", "cols": 120, "rows": 40})
	if out, _ := exec.Command("tmux", "-L", sock, "list-sessions", "-F", "#{session_name}").Output(); !strings.Contains(string(out), "cl") {
		t.Errorf("session tmux « cl » absente : %q", out)
	}
	for _, c := range strings.Split(u.calls(), "\n") {
		if strings.HasPrefix(c, "ARGV: exec ") && !strings.Contains(c, "--user=2000:2000") {
			t.Errorf("exec hors uid du traitement : %s", c)
		}
	}
	if r := call("close", map[string]any{"path": "d/cat/proj"}); r["ok"] != true {
		t.Fatalf("close : %v", r)
	}
	if tabState() != "" {
		t.Error("onglet encore présent après Fermer")
	}
	if b, _ := os.ReadFile(st); !strings.Contains(string(b), "|exited|") {
		t.Errorf("conteneur non arrêté :\n%s", b)
	}
	time.Sleep(300 * time.Millisecond) // le sondage ne relance rien
	if b, _ := os.ReadFile(st); strings.Contains(string(b), "|running|") {
		t.Errorf("projet relancé après Fermer :\n%s", b)
	}
}
