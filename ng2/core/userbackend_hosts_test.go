package core

import (
	"os"
	"strings"
	"testing"
	"time"
)

// LOT-3.1 Batch 2 (#257) — bornage des hôtes d'un backend d'UTILISATEUR (S1–S9, S13, S14, D1 ; contrat user-backends.md §3).
// Décision 2026-11-27 : un utilisateur n'atteint que l'Internet public ; un ADMIN n'a aucune contrainte. Écrits avant le code,
// sans nom d'API imposé : validation de l'enregistrement (Space.ValidateBackends / SaveBackends) et refus AVANT l'appel
// (Space.LoadBackends → Engine.cli lancé avec un faux docker qui journalise). Aucune résolution réseau : hôtes en IP littérales
// ou en noms internes refusés sans DNS.

const hostRefusal = "adresse non autorisée pour un utilisateur"

// formes interdites à un utilisateur : (libellé, valeur de « host »)
var forbiddenHosts = []struct{ name, host string }{
	{"loopback v4", "tcp://127.0.0.1:2375"},
	{"loopback v4 autre", "tcp://127.8.9.10:2375"},
	{"loopback v6", "tcp://[::1]:2375"},
	{"localhost", "tcp://localhost:2375"},
	{"localhost point final", "tcp://localhost.:2375"},
	{"non spécifiée v4", "tcp://0.0.0.0:2375"},
	{"non spécifiée v6", "tcp://[::]:2375"},
	{"métadonnées cloud", "tcp://169.254.169.254:80"},
	{"métadonnées alibaba", "tcp://100.100.100.200:80"},
	{"métadonnées aws v6", "tcp://[fd00:ec2::254]:80"},
	{"link-local v6", "tcp://[fe80::1]:2375"},
	{"10/8", "tcp://10.0.0.5:2375"},
	{"172.16/12 bas", "tcp://172.16.0.1:2375"},
	{"172.16/12 haut", "tcp://172.31.255.254:2375"},
	{"192.168/16", "tcp://192.168.1.211:2375"},
	{"CGNAT 100.64/10", "tcp://100.64.0.1:2375"},
	{"ULA fc00::/7", "tcp://[fc00::1]:2375"},
	{"192.0.0/24", "tcp://192.0.0.1:2375"},
	{"198.18/15", "tcp://198.18.0.1:2375"},
	{"documentation 192.0.2/24", "tcp://192.0.2.1:2375"},
	{"multicast", "tcp://224.0.0.1:2375"},
	{"réservé 240/4", "tcp://240.0.0.1:2375"},
	{"diffusion", "tcp://255.255.255.255:2375"},
	{"IPv4-mappée loopback", "tcp://[::ffff:127.0.0.1]:2375"},
	{"IPv4-mappée privée", "tcp://[::ffff:10.0.0.1]:2375"},
	{"NAT64 vers loopback", "tcp://[64:ff9b::7f00:1]:2375"},
	{"6to4 vers loopback", "tcp://[2002:7f00:1::]:2375"},
	{"décimal", "tcp://2130706433:2375"},
	{"octal", "tcp://0177.0.0.1:2375"},
	{"hexadécimal", "tcp://0x7f000001:2375"},
	{"hexa par octet", "tcp://0x7f.0.0.1:2375"},
	{"forme courte", "tcp://127.1:2375"},
	{"nom .local", "tcp://backend.local:2375"},
	{"nom .lan", "tcp://nas.lan:2376"},
	{"nom .internal", "tcp://srv.internal:2375"},
	{"nom .home.arpa", "tcp://nas.home.arpa:2375"},
	{"nom .svc", "tcp://docker.ns.svc:2375"},
	{"nom .cluster.local", "tcp://docker.ns.svc.cluster.local:2375"},
	{"kubernetes.default", "tcp://kubernetes.default:443"},
	{"nom sans point", "tcp://intranet:2375"},
	{"userinfo", "tcp://user:pw@93.184.216.34:2375"},
	{"sans port", "tcp://93.184.216.34"},
	{"port 0", "tcp://93.184.216.34:0"},
	{"port hors plage", "tcp://93.184.216.34:65536"},
	{"port non numérique", "tcp://93.184.216.34:http"},
	{"chemin", "tcp://93.184.216.34:2375/x"},
	{"requête", "tcp://93.184.216.34:2375?a=1"},
	{"fragment", "tcp://93.184.216.34:2375#f"},
	{"zone IPv6", "tcp://[fe80::1%25eth0]:2375"},
	{"saut de ligne", "tcp://93.184.216.34:2375\nevil"},
	{"caractère de contrôle", "tcp://93.184.216.34\x00:2375"},
	{"espace", "tcp://93.184.216. 34:2375"},
	{"hôte trop long", "tcp://" + strings.Repeat("a", 250) + ".example.org:2375"},
	{"schéma http", "http://93.184.216.34:2375"},
	{"schéma ssh", "ssh://93.184.216.34"},
	{"unix", "unix:///var/run/docker.sock"},
}

var allowedHosts = []string{"tcp://93.184.216.34:2376", "tcp://[2606:2800:220:1:248:1893:25c8:1946]:2376"}

func hb(host string) []byte {
	return []byte("backends:\n  d:\n    type: docker\n    host: " + yamlQuote(host) + "\n")
}

func yamlQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\x00", `\0`)
	return `"` + r.Replace(s) + `"`
}

func errMsgs(ps []ConfigProblem) []string {
	var out []string
	for _, p := range ps {
		if p.Severity == SevError {
			out = append(out, p.Message)
		}
	}
	return out
}

func TestHosts257TableDesFormesInterditesAUnUtilisateur(t *testing.T) {
	if len(forbiddenHosts) < 25 {
		t.Fatalf("la table doit couvrir au moins 25 formes (%d)", len(forbiddenHosts))
	}
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	for _, f := range forbiddenHosts {
		t.Run(f.name, func(t *testing.T) {
			ms := errMsgs(sp.ValidateBackends(hb(f.host)))
			if len(ms) == 0 {
				t.Fatalf("%q accepté à l'enregistrement", f.host)
			}
			if !strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
				t.Errorf("%q : message non uniforme %q (attendu « %s »)", f.host, ms, hostRefusal)
			}
			if _, err := sp.SaveBackends(hb(f.host), time.Time{}); err == nil {
				t.Errorf("%q : SaveBackends l'a accepté", f.host)
			}
			if _, err := os.Stat(sp.BackendsPath()); err == nil {
				t.Errorf("%q : un backends.yaml refusé a été écrit", f.host)
				_ = os.Remove(sp.BackendsPath())
			}
		})
	}
}

// Le message ne dit RIEN de la raison : aucune fuite sur la topologie (S8) — pas de plage, d'IP résolue ni de motif.
func TestHosts257MessageUniformeSansDetail(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	seen := map[string]bool{}
	for _, f := range forbiddenHosts {
		for _, m := range errMsgs(sp.ValidateBackends(hb(f.host))) {
			if strings.Contains(m, hostRefusal) {
				seen[m] = true
				for _, leak := range []string{"loopback", "privé", "link-local", "10.0.0", "192.168", "169.254", "127.0.0.1", "métadonn"} {
					if strings.Contains(strings.ToLower(m), leak) {
						t.Errorf("%q : le message révèle « %s » : %s", f.host, leak, m)
					}
				}
			}
		}
	}
	if len(seen) != 1 {
		t.Errorf("%d messages différents au lieu d'un seul : %v", len(seen), seen)
	}
}

func TestHosts257HotesPublicsAcceptes(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	sp, _ := UserSpace("github-5")
	for _, h := range allowedHosts {
		if ms := errMsgs(sp.ValidateBackends(hb(h))); len(ms) != 0 {
			t.Errorf("hôte public %q refusé : %v", h, ms)
		}
	}
}

// L'admin n'a AUCUNE contrainte : mêmes hôtes (LAN, loopback, noms internes) acceptés — backends.yaml de l'admin inchangé.
func TestHosts257AdminAccepteLesMemesHotes(t *testing.T) {
	for _, f := range forbiddenHosts {
		switch f.name {
		case "loopback v4", "localhost", "métadonnées cloud", "10/8", "172.16/12 bas", "192.168/16", "nom .lan", "nom .local", "nom .internal",
			"nom sans point", "ULA fc00::/7", "CGNAT 100.64/10":
			ms := errMsgs(ValidateConfig(ConfBackends, hb(f.host)))
			if strings.Contains(strings.Join(ms, "\n"), hostRefusal) {
				t.Errorf("admin : %q refusé (« %s ») alors qu'un admin n'a aucune contrainte", f.host, hostRefusal)
			}
		}
	}
}

// Refus AVANT l'appel : un backends.yaml d'utilisateur édité à la main avec un hôte interdit ne lance jamais docker.
func TestHosts257RefusAvantLAppel(t *testing.T) {
	e := s15Setup(t) // faux kubectl ; ajouter un faux docker
	bin := os.Getenv("PATH")
	_ = bin
	dir := t.TempDir()
	log := dir + "/docker.log"
	script := "#!/bin/sh\necho \"ARGV: $*\" >> \"" + log + "\"\necho 1\n"
	if err := os.WriteFile(dir+"/docker", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = e
	for _, name := range []string{"loopback v4", "métadonnées cloud", "10/8", "192.168/16", "nom .lan", "IPv4-mappée loopback", "décimal"} {
		var host string
		for _, f := range forbiddenHosts {
			if f.name == name {
				host = f.host
			}
		}
		sp := s15Space(t, "github-7", string(hb(host)))
		bs, err := sp.LoadBackends()
		if err != nil || len(bs) == 0 {
			continue // refusé au chargement : échec fermé
		}
		_ = os.Remove(log)
		_, _ = Engine{bs[0]}.cmd("version").CombinedOutput()
		if b, _ := os.ReadFile(log); len(b) > 0 {
			t.Errorf("%s (%s) : docker lancé pour un hôte interdit :\n%s", name, host, b)
		}
	}
}

// SSH (P13) : refusé en mode serveur (utilisateur : à l'enregistrement ; admin : diagnostic au chargement) ; mode poste inchangé.
func TestHosts257SSHModeServeurEtModePoste(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	ssh := []byte("backends:\n  s:\n    type: ssh\n    host: srv1\n    shared: [/data]\n")
	sp, _ := UserSpace("github-5")
	if ms := errMsgs(sp.ValidateBackends(ssh)); len(ms) == 0 {
		t.Error("ssh accepté pour un utilisateur")
	}
	for _, tc := range []struct{ name, content string }{
		{"type ssh", string(ssh)},
		{"host ssh://", "backends:\n  d:\n    type: docker\n    host: ssh://user@srv1\n"},
		{"engine_host ssh://", "backends:\n  d:\n    type: podman\n    engine_host: ssh://user@srv1\n"},
		{"hosts (pool ssh)", "backends:\n  s:\n    type: ssh\n    hosts: [a, b]\n    shared: [/data]\n"},
	} {
		if ms := errMsgs(sp.ValidateBackends([]byte(tc.content))); len(ms) == 0 {
			t.Errorf("%s accepté pour un utilisateur", tc.name)
		}
	}
	// admin en mode serveur (pod) : diagnostic « ssh est interdit en mode serveur » ; le fichier reste lisible
	t.Setenv(InClusterVar, "1")
	b := BackendConf{Name: "s", Type: "ssh", Host: "srv1", Shared: []string{"/data"}}
	if txt := problemsText(Diagnose(b)); !strings.Contains(txt, "ssh est interdit en mode serveur") {
		t.Errorf("diagnostic d'un backend ssh en mode serveur : %q", txt)
	}
	if ms := errMsgs(ValidateConfig(ConfBackends, ssh)); len(ms) == 0 || !strings.Contains(strings.Join(ms, "\n"), "ssh est interdit en mode serveur") {
		t.Errorf("enregistrement ssh en mode serveur : %v", ms)
	}
	// mode poste : inchangé
	t.Setenv(InClusterVar, "")
	if txt := problemsText(Diagnose(b)); strings.Contains(txt, "interdit en mode serveur") {
		t.Errorf("mode poste : ssh refusé à tort : %q", txt)
	}
	if ms := errMsgs(ValidateConfig(ConfBackends, ssh)); strings.Contains(strings.Join(ms, "\n"), "interdit en mode serveur") {
		t.Errorf("mode poste : ssh refusé à l'enregistrement : %v", ms)
	}
}

func problemsText(ps []Problem) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.What+" "+p.Fix)
	}
	return strings.Join(s, "\n")
}
