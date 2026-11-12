//go:build linux || darwin

package platform

import (
	"syscall"
)

// ── Disque réseau : signature interrogée ──────────────────────────────────

// OnNetworkFS : le projet est-il sur un système 9p (ex. /mnt/c sous WSL) ?
func OnNetworkFS(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return networkFSMagic[int64(st.Type)]
}

// networkFSMagic : systèmes de fichiers où inotify est lent à poser (réseau)
// et aveugle aux écritures faites ailleurs (autre machine, conteneur distant,
// pod) : leurs projets sont scrutés au lieu d'être surveillés.
var networkFSMagic = map[int64]bool{
	0x01021997: true, // 9p (/mnt/c de WSL)
	0x65735546: true, // FUSE (MooseFS, sshfs, GlusterFS…)
	0x6969:     true, // NFS
	0xFF534D42: true, // CIFS
	0xFE534D42: true, // SMB2
	0x00C36400: true, // Ceph
}
