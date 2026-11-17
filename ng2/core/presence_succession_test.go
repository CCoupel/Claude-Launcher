package core

import (
	"fmt"
	"os"
	"testing"
	"time"

	"claude-launcher-ng2/platform"
)

// Pod de StatefulSet recréé (même nom de machine, même pid) : le témoin du prédécesseur est repris aussitôt ; le même
// processus, un autre processus vivant et une autre machine restent refusés ; ancien format repris.
func TestPresenceSuccessionSamePid(t *testing.T) {
	withHome(t)
	t.Setenv("CL_CACHE_DIR", t.TempDir())
	t.Setenv("CL_HUB_NAME", "hub-succession")
	host, pid := platform.RealHostName(), os.Getpid()
	now := time.Now().Format(time.RFC3339)
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(presencePath(), []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	acquire := func() error {
		rel, err := AcquirePresence()
		if err == nil {
			rel()
		}
		return err
	}
	write(fmt.Sprintf("%s %d %s tok autre-processus", host, pid, now)) // prédécesseur : même pid, autre identifiant
	if err := acquire(); err != nil {
		t.Errorf("prédécesseur de même pid non repris : %v", err)
	}
	write(fmt.Sprintf("%s %d %s tok %s", host, pid, now, processNonce)) // le même processus
	if err := acquire(); err == nil {
		t.Error("le même processus doit rester refusé")
	}
	write(fmt.Sprintf("%s %d %s tok x", host, os.Getppid(), now)) // un autre processus vivant
	if err := acquire(); err == nil {
		t.Error("un autre processus vivant doit rester refusé")
	}
	write(fmt.Sprintf("autre-machine %d %s tok x", pid, now)) // une autre machine
	if err := acquire(); err == nil {
		t.Error("une autre machine doit rester refusée")
	}
	write(fmt.Sprintf("%s %d %s tok", host, pid, now)) // ancien format (sans identifiant de processus)
	if err := acquire(); err != nil {
		t.Errorf("témoin d'ancien format de même pid non repris : %v", err)
	}
}
