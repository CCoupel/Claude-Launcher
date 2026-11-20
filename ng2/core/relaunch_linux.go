//go:build linux

package core

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
)

// procRoot : racine de procfs (tests : arborescence factice).
var procRoot = "/proc"

// auxRoles : sous-commandes du launcher qui tournent en processus auxiliaires d'un serveur tmux.
var auxRoles = map[string]bool{"mirror": true, "layout-watch": true}

// StaleProcs : processus mirror / layout-watch du MÊME serveur tmux (CL_INNER_SOCKET de leur environnement égal à
// celui de ce launcher) dont l'exécutable n'est pas le binaire courant. Jamais un processus d'un autre serveur.
func StaleProcs() []StaleProc {
	exe, err := launcherExe()
	if err != nil {
		return nil
	}
	cur, err := os.Stat(exe)
	if err != nil {
		return nil
	}
	sock := InnerSocket()
	ents, _ := os.ReadDir(procRoot)
	out := []StaleProc{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		role, ok := auxRole(filepath.Join(procRoot, e.Name()))
		if !ok || procEnv(filepath.Join(procRoot, e.Name()), "CL_INNER_SOCKET", "claude-ng2") != sock {
			continue
		}
		pe, err := os.Stat(filepath.Join(procRoot, e.Name(), "exe"))
		if err == nil && os.SameFile(pe, cur) {
			continue // déjà le binaire courant
		}
		out = append(out, StaleProc{Role: role, Pid: pid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })
	return out
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
func auxRole(dir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return "", false
	}
	a := splitNul(b)
	if len(a) < 2 || !auxRoles[a[1]] {
		return "", false
	}
	exe, err := launcherExe()
	if err != nil || filepath.Base(a[0]) != filepath.Base(exe) {
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

// TerminateStale : SIGTERM puis SIGKILL après grace aux processus donnés ; rend leurs arguments d'origine (relancer
// un layout-watch avec les mêmes arguments).
func TerminateStale(procs []StaleProc, grace func()) map[int][]string {
	args := map[int][]string{}
	for _, p := range procs {
		args[p.Pid] = ProcArgs(p.Pid)
		signalPid(p.Pid, false)
	}
	grace()
	for _, p := range procs {
		if pidAlive(p.Pid) {
			signalPid(p.Pid, true)
		}
	}
	return args
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

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
