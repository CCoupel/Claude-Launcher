package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 (dev) : instance utilisateur — liste des projets (découverte + trace), création, F5, Purger, options. Faux docker à ÉTAT (fichier).

func up275Docker(t *testing.T, dir string) {
	t.Helper()
	st := filepath.Join(dir, "state")
	script := `#!/bin/sh
[ "$1" = "-H" ] && shift 2
echo "$*" >> "` + dir + `/calls.log"
S="` + st + `"
touch "$S"
case "$1" in
  create)
    name=""; labels=""; image=""
    for a in "$@"; do
      case "$a" in
        --name=*) name="${a#--name=}" ;;
        --label=*) labels="$labels,${a#--label=}" ;;
      esac
    done
    # l'image est l'argument avant « sh -c … » : les trois derniers sont sh, -c, script
    n=$#; i=0
    for a in "$@"; do i=$((i+1)); [ $i -eq $((n-3)) ] && image="$a"; done
    echo "$name|created|$image|${labels#,}" >> "$S"; echo newid ;;
  ps) case "$*" in *State*) cat "$S" ;; *) awk -F"|" '{print $1" "$4}' "$S" ;; esac ;;
  start) sed -i "s/^$2|created|/$2|running|/; s/^$2|exited|/$2|running|/" "$S" ;;
  stop) sed -i "s/^$2|running|/$2|exited|/" "$S" ;;
  inspect)
    shift; if [ "$1" = "-f" ]; then f="$2"; n="$3"; else n="$1"; fi
    grep -q "^$n|" "$S" || exit 1
    case "$f" in
      *Labels*) grep "^$n|" "$S" | head -1 | tr ',' '\n' | grep '^cl.user=' | cut -d= -f2 ;;
      *HostConfig*) echo "2000000000|4294967296|4096|268435456" ;;
      *) echo true ;;
    esac ;;
  update) : ;;
  rm) n="$2"; sed -i "/^$n|/d" "$S" ;;
  *) echo 27.0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestUserProjets275Parcours(t *testing.T) {
	u := ub257Start(t)
	up275Docker(t, u.dir)
	t.Setenv("CL_USERS_MAX_PROJECTS", "2")
	ub257DeclareDocker(t, "github-1111", "d", "tcp://93.184.216.34:2376", "")
	call := func(m string, args any) map[string]any {
		t.Helper()
		_, res, e := u.call(t, "alice", m, args)
		if e != "" {
			t.Fatalf("%s : %s", m, e)
		}
		return res
	}
	rowsOf := func() []map[string]any {
		var out []map[string]any
		init := call("init", map[string]any{})
		for _, r := range init["state"].(map[string]any)["rows"].([]any) {
			out = append(out, r.(map[string]any))
		}
		return out
	}
	find := func(kind string) map[string]any {
		for _, r := range rowsOf() {
			if r["kind"] == kind {
				return r
			}
		}
		return nil
	}
	if info := call("newInfo", map[string]any{}); info["maxProjects"] != float64(2) {
		t.Errorf("newInfo : %v", info)
	}
	// création : objet arrêté, listé par la découverte
	if r := call("create", map[string]any{"backend": "d", "category": "cat", "name": "proj"}); r["ok"] != true || r["path"] != "d/cat/proj" {
		t.Fatalf("create : %v", r)
	}
	p := find("project")
	if p == nil || p["path"] != "d/cat/proj" || p["stopped"] != true {
		t.Fatalf("projet absent ou pas arrêté : %v", p)
	}
	if r := call("create", map[string]any{"backend": "d", "category": "cat", "name": "proj"}); r["ok"] != false || r["code"] != "conflict" || r["field"] != "name" {
		t.Errorf("nom pris : %v", r)
	}
	for name, a := range map[string]map[string]any{
		"nom invalide":    {"backend": "d", "category": "cat", "name": "Pas Valide"},
		"motif réservé":   {"backend": "d", "category": "cat", "name": "x-old-123"},
		"backend inconnu": {"backend": "zz", "category": "cat", "name": "n"},
		"dépôt à cloner":  {"backend": "d", "category": "cat", "name": "n2", "repo": "https://github.com/a/b"},
		"image hostile":   {"backend": "d", "category": "cat", "name": "n3", "image": "-v /:/h"},
	} {
		if r := call("create", a); r["ok"] != false {
			t.Errorf("%s accepté : %v", name, r)
		}
	}
	// limite de projets : le 3e est refusé (2 au plus) ; code limit sans field
	if r := call("create", map[string]any{"backend": "d", "category": "cat", "name": "deux"}); r["ok"] != true {
		t.Fatalf("2e projet : %v", r)
	}
	if r := call("create", map[string]any{"backend": "d", "category": "cat", "name": "trois"}); r["ok"] != false || r["code"] != "limit" || r["field"] != nil || !strings.Contains(fmt.Sprint(r["msg"]), "(2)") {
		t.Errorf("limite : %v", r)
	}
	// F5 démarre le projet arrêté (sans session)
	if r := call("refreshProject", map[string]any{"path": "d/cat/proj"}); r["ok"] != true || r["started"] != true {
		t.Fatalf("F5 : %v", r)
	}
	for _, r := range rowsOf() {
		if r["path"] == "d/cat/proj" && r["stopped"] == true {
			t.Error("le projet reste arrêté après F5")
		}
	}
	// options : lecture, validation en direct, application à chaud
	set := call("projectSettings", map[string]any{"path": "d/cat/proj"})
	opts, _ := set["options"].(map[string]any)
	if opts["kind"] != "container" || opts["shared"] != true {
		t.Errorf("projectSettings : %v", set)
	}
	bad := call("projectApply", map[string]any{"path": "d/cat/proj", "dryRun": true, "options": map[string]any{"values": map[string]any{"cpus": "0", "gpus": "all --privileged"}}})
	if bad["ok"] != false || !strings.Contains(fmt.Sprint(bad["problems"]), "container.cpus") {
		t.Errorf("validation en direct : %v", bad)
	}
	if r := call("projectApply", map[string]any{"path": "d/cat/proj", "options": map[string]any{"values": map[string]any{"cpus": "1.5", "memory": "2g"}}}); r["ok"] != true || r["applied"] != "hot" {
		t.Errorf("application à chaud : %v", r)
	}
	// shm_size : recréation après confirmation (jeton lié aux options)
	rc := call("projectApply", map[string]any{"path": "d/cat/proj", "options": map[string]any{"values": map[string]any{"shm_size": "1g"}}})
	rec, _ := rc["recreate"].(map[string]any)
	if rc["ok"] != false || rec == nil || rec["token"] == "" {
		t.Fatalf("recréation sans jeton : %v", rc)
	}
	if r := call("projectApply", map[string]any{"path": "d/cat/proj", "confirm": rec["token"], "options": map[string]any{"values": map[string]any{"shm_size": "1g"}}}); r["ok"] != true {
		t.Errorf("recréation confirmée : %v", r)
	}
	// Purger : refusé tant que l'objet existe ; disparu → ligne « missing », puis purge de la trace seule
	_ = os.WriteFile(filepath.Join(u.dir, "state"), nil, 0o644)
	time.Sleep(10 * time.Millisecond)
	if _, res, _ := u.call(t, "alice", "refreshProject", map[string]any{"path": "d/cat/proj"}); res["ok"] != false { // force la relecture
		t.Errorf("F5 sur un projet disparu : %v", res)
	}
	var gone map[string]any
	for _, r := range rowsOf() {
		if r["path"] == "d/cat/proj" {
			gone = r
		}
	}
	if m, _ := gone["missing"].(map[string]any); m == nil || m["backend"] != "d" {
		t.Errorf("ligne « missing » attendue : %v", gone)
	}
	if r := call("projectPurge", map[string]any{"path": "d/cat/proj"}); r["ok"] != true || !strings.Contains(fmt.Sprint(r["msg"]), "restent") {
		t.Errorf("purge : %v", r)
	}
	for _, r := range rowsOf() {
		if r["path"] == "d/cat/proj" {
			t.Errorf("projet encore listé après la purge : %v", r)
		}
	}
	// isolement : bob ne voit rien d'alice
	_, b, _ := u.call(t, "bob", "init", map[string]any{})
	if strings.Contains(fmt.Sprint(b), "cat/proj") {
		t.Error("bob voit un projet d'alice")
	}
	u.scanLeaks(t)
}
