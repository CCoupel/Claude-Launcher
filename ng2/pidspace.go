package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Espaces de PID. Claude Code note dans la team, à côté du PID du leader
// (leadProcess.pid), l'espace où ce PID a un sens : leadProcess.pidSpace =
// « <hostname>#pid:[<inode du namespace>] ». Un leader lancé dans un conteneur
// a le PID de son conteneur ; un leader d'avant un redémarrage de WSL a un PID
// d'un namespace disparu. Tester ces PID avec kill(2) sur l'hôte serait faux.

func HostName() string {
	h, _ := os.Hostname()
	return h
}

// hostPidSpace : espace de PID du launcher, au format de Claude Code.
func HostPidSpace() string {
	h, _ := os.Hostname()
	ns, _ := os.Readlink("/proc/self/ns/pid")
	return h + "#" + ns
}

// leaderState : ce qu'on peut dire du leader (pid, pidSpace).
type LeaderState int

const (
	LeaderUnknown LeaderState = iota // espace étranger introuvable ici : on ne conclut rien
	LeaderAlive
	LeaderDead
)

// leaderAliveInHosts : leaderAliveIn, les hostnames des conteneurs locaux étant
// fournis par l'appelant (localCtrHosts).
func LeaderAliveInHosts(pid int, space string, localCtrHosts func() map[string]bool) LeaderState {
	if pid <= 0 {
		return LeaderDead
	}
	host := HostPidSpace()
	if space == "" || space == host {
		switch err := syscall.Kill(pid, 0); {
		case err == nil || err == syscall.EPERM:
			return LeaderAlive
		default:
			return LeaderDead
		}
	}
	hName, _, _ := strings.Cut(host, "#")
	name, ns, ok := strings.Cut(space, "#")
	if !ok {
		return LeaderUnknown
	}
	if name == hName {
		return LeaderDead // même machine, namespace d'un démarrage précédent
	}
	// conteneur local : ses processus sont visibles de l'hôte ; NSpid donne leur
	// PID dans chaque namespace, le dernier étant celui du conteneur
	found := false
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		if l, err := os.Readlink(d + "/ns/pid"); err != nil || l != ns {
			continue
		}
		found = true
		b, err := os.ReadFile(d + "/status")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if f := strings.Fields(strings.TrimPrefix(line, "NSpid:")); strings.HasPrefix(line, "NSpid:") && len(f) > 0 {
				if f[len(f)-1] == strconv.Itoa(pid) {
					return LeaderAlive
				}
			}
		}
	}
	if found {
		return LeaderDead // le conteneur tourne, son leader non
	}
	if strings.HasPrefix(name, "cl-") || localCtrHosts()[name] {
		return LeaderDead // conteneur NG2 local arrêté (« cl- » : hostname d'avant v5.6)
	}
	return LeaderUnknown // autre machine (ssh, k8s…) : invérifiable d'ici
}

func leaderAliveIn(pid int, space string) LeaderState {
	return LeaderAliveInHosts(pid, space, localCtrHosts)
}

var localHosts struct {
	sync.Mutex
	at  time.Time
	set map[string]bool
}

// localCtrHosts : hostnames (noms de projet) des conteneurs NG2 des moteurs
// locaux, arrêtés compris ; relu au plus toutes les 30 s.
var localCtrHosts = func() map[string]bool {
	localHosts.Lock()
	defer localHosts.Unlock()
	if localHosts.set != nil && time.Since(localHosts.at) < 30*time.Second {
		return localHosts.set
	}
	set := map[string]bool{}
	bs, _ := LoadBackends()
	for _, b := range bs {
		if b.Type == "host" || b.remote() {
			continue
		}
		e := Engine{b}
		ids, err := e.quick("ps", "-a", "-q", "--filter", "label=cl.launcher=ng2")
		if err != nil || len(strings.Fields(string(ids))) == 0 {
			continue
		}
		out, err := e.quick(append([]string{"inspect", "-f", "{{.Config.Hostname}}"}, strings.Fields(string(ids))...)...)
		if err != nil {
			continue
		}
		for _, h := range strings.Fields(string(out)) {
			set[h] = true
		}
	}
	localHosts.at, localHosts.set = time.Now(), set
	return set
}
