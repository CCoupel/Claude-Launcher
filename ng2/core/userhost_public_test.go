package core

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// LOT-3.1 cycle 2 (#257, GATE refusé) — « un utilisateur qui déclare tcp://yahoo.fr:2376 reçoit toujours “adresse non autorisée” ».
// Les tests du Batch 2 ne couvraient que les formes INTERDITES et un résolveur « 1 IP ». Ici : les adresses publiques RÉELLES (v4 ET v6,
// AAAA compris) de sites connus sont acceptées ; plages limites (juste en dessous / au-dessus de chaque plage interdite) ;
// DNS en échec → refus ; résolution lente acceptée, bloquée → bornée. Résolveur factice (SetUserHostResolverForTest), aucun réseau ici ;
// le vrai DNS est vérifié par userhost_net_test.go (étiquette « net »).

func fakeDNS(m map[string][]string) func(string) ([]net.IP, error) {
	return func(h string) ([]net.IP, error) {
		ss, ok := m[strings.ToLower(strings.TrimSuffix(h, "."))]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: h, IsNotFound: true}
		}
		var out []net.IP
		for _, s := range ss {
			out = append(out, net.ParseIP(s)) // net.ParseIP rend la forme 16 octets, comme net.LookupIP
		}
		return out, nil
	}
}

// adresses publiques réelles (relevées 2026-11-27 pour les v4 ; v6 : préfixes publics connus des mêmes opérateurs)
var realDNS = map[string][]string{
	"yahoo.fr":           {"13.248.158.7", "76.223.84.192"},
	"yahoo.com":          {"98.137.11.163", "74.6.231.20", "2001:4998:24:120d::1:0", "2001:4998:44:3507::8000"},
	"github.com":         {"4.163.8.144", "140.82.112.3", "2606:50c0:8000::154"},
	"example.com":        {"104.20.23.154", "172.66.147.243", "2606:4700:10::6814:179a", "2606:4700:10::ac42:93f3"},
	"google.com":         {"142.250.74.110", "2a00:1450:4007:80d::200e"},
	"cloudflare.com":     {"104.16.132.229", "2606:4700::6810:84e5"},
	"v6only.example.org": {"2001:4860:4860::8888"},
	"v4only.example.org": {"93.184.216.34"},
}

func TestPublic258AdressesReellesAcceptees(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	defer SetUserHostResolverForTest(fakeDNS(realDNS))()
	sp, _ := UserSpace("github-5")
	for name := range realDNS {
		for _, form := range []string{"tcp://" + name + ":2376", "tcp://" + strings.ToUpper(name) + ":2376", "tcp://" + name + ".:2376"} {
			if err := CheckUserHost(form); err != nil {
				t.Errorf("CheckUserHost(%q) : %v (adresse publique réelle refusée)", form, err)
			}
			if ms := errMsgs(sp.ValidateBackends(hb(form))); len(ms) != 0 {
				t.Errorf("ValidateBackends(%q) : %v", form, ms)
			}
		}
		if err := CheckUserServer("https://" + name + ":6443"); err != nil {
			t.Errorf("CheckUserServer(%s) : %v", name, err)
		}
	}
	// noms publics de formes variées
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil })()
	for _, n := range []string{"sub.domain.co.uk", "a1.b2.example.org", "my-host.example.io", "xn--bcher-kva.example", "docker.example.dev", "nas.example.fr", "x.y.z.example.com", "9gag.com", "a.b"} {
		if err := CheckUserHost("tcp://" + n + ":2376"); err != nil {
			t.Errorf("nom public %q refusé : %v", n, err)
		}
	}
}

// Backends chargés depuis le fichier de l'utilisateur, appel docker : le même hôte public n'est PAS refusé avant l'appel.
func TestPublic258HoteReelAvantLAppel(t *testing.T) {
	s15Setup(t)
	f := s257Tools(t)
	defer SetUserHostResolverForTest(fakeDNS(realDNS))()
	for _, h := range []string{"tcp://yahoo.fr:2376", "tcp://github.com:2376", "tcp://example.com:2376", "tcp://v6only.example.org:2376"} {
		sp := s15Space(t, "github-9", string(hb(h)))
		bs, err := sp.LoadBackends()
		if err != nil || len(bs) == 0 {
			t.Errorf("%s : backend non chargé (%v)", h, err)
			continue
		}
		before := strings.Count(f.text(), "ARGV:")
		_ = Engine{bs[0]}.cmd("version").Run()
		if strings.Count(f.text(), "ARGV:") != before+1 {
			t.Errorf("%s : docker non lancé pour un hôte public", h)
		}
	}
}

// IPv6 littérales publiques (crochets) et IPv4 littérales publiques.
func TestPublic258IPLitteralesPubliques(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	for _, h := range []string{"tcp://[2606:4700:4700::1111]:2376", "tcp://[2620:fe::fe]:2376", "tcp://[2001:4860:4860::8888]:2376", "tcp://[2001:4998:44:3507::8000]:2376",
		"tcp://[2a00:1450:4007:80d::200e]:2376", "tcp://1.1.1.1:2376", "tcp://8.8.8.8:2376", "tcp://9.9.9.9:2376", "tcp://13.248.158.7:2376"} {
		if ms := errMsgs(sp.ValidateBackends(hb(h))); len(ms) != 0 {
			t.Errorf("%s refusé : %v", h, ms)
		}
	}
}

// Plages limites : l'adresse juste en dessous et juste au-dessus de chaque plage interdite est publique ; les bornes de la plage sont interdites.
var edgeV4 = []struct {
	ip string
	ok bool
}{
	{"0.255.255.255", false}, {"1.0.0.0", true},
	{"9.255.255.255", true}, {"10.0.0.0", false}, {"10.255.255.255", false}, {"11.0.0.0", true},
	{"100.63.255.255", true}, {"100.64.0.0", false}, {"100.127.255.255", false}, {"100.128.0.0", true},
	{"126.255.255.255", true}, {"127.0.0.0", false}, {"127.255.255.255", false}, {"128.0.0.0", true},
	{"169.253.255.255", true}, {"169.254.0.0", false}, {"169.254.255.255", false}, {"169.255.0.0", true},
	{"172.15.255.255", true}, {"172.16.0.0", false}, {"172.31.255.255", false}, {"172.32.0.0", true},
	{"191.255.255.255", true}, {"192.0.0.0", false}, {"192.0.0.255", false}, {"192.0.1.0", true},
	{"192.0.1.255", true}, {"192.0.2.0", false}, {"192.0.2.255", false}, {"192.0.3.0", true},
	{"192.88.98.255", true}, {"192.88.99.0", false}, {"192.88.99.255", false}, {"192.88.100.0", true},
	{"192.167.255.255", true}, {"192.168.0.0", false}, {"192.168.255.255", false}, {"192.169.0.0", true},
	{"198.17.255.255", true}, {"198.18.0.0", false}, {"198.19.255.255", false}, {"198.20.0.0", true},
	{"198.51.99.255", true}, {"198.51.100.0", false}, {"198.51.100.255", false}, {"198.51.101.0", true},
	{"203.0.112.255", true}, {"203.0.113.0", false}, {"203.0.113.255", false}, {"203.0.114.0", true},
	{"223.255.255.255", true}, {"224.0.0.0", false}, {"239.255.255.255", false}, {"240.0.0.0", false}, {"255.255.255.254", false},
}

var edgeV6 = []struct {
	ip string
	ok bool
}{
	{"2001:db7:ffff:ffff:ffff:ffff:ffff:ffff", true}, {"2001:db8::", false}, {"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"2001:db9::", true},
	{"2001:1::", true}, {"2001::", false}, {"2001:0:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"2001:2::", true},
	{"2001:4860:4860::8888", true}, {"2606:4700:4700::1111", true}, {"2620:fe::fe", true}, {"2a00:1450:4007:80d::200e", true},
	{"2001:4998:44:3507::8000", true},
	{"fbff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true}, {"fc00::", false}, {"fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"fe00::", true},
	{"fe7f:ffff:ffff:ffff:ffff:ffff:ffff:ffff", true}, {"fe80::", false}, {"febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false},
	{"fec0::", false}, {"ff00::", false}, {"::1", false}, {"::", false},
	{"2002::", false}, {"2002:ffff:ffff:ffff:ffff:ffff:ffff:ffff", false}, {"2003::", true}, {"64:ff9b::", false}, {"64:ff9b::ffff:ffff", false},
}

func TestPublic258PlagesLimitesLitterales(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	check := func(host string, ok bool, what string) {
		ms := errMsgs(sp.ValidateBackends(hb("tcp://" + host + ":2376")))
		if ok && len(ms) != 0 {
			t.Errorf("%s : %s public refusé : %v", what, host, ms)
		}
		if !ok && !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
			t.Errorf("%s : %s interdit accepté (%v)", what, host, ms)
		}
	}
	for _, e := range edgeV4 {
		check(e.ip, e.ok, "v4")
	}
	for _, e := range edgeV6 {
		check("["+e.ip+"]", e.ok, "v6")
	}
}

// Mêmes adresses limites obtenues par le DNS (A ou AAAA) : décision identique.
func TestPublic258PlagesLimitesParDNS(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	var cur string
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP(cur)}, nil })()
	for _, e := range append(append([]struct {
		ip string
		ok bool
	}{}, edgeV4...), edgeV6...) {
		cur = e.ip
		resetResolveCache()
		err := CheckUserHost("tcp://edge.example.org:2376")
		if e.ok && err != nil {
			t.Errorf("DNS → %s public refusé : %v", e.ip, err)
		}
		if !e.ok && err == nil {
			t.Errorf("DNS → %s interdit accepté", e.ip)
		}
	}
}

// IPv4 rendue en forme IPv4-mappée (::ffff:a.b.c.d) par le résolveur : jugée sur l'IPv4.
func TestPublic258IPv4MappeeParDNS(t *testing.T) {
	var cur string
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP(cur)}, nil })()
	for ip, ok := range map[string]bool{"::ffff:13.248.158.7": true, "::ffff:127.0.0.1": false, "::ffff:192.168.1.1": false, "::ffff:10.0.0.1": false} {
		cur = ip
		resetResolveCache()
		if err := CheckUserHost("tcp://yahoo.fr:2376"); (err == nil) != ok {
			t.Errorf("%s : accepté=%v, attendu %v", ip, err == nil, ok)
		}
	}
}

// Public + privé dans la même réponse : refus (une seule IP interdite suffit) ; public v4 + public v6 : accepté.
func TestPublic258ReponsesMixtes(t *testing.T) {
	m := map[string][]string{
		"ok.example.org":   {"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"},
		"mix4.example.org": {"93.184.216.34", "10.0.0.7"},
		"mix6.example.org": {"2606:2800:220:1:248:1893:25c8:1946", "fd00::1"},
		"mix7.example.org": {"2606:2800:220:1:248:1893:25c8:1946", "::1"},
	}
	defer SetUserHostResolverForTest(fakeDNS(m))()
	for h, ok := range map[string]bool{"ok.example.org": true, "mix4.example.org": false, "mix6.example.org": false, "mix7.example.org": false} {
		if err := CheckUserHost("tcp://" + h + ":2376"); (err == nil) != ok {
			t.Errorf("%s : accepté=%v, attendu %v", h, err == nil, ok)
		}
	}
}

// DNS en échec (erreur, liste vide, nom inconnu, délai) : refus ; jamais mis en cache (la reprise du DNS refonctionne).
func TestPublic258DNSEnEchec(t *testing.T) {
	var mode atomic.Int32 // 0 erreur, 1 vide, 2 nom inconnu, 3 normal
	defer SetUserHostResolverForTest(func(h string) ([]net.IP, error) {
		switch mode.Load() {
		case 0:
			return nil, errors.New("server misbehaving")
		case 1:
			return nil, nil
		case 2:
			return nil, &net.DNSError{Err: "no such host", Name: h, IsNotFound: true}
		}
		return []net.IP{net.ParseIP("13.248.158.7")}, nil
	})()
	for m := int32(0); m < 3; m++ {
		mode.Store(m)
		resetResolveCache()
		if err := CheckUserHost("tcp://yahoo.fr:2376"); err == nil || err.Error() != HostRefusal {
			t.Errorf("mode %d : %v (refus uniforme attendu)", m, err)
		}
	}
	mode.Store(3) // le DNS revient : l'échec précédent n'est pas resté en cache
	if err := CheckUserHost("tcp://yahoo.fr:2376"); err != nil {
		t.Errorf("après reprise du DNS : %v (un échec ne doit pas être mis en cache)", err)
	}
}

// Résolution lente (1,5 s) : acceptée à l'enregistrement ; résolveur bloqué : l'enregistrement rend la main (borné), en refus.
func TestPublic258ResolutionLenteEtBloquee(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) {
		time.Sleep(1500 * time.Millisecond)
		return []net.IP{net.ParseIP("13.248.158.7")}, nil
	})()
	if ms := errMsgs(sp.ValidateBackends(hb("tcp://yahoo.fr:2376"))); len(ms) != 0 {
		t.Errorf("résolution de 1,5 s refusée : %v", ms)
	}
	stop := make(chan struct{})
	defer close(stop)
	restore := SetUserHostResolverForTest(func(string) ([]net.IP, error) { <-stop; return nil, errors.New("bloqué") })
	defer restore()
	t0 := time.Now()
	ms := errMsgs(sp.ValidateBackends(hb("tcp://blocked.example.org:2376")))
	if d := time.Since(t0); d > 12*time.Second {
		t.Errorf("l'enregistrement attend un résolveur bloqué %v (doit être borné)", d)
	}
	if !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
		t.Errorf("résolveur bloqué : %v (refus attendu)", ms)
	}
}

// Plusieurs hôtes dans une même validation (kubeconfig à deux clusters, 10 backends) : validés en parallèle, pas en série de timeouts.
func TestPublic258ValidationParallele(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) {
		time.Sleep(600 * time.Millisecond)
		return []net.IP{net.ParseIP("13.248.158.7")}, nil
	})()
	var b strings.Builder
	b.WriteString("backends:\n")
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		b.WriteString("  " + n + ":\n    type: docker\n    host: tcp://" + n + ".example.org:2376\n    volume: v\n")
	}
	t0 := time.Now()
	if ms := errMsgs(sp.ValidateBackends([]byte(b.String()))); len(ms) != 0 {
		t.Errorf("8 hôtes publics refusés : %v", ms)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("8 résolutions de 0,6 s ont pris %v : validation séquentielle", d)
	}
	_ = os.Remove(sp.BackendsPath())
}
