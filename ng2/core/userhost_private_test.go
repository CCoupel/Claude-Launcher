package core

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// LOT-3.2 Batch 0 (#275, E1b) — users.allowPrivateNetworks (CL_USERS_ALLOW_PRIVATE, défaut false ; contracts/user-backends.md §3).
// AVEC le paramètre : 10/8, 172.16/12, 192.168/16, fc00::/7 et les noms internes qui y mènent sont admis pour un UTILISATEUR ;
// loopback, localhost*, 0.0.0.0, link-local et MÉTADONNÉES (169.254/16, 100.100.100.200, fd00:ec2::254 — pourtant dans fc00::/7),
// multicast, 100.64/10, 192.0.0/24, 198.18/15, documentation, 240/4, diffusion : TOUJOURS refusés. SANS : tout est refusé.
// Écrits avant le code ; la variable est lue à chaque appel (t.Setenv suffit). Aucun réseau : IP littérales, noms internes, DNS factice.

var privateOnlyHosts = []struct{ name, host string }{
	{"10/8 bas", "tcp://10.0.0.5:2375"}, {"10/8 haut", "tcp://10.255.255.254:2375"},
	{"172.16/12 bas", "tcp://172.16.0.1:2375"}, {"172.16/12 haut", "tcp://172.31.255.254:2375"},
	{"192.168/16", "tcp://192.168.1.211:2375"}, {"192.168/16 haut", "tcp://192.168.255.254:2375"},
	{"ULA fc00", "tcp://[fc00::1]:2375"}, {"ULA fd", "tcp://[fd12:3456:789a::1]:2375"},
	{"IPv4-mappée privée", "tcp://[::ffff:10.0.0.1]:2375"},
	{"nom .lan", "tcp://nas.lan:2376"}, {"nom .local", "tcp://backend.local:2375"}, {"nom .internal", "tcp://srv.internal:2375"},
	{"nom .home.arpa", "tcp://nas.home.arpa:2375"}, {"nom .svc", "tcp://docker.ns.svc:2375"},
	{"nom .cluster.local", "tcp://docker.ns.svc.cluster.local:2375"}, {"nom sans point", "tcp://intranet:2375"},
}

var alwaysRefusedHosts = []struct{ name, host string }{
	{"loopback", "tcp://127.0.0.1:2375"}, {"loopback autre", "tcp://127.8.9.10:2375"}, {"loopback v6", "tcp://[::1]:2375"},
	{"IPv4-mappée loopback", "tcp://[::ffff:127.0.0.1]:2375"}, {"localhost", "tcp://localhost:2375"}, {"localhost.", "tcp://localhost.:2375"},
	{"sous-domaine localhost", "tcp://app.localhost:2375"},
	{"0.0.0.0", "tcp://0.0.0.0:2375"}, {"::", "tcp://[::]:2375"},
	{"métadonnées 169.254.169.254", "tcp://169.254.169.254:80"}, {"link-local 169.254.1.1", "tcp://169.254.1.1:2375"},
	{"link-local v6", "tcp://[fe80::1]:2375"}, {"métadonnées alibaba", "tcp://100.100.100.200:80"},
	{"métadonnées aws v6 (dans fc00::/7)", "tcp://[fd00:ec2::254]:80"},
	{"CGNAT", "tcp://100.64.0.1:2375"}, {"192.0.0/24", "tcp://192.0.0.1:2375"}, {"198.18/15", "tcp://198.18.0.1:2375"},
	{"documentation 192.0.2", "tcp://192.0.2.1:2375"}, {"documentation 198.51.100", "tcp://198.51.100.1:2375"},
	{"documentation 203.0.113", "tcp://203.0.113.1:2375"}, {"documentation v6", "tcp://[2001:db8::1]:2375"},
	{"multicast", "tcp://224.0.0.1:2375"}, {"multicast v6", "tcp://[ff02::1]:2375"}, {"réservé 240/4", "tcp://240.0.0.1:2375"},
	{"diffusion", "tcp://255.255.255.255:2375"}, {"kubernetes.default", "tcp://kubernetes.default:443"},
}

func privCheck(t *testing.T, host string) []string {
	t.Helper()
	sp, _ := UserSpace("github-5")
	return errMsgs(sp.ValidateBackends(hb(host)))
}

func TestPrivate275SansLeParametreToutEstRefuse(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("CL_USERS_ALLOW_PRIVATE", "")
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.43.1.2")}, nil })()
	for _, f := range append(append([]struct{ name, host string }{}, privateOnlyHosts...), alwaysRefusedHosts...) {
		if ms := privCheck(t, f.host); !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
			t.Errorf("sans paramètre : %s (%s) accepté ou message non uniforme : %v", f.name, f.host, ms)
		}
	}
}

func TestPrivate275AvecLeParametreLesPlagesPriveesSontAdmises(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("CL_USERS_ALLOW_PRIVATE", "true")
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.43.1.2")}, nil })()
	for _, f := range privateOnlyHosts {
		if ms := privCheck(t, f.host); len(ms) != 0 {
			t.Errorf("avec paramètre : %s (%s) refusé : %v", f.name, f.host, ms)
		}
	}
}

func TestPrivate275AvecLeParametreLesReservesRestentRefusees(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("CL_USERS_ALLOW_PRIVATE", "true")
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.43.1.2")}, nil })()
	for _, f := range alwaysRefusedHosts {
		if ms := privCheck(t, f.host); !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
			t.Errorf("avec paramètre : %s (%s) accepté : %v (toujours refusé)", f.name, f.host, ms)
		}
	}
}

// Valeur du paramètre : seul « true » l'active ; toute autre valeur, absente ou illisible = refus (échec fermé).
func TestPrivate275ValeursDuParametre(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	for v, allowed := range map[string]bool{"true": true, "false": false, "": false, "0": false, "oui": false, "yes": false, "truee": false, " ": false} {
		t.Setenv("CL_USERS_ALLOW_PRIVATE", v)
		ms := privCheck(t, "tcp://192.168.1.211:2375")
		if allowed && len(ms) != 0 || !allowed && len(ms) == 0 {
			t.Errorf("CL_USERS_ALLOW_PRIVATE=%q : refus=%v (autorisé attendu : %v)", v, len(ms) != 0, allowed)
		}
	}
}

// DNS : avec le paramètre, un nom public qui résout vers une IP privée est admis ; vers loopback / métadonnées : refusé ; mixte privé + métadonnées : refusé.
func TestPrivate275ResolutionDNS(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	cases := map[string]struct {
		ips      []string
		with, wo bool // accepté avec / sans le paramètre
	}{
		"privé":               {[]string{"10.43.1.2"}, true, false},
		"privé + public":      {[]string{"10.43.1.2", "93.184.216.34"}, true, false},
		"ULA":                 {[]string{"fd12::5"}, true, false},
		"loopback":            {[]string{"127.0.0.1"}, false, false},
		"métadonnées":         {[]string{"169.254.169.254"}, false, false},
		"privé + métadonnées": {[]string{"10.43.1.2", "169.254.169.254"}, false, false},
		"aws v6":              {[]string{"fd00:ec2::254"}, false, false},
		"public":              {[]string{"93.184.216.34"}, true, true},
	}
	for name, c := range cases {
		c := c
		restore := SetUserHostResolverForTest(func(string) ([]net.IP, error) {
			var out []net.IP
			for _, s := range c.ips {
				out = append(out, net.ParseIP(s))
			}
			return out, nil
		})
		for _, mode := range []struct {
			env  string
			want bool
		}{{"true", c.with}, {"false", c.wo}} {
			t.Setenv("CL_USERS_ALLOW_PRIVATE", mode.env)
			resetResolveCache()
			if err := CheckUserHost("tcp://db.example.org:2376"); (err == nil) != mode.want {
				t.Errorf("%s, paramètre=%s : accepté=%v, attendu %v", name, mode.env, err == nil, mode.want)
			}
		}
		restore()
	}
}

// Revalidation avant CHAQUE appel, avec et sans le paramètre : le DNS qui passe du privé au loopback (ou le paramètre retiré) est refusé
// au second appel ; docker n'est pas lancé.
func TestPrivate275RevalidationAvantChaqueAppel(t *testing.T) {
	f := s257Tools(t)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("CL_USERS_ALLOW_PRIVATE", "true")
	cur := "10.43.1.2"
	defer SetUserHostResolverForTest(func(string) ([]net.IP, error) { return []net.IP{net.ParseIP(cur)}, nil })()
	defer userHostCacheTTL.Set(userHostCacheTTL.Set(5 * time.Millisecond))
	sp := s15Space(t, "github-9", "backends:\n  d:\n    type: docker\n    host: tcp://docker.lan.example.org:2376\n    volume: v\n")
	bs, err := sp.LoadBackends()
	if err != nil || len(bs) == 0 {
		t.Fatalf("backend privé non chargé avec le paramètre : %v", err)
	}
	if err := (Engine{bs[0]}).cmd("version").Run(); err != nil {
		t.Fatalf("premier appel (privé autorisé) : %v", err)
	}
	n := strings.Count(f.text(), "ARGV:")
	cur = "127.0.0.1" // rebinding vers loopback : refusé même avec le paramètre
	time.Sleep(20 * time.Millisecond)
	if err := (Engine{bs[0]}).cmd("version").Run(); err == nil || strings.Count(f.text(), "ARGV:") != n {
		t.Errorf("appel vers loopback accepté avec le paramètre (err=%v)", err)
	}
	cur = "10.43.1.2"
	time.Sleep(20 * time.Millisecond)
	if err := (Engine{bs[0]}).cmd("version").Run(); err != nil {
		t.Errorf("retour au privé autorisé : %v", err)
	}
	// le paramètre retiré (redéploiement) : l'appel suivant est refusé sans relire le backends.yaml
	t.Setenv("CL_USERS_ALLOW_PRIVATE", "false")
	n = strings.Count(f.text(), "ARGV:")
	time.Sleep(20 * time.Millisecond)
	if err := (Engine{bs[0]}).cmd("version").Run(); err == nil || strings.Count(f.text(), "ARGV:") != n {
		t.Errorf("paramètre retiré : l'appel vers le LAN est encore accepté (err=%v)", err)
	}
}

// L'admin n'est jamais concerné (ni avec ni sans le paramètre).
func TestPrivate275AdminSansContrainte(t *testing.T) {
	for _, v := range []string{"true", "false", ""} {
		t.Setenv("CL_USERS_ALLOW_PRIVATE", v)
		for _, f := range []string{"tcp://127.0.0.1:2375", "tcp://192.168.1.211:2375", "tcp://169.254.169.254:80", "tcp://nas.lan:2376"} {
			if ms := errMsgs(ValidateConfig(ConfBackends, hb(f))); strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
				t.Errorf("paramètre=%q : admin refusé pour %s", v, f)
			}
		}
	}
	_ = os.Getenv
}
