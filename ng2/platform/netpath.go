package platform

import "strings"

// ── Chemins réseau sous Windows (logique pure, testée sous Linux) ─────────
//
// \\wsl.localhost\… (9P) et les partages SMB ne remontent pas, ou mal, les
// notifications de ReadDirectoryChangesW : comme sous Linux pour 9p/FUSE/NFS,
// leurs projets sont relus périodiquement (treewatch.go, cache.go).

// Types de lecteur de GetDriveType (winbase.h).
const (
	DriveUnknown   = 0
	DriveNoRootDir = 1
	DriveRemovable = 2
	DriveFixed     = 3
	DriveRemote    = 4
	DriveCDROM     = 5
	DriveRAMDisk   = 6
)

// IsUNCPath : \\serveur\partage\… ou //serveur/partage/… (dont \\wsl.localhost\…,
// \\wsl$\…, \\?\UNC\…).
func IsUNCPath(p string) bool {
	if len(p) < 3 {
		return false
	}
	sep := func(c byte) bool { return c == '\\' || c == '/' }
	if !sep(p[0]) || !sep(p[1]) {
		return false
	}
	// \\?\C:\… (chemin long local) : pas un partage ; \\?\UNC\… : si
	if p[2] == '?' || p[2] == '.' {
		rest := p[3:]
		if len(rest) > 0 && sep(rest[0]) {
			rest = rest[1:]
		}
		return len(rest) >= 4 && strings.EqualFold(rest[:3], "UNC") && sep(rest[3])
	}
	return !sep(p[2])
}

// DriveRoot : « C:\ » pour C:\… ou C:/… ; "" si p ne commence pas par une lettre de lecteur.
func DriveRoot(p string) string {
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z') {
		return strings.ToUpper(p[:1]) + `:\`
	}
	return ""
}

// winNetworkPath : p (chemin Windows) est-il sur un partage réseau ? UNC, ou
// lecteur dont driveType (GetDriveType) dit qu'il est distant.
func winNetworkPath(p string, driveType func(root string) uint32) bool {
	if IsUNCPath(p) {
		return true
	}
	if r := DriveRoot(p); r != "" && driveType != nil {
		return driveType(r) == DriveRemote
	}
	return false
}
