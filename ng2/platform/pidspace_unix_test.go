//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProcessAliveLinux(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("le processus courant est vu mort")
	}
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Skip("true indisponible")
	}
	if ProcessAlive(c.Process.Pid) {
		t.Errorf("le processus %d terminé et réclamé est vu vivant", c.Process.Pid)
	}
}

// HostPidSpace : « <nom réel de la machine>#pid:[<inode>] » — le nom réel, jamais CL_HUB_NAME (Claude Code note
// le nom réel dans les espaces de PID d'une team).
func TestHostPidSpaceUsesRealHostName(t *testing.T) {
	t.Setenv(HubNameVar, "pod-stable")
	s := HostPidSpace()
	if s == "" {
		t.Skip("espace de PID non lisible ici")
	}
	if !strings.HasPrefix(s, RealHostName()+"#pid:") {
		t.Errorf("HostPidSpace = %q, attendu le préfixe %q", s, RealHostName()+"#pid:")
	}
	if strings.Contains(s, "pod-stable") {
		t.Errorf("CL_HUB_NAME fuit dans l'espace de PID : %q", s)
	}
}

func TestLeaderAliveInHostsLinux(t *testing.T) {
	none := func() map[string]bool { return nil }
	if got := LeaderAliveInHosts(os.Getpid(), HostPidSpace(), none); HostPidSpace() != "" && got != LeaderAlive {
		t.Errorf("leader local vivant : %v", got)
	}
	if got := LeaderAliveInHosts(0, HostPidSpace(), none); got == LeaderAlive {
		t.Errorf("pid 0 vu vivant")
	}
	// espace de PID étranger CONSTRUIT : un inode de namespace qui n'existe sur aucune machine (les vrais commencent
	// à 4026531836 : celui-là est le namespace d'init de nombreux hôtes, runners CI compris — le choisir ferait trouver
	// notre propre pid), sur une machine inconnue et sans conteneur local de ce nom : on ne conclut rien.
	if got := LeaderAliveInHosts(os.Getpid(), "machine-inconnue-tw#pid:[7]", none); got != LeaderUnknown {
		t.Errorf("espace de PID étranger : %v, attendu LeaderUnknown (on ne conclut rien)", got)
	}
}
