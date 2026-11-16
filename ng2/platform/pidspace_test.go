package platform

import (
	"os"
	"strings"
	"testing"
)

// #169 (LOT-3.2) — nom du launcher (CL_HUB_NAME), forme valide, espaces de PID. Contrat : contracts/launcher-env.md.

func TestValidHubNameForm(t *testing.T) {
	for _, ok := range []string{"a", "A1", "cluster-qualif", "hub.prod_1", "0abc", strings.Repeat("a", 63)} {
		if !ValidHubName(ok) {
			t.Errorf("%q refusé, attendu valide", ok)
		}
	}
	for _, bad := range []string{"", "-a", ".a", "_a", "a b", "a/b", "a:b", "é", strings.Repeat("a", 64), "a\n"} {
		if ValidHubName(bad) {
			t.Errorf("%q accepté, attendu invalide", bad)
		}
	}
}

func TestHubNameErrorOnlyWhenSetAndInvalid(t *testing.T) {
	t.Setenv(HubNameVar, "")
	if err := HubNameError(); err != nil {
		t.Errorf("variable absente : %v", err)
	}
	t.Setenv(HubNameVar, "cluster-qualif")
	if err := HubNameError(); err != nil {
		t.Errorf("nom valide : %v", err)
	}
	t.Setenv(HubNameVar, "-mauvais nom")
	err := HubNameError()
	if err == nil || !strings.Contains(err.Error(), HubNameVar) || !strings.Contains(err.Error(), "-mauvais nom") {
		t.Errorf("nom invalide : %v, attendu une erreur citant la variable et la valeur", err)
	}
}

func TestHostNameFollowsCLHubNameRealHostNameDoesNot(t *testing.T) {
	real, _ := os.Hostname()
	t.Setenv(HubNameVar, "")
	if HostName() != real || RealHostName() != real {
		t.Errorf("sans CL_HUB_NAME : %q / %q, attendu %q (v6.2.0)", HostName(), RealHostName(), real)
	}
	t.Setenv(HubNameVar, "pod-stable")
	if HostName() != "pod-stable" {
		t.Errorf("HostName = %q, attendu pod-stable", HostName())
	}
	if RealHostName() != real {
		t.Errorf("RealHostName = %q, attendu le nom réel %q (comparé aux espaces de PID de Claude Code)", RealHostName(), real)
	}
	t.Setenv(HubNameVar, "nom invalide")
	if HostName() != real { // invalide : repli (l'erreur est signalée au démarrage par HubNameError)
		t.Errorf("nom invalide : HostName = %q, attendu le repli %q", HostName(), real)
	}
}
