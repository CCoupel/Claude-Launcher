package core

import (
	"strings"
	"testing"
)

// #171 (cycle 1, audit sécurité F2 et revue M1) — ParsePublicURL : hôte strict (RFC 1123 ou IP littérale), IDN en
// punycode, ponctuation et « % » refusés, port par défaut du schéma retiré. Contrat : contracts/desktop-web.md.
func TestParsePublicURLNormalisation(t *testing.T) {
	for in, w := range map[string][3]string{ // adresse → origine, hôte, port
		"https://launcher.exemple:443":       {"https://launcher.exemple", "launcher.exemple", ""},           // port par défaut retiré
		"http://launcher.exemple:80":         {"http://launcher.exemple", "launcher.exemple", ""},            // idem
		"https://launcher.exemple:80":        {"https://launcher.exemple:80", "launcher.exemple", "80"},      // 80 n'est pas le défaut d'https
		"http://launcher.exemple:443":        {"http://launcher.exemple:443", "launcher.exemple", "443"},     // ni 443 celui d'http
		"https://LAUNCHER.Exemple/":          {"https://launcher.exemple", "launcher.exemple", ""},           // casse, « / » final
		"https://bücher.example":             {"https://xn--bcher-kva.example", "xn--bcher-kva.example", ""}, // IDN → punycode
		"https://xn--bcher-kva.example:8443": {"https://xn--bcher-kva.example:8443", "xn--bcher-kva.example", "8443"},
		"https://192.168.1.10:8443":          {"https://192.168.1.10:8443", "192.168.1.10", "8443"},
		"https://[2001:db8::1]":              {"https://[2001:db8::1]", "2001:db8::1", ""},
		"https://[2001:db8::1]:8443":         {"https://[2001:db8::1]:8443", "2001:db8::1", "8443"},
		"https://srv-1.int.exemple":          {"https://srv-1.int.exemple", "srv-1.int.exemple", ""},
		"https://localhost:8787":             {"https://localhost:8787", "localhost", "8787"},
	} {
		p, err := ParsePublicURL(in)
		if err != nil || p.Origin != w[0] || p.Host != w[1] || p.Port != w[2] {
			t.Errorf("%q : %+v %v, attendu origine=%s hôte=%s port=%s", in, p, err, w[0], w[1], w[2])
		}
	}
}

func TestParsePublicURLRefusesBadHosts(t *testing.T) {
	for _, in := range []string{
		"https://launcher%2eexemple",      // « % » dans l'hôte
		"https://launcher.exemple%00",     // idem, octet nul encodé
		"https://launcher_exemple",        // « _ » : hors RFC 1123
		"https://launcher!.exemple",       // ponctuation
		"https://launch er.exemple",       // espace
		"https://launcher.exemple.:8443x", // port non numérique
		"https://-launcher.exemple",       // label commençant par « - »
		"https://launcher-.exemple",       // label finissant par « - »
		"https://launcher..exemple",       // label vide
		"https://launch\x7fer.exemple",    // caractère de contrôle
		"https://launcher.exemple,evil.test",
		"https://launcher.exemple;x",
		"https://[::1%25eth0]",                            // zone IPv6
		"https://" + strings.Repeat("a", 64) + ".exemple", // label de plus de 63 caractères
		"https://[fe80::1",                                // crochet non fermé
	} {
		if p, err := ParsePublicURL(in); err == nil {
			t.Errorf("%q accepté : %+v", in, p)
		}
	}
}

// Un hôte homographe (IDN) ne se confond pas avec l'ASCII : les deux ont des formes normalisées différentes.
func TestParsePublicURLIDNNotConfusedWithASCII(t *testing.T) {
	a, err1 := ParsePublicURL("https://exemple.test")
	b, err2 := ParsePublicURL("https://еxemple.test") // « е » cyrillique
	if err1 != nil || err2 != nil {
		t.Fatalf("%v %v", err1, err2)
	}
	if a.Host == b.Host || !strings.HasPrefix(b.Host, "xn--") {
		t.Errorf("hôte cyrillique %q confondu avec %q (attendu un punycode distinct)", b.Host, a.Host)
	}
}
