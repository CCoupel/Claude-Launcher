package platform

// Windows : le launcher ne lance aucun claude sur le poste (backends locaux
// refusés) ; tout leader vit dans un conteneur distant, dans un espace de PID
// Linux invérifiable d'ici. Comportement sûr : on ne conclut à la mort d'un
// leader que sur un PID invalide, comme sous Linux ; sinon LeaderUnknown, et
// aucune team n'est supprimée sur un doute (cf. leaderVerdict).

// HostPidSpace : identité du poste au format de Claude Code, sans namespace
// (aucun leader ne peut la porter : il tourne toujours dans un conteneur).
func HostPidSpace() string { return HostName() + "#" }

// LeaderAliveInHosts : leader d'un conteneur, invérifiable depuis Windows.
func LeaderAliveInHosts(pid int, space string, localCtrHosts func() map[string]bool) LeaderState {
	return leaderVerdictRemoteOnly(pid)
}
