package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// #175 : sorties réseau bloquées — message clair dans un pod, texte inchangé hors cluster.
func TestNetworkBlockedMessages(t *testing.T) {
	if !NetworkErrorText("dial tcp: lookup api.github.com on 10.0.0.10:53: no such host") || NetworkErrorText("http 404 not found") {
		t.Error("NetworkErrorText")
	}
	t.Setenv(InClusterVar, "0")
	if NetBlockedHint() != "" {
		t.Errorf("hors cluster : %q", NetBlockedHint())
	}
	t.Setenv(InClusterVar, "1")
	if h := NetBlockedHint(); !strings.Contains(h, "sorties réseau du pod") {
		t.Errorf("en cluster : %q", h)
	}
	err := ghFail(context.Background(), errors.New("exit 1"), []byte("Get https://api.github.com: dial tcp 140.82.1.1:443: i/o timeout"), []string{"api", "x"}, "/p")
	if !strings.Contains(err.Error(), "GitHub injoignable") || !strings.Contains(err.Error(), "NetworkPolicy") {
		t.Errorf("gh injoignable en cluster : %v", err)
	}
}
