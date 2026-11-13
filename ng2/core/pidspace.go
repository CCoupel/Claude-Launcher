package core

import (
	"strings"
	"sync"
	"time"

	"claude-launcher-ng2/platform"
)

func leaderAliveIn(pid int, space string) platform.LeaderState {
	return platform.LeaderAliveInHosts(pid, space, LocalCtrHosts)
}

var localHosts struct {
	sync.Mutex
	at  time.Time
	set map[string]bool
}

// LocalCtrHosts : hostnames (noms de projet) des conteneurs NG2 des moteurs
// locaux, arrêtés compris ; relu au plus toutes les 30 s.
var LocalCtrHosts = func() map[string]bool {
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
