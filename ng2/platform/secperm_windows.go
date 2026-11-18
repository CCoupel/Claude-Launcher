//go:build windows

package platform

import (
	"os"
	"os/exec"
)

// RestrictToUser : ACL réduite à l'utilisateur courant (héritage coupé), pour le coffre des secrets (#194).
func RestrictToUser(path string, dir bool) error {
	user := os.Getenv("USERNAME")
	if d := os.Getenv("USERDOMAIN"); d != "" && user != "" {
		user = d + `\` + user
	}
	grant := user + ":F"
	if dir {
		grant = user + ":(OI)(CI)F"
	}
	return NoWindow(exec.Command("icacls", path, "/inheritance:r", "/grant:r", grant)).Run()
}
