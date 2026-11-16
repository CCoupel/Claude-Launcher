package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #176 (LOT-3.2) — settings.json écrit par plusieurs launchers ; règle « un seul pilote » entre deux launchers de
// noms différents. Tout est isolé : HOME factice, tmux sur un socket dédié, faux « podman » (script).

const twSettingsEnv = "TW_SETTINGS_HELPER"

// Sous-processus d'aide : SyncSettingsEnv et SyncBrowserHook en boucle sur le settings.json partagé.
func TestSettingsHelperProcess(t *testing.T) {
	who := os.Getenv(twSettingsEnv)
	if who == "" {
		t.Skip("sous-processus d'aide")
	}
	dir := os.Getenv("TW_SETTINGS_DIR")
	for i := 0; i < 40; i++ {
		vars := []envVar{{"TW_K0", who + "-" + strconv.Itoa(i)}, {"TW_K1", "v1"}, {"TW_K2", "v2"}, {"TW_K3", "v3"}, {"TW_K4", "v4"}}
		if err := SyncSettingsEnv(dir, vars); err != nil {
			t.Fatalf("SyncSettingsEnv : %v", err)
		}
		if err := SyncBrowserHook(dir); err != nil {
			t.Fatalf("SyncBrowserHook : %v", err)
		}
	}
}

// Plusieurs processus écrivent settings.json (bloc env + hook SessionStart) : à chaque lecture le fichier est un JSON
// valide qui garde la clé de l'utilisateur ; à la fin, toutes les variables et le hook sont là, sans verrou ni
// fichier temporaire orphelin.
func TestConcurrentSettingsJSONNeverCorruptNorLost(t *testing.T) {
	unixOnly(t)
	dir := filepath.Join(t.TempDir(), "claude")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus","permissions":{"allow":["Bash(ls)"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var bad atomic.Value
	stop := make(chan struct{})
	var wgW sync.WaitGroup
	wgW.Add(1)
	go func() { // observateur : jamais de fichier tronqué, jamais la clé de l'utilisateur perdue
		defer wgW.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var m map[string]json.RawMessage
			if e := json.Unmarshal(b, &m); e != nil {
				bad.Store(fmt.Sprintf("JSON invalide en cours d'écriture : %v (%.80q)", e, b))
				return
			}
			if _, ok := m["model"]; !ok {
				bad.Store("clé « model » de l'utilisateur perdue : " + string(b))
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	var wg sync.WaitGroup
	for _, who := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func(who string) {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestSettingsHelperProcess$", "-test.count=1")
			c.Env = append(os.Environ(), twSettingsEnv+"="+who, "TW_SETTINGS_DIR="+dir, "HOME="+dir, "XDG_CACHE_HOME="+dir, "CL_CONFIG_DIR="+dir, "CL_IN_CLUSTER=0")
			if b, err := c.CombinedOutput(); err != nil {
				bad.Store(string(b))
			}
		}(who)
	}
	wg.Wait()
	close(stop)
	wgW.Wait()
	if v := bad.Load(); v != nil {
		t.Fatalf("%v", v)
	}
	b, _ := os.ReadFile(path)
	var doc struct {
		Model string            `json:"model"`
		Env   map[string]string `json:"env"`
		Hooks map[string][]any  `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("JSON final invalide : %v", err)
	}
	if doc.Model != "opus" || !strings.Contains(string(b), "Bash(ls)") {
		t.Errorf("réglages de l'utilisateur perdus : %s", b)
	}
	for _, k := range []string{"TW_K0", "TW_K1", "TW_K2", "TW_K3", "TW_K4"} {
		if doc.Env[k] == "" {
			t.Errorf("variable %s perdue : %v", k, doc.Env)
		}
	}
	if n := len(doc.Hooks["SessionStart"]); n != 1 {
		t.Errorf("hook SessionStart du launcher : %d groupe(s), attendu exactement 1", n)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasSuffix(e.Name(), ".lock") {
			t.Errorf("fichier orphelin : %s", e.Name())
		}
	}
}

// ── un seul pilote ───────────────────────────────────────────────────────────

// twFakeCtr : faux « podman » — `podman exec … <conteneur> tmux -u -L cl -f … ARGS` devient un tmux local sur un
// dossier de sockets isolé ; rend l'Engine correspondant et la fonction qui lit une option de la session.
func twFakeCtr(t *testing.T) Engine {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux absent")
	}
	unixOnly(t)
	bin, sock := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
[ "$1" = "exec" ] || exit 0
shift
while [ $# -gt 0 ]; do
  case "$1" in
    -i|-t) shift ;;
    -e|-u) shift 2 ;;
    *) break ;;
  esac
done
shift  # nom du conteneur
[ "$1" = "tmux" ] || exec "$@"
shift
args=""
while [ $# -gt 0 ]; do
  case "$1" in
    -u) shift ;;
    -f) shift 2 ;;
    *) break ;;
  esac
done
export TMUX_TMPDIR="` + sock + `"
exec tmux "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_TMPDIR", sock)
	t.Setenv("TMUX", "")
	if out, err := exec.Command("tmux", "-L", ctrSocket, "new-session", "-d", "-s", CtrSession, "sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("tmux : %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", ctrSocket, "kill-server").Run() })
	return Engine{B: BackendConf{Name: "tw-podman", Type: "podman"}}
}

func twPanes(t *testing.T) int {
	out, err := exec.Command("tmux", "-L", ctrSocket, "list-panes", "-s", "-t", "="+CtrSession+":", "-F", "#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("session perdue : %v", err)
	}
	return len(strings.Fields(string(out)))
}

// « Piloté par un autre » dans les deux sens, prise de main et reprise sans perte de la session.
func TestOnePilotBothWaysAndTakeOverLosesNothing(t *testing.T) {
	withHome(t)
	t.Setenv("CL_IN_CLUSTER", "0")
	e := twFakeCtr(t)
	const name = "tw-ctr"
	_ = exec.Command("tmux", "-L", ctrSocket, "set-option", "-t", "="+CtrSession+":", "@tw_marker", "intact").Run()
	panes := twPanes(t)

	idA, idB := "", ""
	as := func(hub string) { t.Setenv("CL_HUB_NAME", hub) }
	as("hub-a")
	idA = LauncherID()
	as("hub-b")
	idB = LauncherID()
	if idA == idB || idA == "" {
		t.Fatalf("identités non distinctes : %q %q", idA, idB)
	}

	// A prend la propriété : B voit « piloté par un autre » (propriétaire vivant, ≠ B)
	as("hub-a")
	e.ClaimFree(name)
	if o := e.Owner(name); o.Id != idA || !o.Alive() {
		t.Fatalf("A propriétaire : %+v, attendu %q vivant", o, idA)
	}
	as("hub-b")
	if o := e.Owner(name); !o.Alive() || o.Id == LauncherID() || o.Id != idA {
		t.Errorf("côté B : %+v — doit voir A comme propriétaire vivant, différent de lui", o)
	}
	// B prend la main : A voit alors B
	e.TakeOver(name)
	if o := e.Owner(name); o.Id != idB || !o.Alive() {
		t.Fatalf("après prise de main de B : %+v, attendu %q", o, idB)
	}
	as("hub-a")
	if o := e.Owner(name); !o.Alive() || o.Id == LauncherID() || o.Id != idB {
		t.Errorf("côté A : %+v — doit voir B comme propriétaire vivant, différent de lui", o)
	}
	// A reprend : sens inverse
	e.TakeOver(name)
	if o := e.Owner(name); o.Id != idA {
		t.Errorf("reprise par A : %+v", o)
	}
	// sans perte : session, panes et option de la session intacts
	if got := twPanes(t); got != panes {
		t.Errorf("panes : %d, attendu %d (la prise de main ne doit rien fermer)", got, panes)
	}
	if out, _ := exec.Command("tmux", "-L", ctrSocket, "show-option", "-v", "-t", "="+CtrSession+":", "@tw_marker").Output(); strings.TrimSpace(string(out)) != "intact" {
		t.Errorf("option de session perdue : %q", out)
	}
}

// Propriétaire parti (battement trop ancien) : plus « vivant », l'autre launcher prend la propriété sans question.
func TestStaleOwnerIsNotAForeignPilot(t *testing.T) {
	withHome(t)
	t.Setenv("CL_IN_CLUSTER", "0")
	e := twFakeCtr(t)
	t.Setenv("CL_HUB_NAME", "hub-a")
	idA := LauncherID()
	old := strconv.FormatInt(time.Now().Add(-10*ownerTTL).Unix(), 10)
	for k, v := range map[string]string{"@cl_owner": idA, "@cl_owner_at": old} {
		if err := exec.Command("tmux", "-L", ctrSocket, "set-option", "-t", "="+CtrSession+":", k, v).Run(); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CL_HUB_NAME", "hub-b")
	if o := e.Owner("tw-ctr"); o.Alive() {
		t.Errorf("propriétaire parti vu vivant : %+v", o)
	}
	e.ClaimFree("tw-ctr")
	if o := e.Owner("tw-ctr"); o.Id != LauncherID() || !o.Alive() {
		t.Errorf("prise de propriété libre : %+v", o)
	}
}

const twAltEnv = "TW_ALT_HELPER"

// Sous-processus d'aide : alterne deux jeux de variables A et B sur le settings.json partagé.
func TestSettingsAltHelperProcess(t *testing.T) {
	who := os.Getenv(twAltEnv)
	if who == "" {
		t.Skip("sous-processus d'aide")
	}
	dir := os.Getenv("TW_SETTINGS_DIR")
	set := func(p string) []envVar {
		var v []envVar
		for i := 0; i < 5; i++ {
			v = append(v, envVar{fmt.Sprintf("TW_%s%d", p, i), p + "-val"})
		}
		return v
	}
	for i := 0; i < 40; i++ {
		p := "A"
		if (i+len(who))%2 == 1 {
			p = "B"
		}
		if err := SyncSettingsEnv(dir, set(p)); err != nil {
			t.Fatalf("SyncSettingsEnv : %v", err)
		}
	}
}

// Deux jeux de variables alternés par plusieurs processus : l'état final est le jeu A complet OU le jeu B complet
// (jamais un mélange), le JSON est valide, les clés racine de l'utilisateur sont intactes (sensible au verrou).
func TestConcurrentSettingsEnvAlternatingSetsEndComplete(t *testing.T) {
	unixOnly(t)
	dir := filepath.Join(t.TempDir(), "claude")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(path, []byte(`{"permissions":{"allow":["Bash(ls)"]}}`), 0o644)
	var wg sync.WaitGroup
	var fail atomic.Value
	for _, who := range []string{"a", "bb", "ccc", "dddd"} {
		wg.Add(1)
		go func(who string) {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestSettingsAltHelperProcess$", "-test.count=1")
			c.Env = append(os.Environ(), twAltEnv+"="+who, "TW_SETTINGS_DIR="+dir, "HOME="+dir, "XDG_CACHE_HOME="+dir, "CL_CONFIG_DIR="+dir, "CL_IN_CLUSTER=0")
			if b, err := c.CombinedOutput(); err != nil {
				fail.Store(string(b))
			}
		}(who)
	}
	wg.Wait()
	if v := fail.Load(); v != nil {
		t.Fatalf("%v", v)
	}
	b, _ := os.ReadFile(path)
	var doc struct {
		Env         map[string]string `json:"env"`
		Permissions json.RawMessage   `json:"permissions"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("JSON final invalide : %v", err)
	}
	if !strings.Contains(string(doc.Permissions), "Bash(ls)") {
		t.Errorf("clé racine de l'utilisateur perdue : %s", b)
	}
	var a, bb int
	for k := range doc.Env {
		switch {
		case strings.HasPrefix(k, "TW_A"):
			a++
		case strings.HasPrefix(k, "TW_B"):
			bb++
		}
	}
	if !(a == 5 && bb == 0) && !(a == 0 && bb == 5) {
		t.Errorf("état final mélangé ou incomplet : %d variables A, %d variables B (%v)", a, bb, doc.Env)
	}
}
