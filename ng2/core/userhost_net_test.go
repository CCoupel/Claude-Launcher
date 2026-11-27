//go:build net

package core

import (
	"context"
	"net"
	"testing"
	"time"
)

// realResolver : le résolveur de production (TestMain le remplace par un résolveur factice pour tous les autres tests).
func realResolver(host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// LOT-3.1 cycle 2 (#257) — vrai DNS (étiquette « net », non bloquant en CI : `go test -tags net ./core -run Net258`).
// Les noms publics connus doivent être acceptés pour un utilisateur ; un nom qui résout vers du privé reste refusé.
func TestNet258VraiDNS(t *testing.T) {
	defer SetUserHostResolverForTest(realResolver)()
	for _, h := range []string{"yahoo.fr", "yahoo.com", "github.com", "example.com", "google.com", "cloudflare.com"} {
		if err := CheckUserHost("tcp://" + h + ":2376"); err != nil {
			t.Errorf("%s : %v (adresse publique réelle refusée avec le vrai DNS)", h, err)
		}
		if err := CheckUserServer("https://" + h + ":6443"); err != nil {
			t.Errorf("serveur %s : %v", h, err)
		}
	}
	for _, h := range []string{"localhost", "nip.io", "127.0.0.1.nip.io", "10.0.0.1.nip.io"} {
		if h == "nip.io" {
			continue
		}
		if err := CheckUserHost("tcp://" + h + ":2376"); err == nil {
			t.Errorf("%s accepté (résout vers du privé/loopback)", h)
		}
	}
}
