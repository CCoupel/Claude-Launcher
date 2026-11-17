package platform

import (
	"fmt"
	"os"
	"regexp"
)

// Espaces de PID. Claude Code note dans la team, à côté du PID du leader
// (leadProcess.pid), l'espace où ce PID a un sens : leadProcess.pidSpace =
// « <hostname>#pid:[<inode du namespace>] ». Un leader lancé dans un conteneur
// a le PID de son conteneur ; un leader d'avant un redémarrage de WSL a un PID
// d'un namespace disparu. Tester ces PID avec kill(2) sur l'hôte serait faux.

// HubNameVar : nom du launcher, à la place du nom de la machine (#169).
const HubNameVar = "CL_HUB_NAME"

var hubNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// ValidHubName : forme admise pour CL_HUB_NAME.
func ValidHubName(s string) bool { return hubNameRe.MatchString(s) }

// HubNameError : CL_HUB_NAME posée mais de forme invalide (nil sinon) — contrôlée au démarrage.
func HubNameError() error {
	if v := os.Getenv(HubNameVar); v != "" && !ValidHubName(v) {
		return fmt.Errorf("%s=%q invalide : %s attendu (lettres, chiffres, « . », « _ » et « - », 63 caractères au plus, premier caractère alphanumérique)",
			HubNameVar, v, hubNameRe.String())
	}
	return nil
}

// RealHostName : nom réel de la machine (ou du pod). C'est lui que Claude Code note dans les espaces de PID
// d'une team : ne sert qu'à comparer avec ceux-là.
func RealHostName() string {
	h, _ := os.Hostname()
	return h
}

// HostName : nom du launcher — CL_HUB_NAME s'il est posé (et valide), sinon le nom de la machine. Clé hubs.<nom> de
// backends.yaml, identité de propriétaire des sessions, hôte admis des serveurs web ; stable quand le pod change de nom.
func HostName() string {
	if v := os.Getenv(HubNameVar); v != "" && ValidHubName(v) {
		return v
	}
	return RealHostName()
}

// LeaderState : ce qu'on peut dire du leader (pid, pidSpace).
type LeaderState int

const (
	LeaderUnknown LeaderState = iota // espace étranger introuvable ici : on ne conclut rien
	LeaderAlive
	LeaderDead
)

// leaderVerdictRemoteOnly : verdict d'une machine où aucun leader ne tourne
// localement (Windows) : PID invalide → mort, sinon invérifiable.
func leaderVerdictRemoteOnly(pid int) LeaderState {
	if pid <= 0 {
		return LeaderDead
	}
	return LeaderUnknown
}
