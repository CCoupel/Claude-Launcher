//go:build linux

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

// auxRoles : sous-commandes du launcher qui tournent en processus auxiliaires d'un serveur tmux.
var auxRoles = map[string]bool{"mirror": true, "layout-watch": true}

// FindStaleProcs : processus mirror / layout-watch du MÊME serveur tmux (CL_INNER_SOCKET de leur environnement égal à
// innerSocket) dont l'exécutable n'est pas curExe (binaire remplacé ou supprimé compris). Jamais un autre serveur.
func FindStaleProcs(root, curExe, innerSocket string) []StaleProc {
	cur, err := os.Stat(curExe)
	if err != nil {
		return nil
	}
	ents, _ := os.ReadDir(root)
	out := []StaleProc{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		role, ok := auxRole(dir, curExe)
		if !ok || procEnv(dir, "CL_INNER_SOCKET", "claude-ng2") != innerSocket {
			continue
		}
		if pe, err := os.Stat(filepath.Join(dir, "exe")); err == nil && os.SameFile(pe, cur) {
			continue // déjà le binaire courant
		}
		out = append(out, StaleProc{Role: role, PID: pid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// StaleProcs : FindStaleProcs pour ce launcher.
func StaleProcs() []StaleProc {
	exe, err := launcherExe()
	if err != nil {
		return nil
	}
	return FindStaleProcs(procRoot, exe, InnerSocket())
}

// ProcArgs : arguments (argv) d'un processus, lus de cmdline.
func ProcArgs(pid int) []string {
	b, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil
	}
	return splitNul(b)
}

func splitNul(b []byte) []string {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil
	}
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		out = append(out, string(p))
	}
	return out
}

// auxRole : rôle auxiliaire d'un processus (argv[1] = mirror / layout-watch, argv[0] se terminant comme le binaire).
func auxRole(dir, curExe string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return "", false
	}
	a := splitNul(b)
	if len(a) < 2 || !auxRoles[a[1]] {
		return "", false
	}
	if filepath.Base(a[0]) != filepath.Base(curExe) {
		return "", false
	}
	return a[1], true
}

// procEnv : variable d'environnement d'un processus (def si absente).
func procEnv(dir, key, def string) string {
	b, err := os.ReadFile(filepath.Join(dir, "environ"))
	if err != nil {
		return "\x00illisible"
	}
	pre := []byte(key + "=")
	for _, kv := range bytes.Split(b, []byte{0}) {
		if bytes.HasPrefix(kv, pre) {
			return string(kv[len(pre):])
		}
	}
	return def
}

// StopProcs : SIGTERM, puis SIGKILL des survivants après grace ; rend la main quand tous sont partis (ou, au pire,
// 2 s après le SIGKILL).
func StopProcs(procs []StaleProc, grace time.Duration) {
	TermProcs(procs)
	if !waitGone(procs, grace) {
		KillSurvivors(procs)
	}
}

// TermProcs : SIGTERM à chacun.
func TermProcs(procs []StaleProc) {
	for _, p := range procs {
		signalPid(p.PID, false)
	}
}

// KillSurvivors : SIGKILL de ceux qui vivent encore, puis attente de leur départ.
func KillSurvivors(procs []StaleProc) {
	for _, p := range procs {
		if pidAlive(p.PID) {
			signalPid(p.PID, true)
		}
	}
	waitGone(procs, 2*time.Second)
}

func waitGone(procs []StaleProc, d time.Duration) bool {
	for end := time.Now().Add(d); ; time.Sleep(20 * time.Millisecond) {
		alive := false
		for _, p := range procs {
			if pidAlive(p.PID) {
				alive = true
			}
		}
		if !alive {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

func signalPid(pid int, kill bool) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if kill {
		_ = p.Kill()
		return
	}
	_ = p.Signal(syscall.SIGTERM)
}

// pidAlive : vivant (un zombie, en attente d'être récolté, est compté parti).
func pidAlive(pid int) bool {
	if b, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat")); err == nil {
		if i := bytes.LastIndexByte(b, ')'); i >= 0 && i+2 < len(b) && b[i+2] == 'Z' {
			return false
		}
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
