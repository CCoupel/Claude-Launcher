//go:build !windows

package desktop

import (
	"strings"
	"testing"
)

// F2 (execApply) : backend / image (env.yaml) refusés conteneur actif ou projet ouvert ailleurs ; flux d'approbation #215
// (envTrustAnswer) jamais bloqué par ce refus.
func TestExecApplyEtApprobationNonBloquees216(t *testing.T) {
	f := containerFix215(t, "")
	dir := pendingProject215(t, f, "host")
	setOwner216(t, "", "p-alpha sur srv1")
	if _, err := f.a.execApply(dir, "docker.io/library/debian:12", "srv1", false); err == nil || !strings.Contains(err.Error(), envEditMention216) {
		t.Fatalf("execApply conteneur actif : %v", err)
	}
	info := dispatchJSON(t, f.a, "envTrustInfo", map[string]any{"path": dir})
	sha, _ := info["sha"].(string)
	if v := dispatchJSON(t, f.a, "envTrustAnswer", map[string]any{"path": dir, "choice": "ignore", "sha": sha}); v["ok"] == false || v["tab"] != dir {
		t.Fatalf("approbation bloquée par le refus de l'env : %v", v)
	}
}
