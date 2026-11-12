package platform

import "errors"

// Windows : implémentations minimales, le temps de la couche platform Windows
// (#110). Elles permettent de compiler core et platform pour Windows ; aucune
// n'est appelée par le launcher Linux.

// ErrUnsupported : fonction non disponible sur ce système.
var ErrUnsupported = errors.New("non supporté sous Windows")

// HostPidSpace : pas d'espace de PID au format de Claude Code (Linux).
func HostPidSpace() string { return HostName() + "#" }

// LeaderAliveInHosts : invérifiable d'ici.
func LeaderAliveInHosts(pid int, space string, localCtrHosts func() map[string]bool) LeaderState {
	return LeaderUnknown
}

// OnNetworkFS : pas de détection du système de fichiers.
func OnNetworkFS(path string) bool { return false }

// RawMode : pas de mode brut du terminal.
func RawMode(fd int) (func(), error) { return nil, ErrUnsupported }

// ExecBinary : pas d'execve.
func ExecBinary(argv0 string, argv []string, envv []string) error { return ErrUnsupported }
