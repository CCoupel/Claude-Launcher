//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RestrictToUser : ACL réduite à l'utilisateur courant (héritage coupé), pour le coffre des secrets (#194).
func RestrictToUser(path string, dir bool) error {
	user := os.Getenv("USERNAME")
	if d := os.Getenv("USERDOMAIN"); d != "" && user != "" {
		user = d + `\` + user
	}
	if os.Getenv("USERNAME") == "" {
		return errors.New("USERNAME vide : ACL du coffre impossible")
	}
	grant := user + ":F"
	if dir {
		grant = user + ":(OI)(CI)F"
	}
	if out, err := NoWindow(exec.Command("icacls", path, "/inheritance:r", "/grant:r", grant)).CombinedOutput(); err != nil {
		return fmt.Errorf("icacls : %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
