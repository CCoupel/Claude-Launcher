//go:build linux || darwin

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// HostPidSpace : espace de PID du launcher, au format de Claude Code.
func HostPidSpace() string {
	h, _ := os.Hostname()
	ns, _ := os.Readlink("/proc/self/ns/pid")
	return h + "#" + ns
}

// LeaderAliveInHosts : leaderAliveIn, les hostnames des conteneurs locaux étant
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
