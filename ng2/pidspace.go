package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Espaces de PID. Claude Code note dans la team, à côté du PID du leader
// (leadProcess.pid), l'espace où ce PID a un sens : leadProcess.pidSpace =
// « <hostname>#pid:[<inode du namespace>] ». Un leader lancé dans un conteneur
// a le PID de son conteneur ; un leader d'avant un redémarrage de WSL a un PID
// d'un namespace disparu. Tester ces PID avec kill(2) sur l'hôte serait faux.

func hostName() string {
	h, _ := os.Hostname()
	return h
}

// hostPidSpace : espace de PID du launcher, au format de Claude Code.
func hostPidSpace() string {
	h, _ := os.Hostname()
	ns, _ := os.Readlink("/proc/self/ns/pid")
	return h + "#" + ns
}

// leaderState : ce qu'on peut dire du leader (pid, pidSpace).
type leaderState int

const (
	leaderUnknown leaderState = iota // espace étranger introuvable ici : on ne conclut rien
	leaderAlive
	leaderDead
)

func leaderAliveIn(pid int, space string) leaderState {
	if pid <= 0 {
		return leaderDead
	}
	host := hostPidSpace()
	if space == "" || space == host {
		switch err := syscall.Kill(pid, 0); {
		case err == nil || err == syscall.EPERM:
			return leaderAlive
		default:
			return leaderDead
		}
	}
	hName, _, _ := strings.Cut(host, "#")
	name, ns, ok := strings.Cut(space, "#")
	if !ok {
		return leaderUnknown
	}
	if name == hName {
		return leaderDead // même machine, namespace d'un démarrage précédent
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
					return leaderAlive
				}
			}
		}
	}
	if found {
		return leaderDead // le conteneur tourne, son leader non
	}
	if strings.HasPrefix(name, "cl-") {
		return leaderDead // conteneur NG2 local arrêté
	}
	return leaderUnknown // autre machine (ssh, k8s…) : invérifiable d'ici
}
