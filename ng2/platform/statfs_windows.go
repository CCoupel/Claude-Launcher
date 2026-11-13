package platform

import "golang.org/x/sys/windows"

// OnNetworkFS : chemin UNC (\\wsl.localhost\…, \\nas\…) ou lecteur réseau :
// scruté au lieu d'être surveillé (cf. netpath.go).
func OnNetworkFS(path string) bool {
	return winNetworkPath(path, getDriveType)
}

func getDriveType(root string) uint32 {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return DriveUnknown
	}
	return windows.GetDriveType(p)
}
