package platform

import (
	"os"
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

// LeaderState : ce qu'on peut dire du leader (pid, pidSpace).
type LeaderState int

const (
	LeaderUnknown LeaderState = iota // espace étranger introuvable ici : on ne conclut rien
	LeaderAlive
	LeaderDead
)
