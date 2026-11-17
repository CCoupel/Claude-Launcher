package core

import (
	"strings"
	"testing"
)

// #171 : forme de l'adresse publique et correspondance de l'en-tête Host.
func TestParsePublicURL(t *testing.T) {
	good := map[string][3]string{ // adresse → origine, hôte, port
		"https://launcher.exemple":       {"https://launcher.exemple", "launcher.exemple", ""},
		"https://Launcher.Exemple:8443/": {"https://launcher.exemple:8443", "launcher.exemple", "8443"},
		"http://10.0.0.5:8080":           {"http://10.0.0.5:8080", "10.0.0.5", "8080"},
		"https://[::1]:9000":             {"https://[::1]:9000", "::1", "9000"},
	}
	for in, w := range good {
		p, err := ParsePublicURL(in)
		if err != nil || p.Origin != w[0] || p.Host != w[1] || p.Port != w[2] {
			t.Errorf("%q : %+v %v, attendu %v", in, p, err, w)
		}
	}
	for _, in := range []string{"", "launcher.exemple", "ftp://h", "https://", "https://u:p@h", "https://h/chemin", "https://h?x=1", "https://h#f", "https://h:0", "https://h:99999", "https://h?"} {
		if _, err := ParsePublicURL(in); err == nil {
			t.Errorf("%q accepté, attendu une erreur", in)
		}
	}
	p, _ := ParsePublicURL("https://launcher.exemple")
	if !p.HTTPS() || !p.MatchesHost("launcher.exemple") || !p.MatchesHost("LAUNCHER.exemple") || p.MatchesHost("launcher.exemple:443") || p.MatchesHost("autre.exemple") {
		t.Error("correspondance Host de l'adresse publique sans port")
	}
	q, _ := ParsePublicURL("https://launcher.exemple:8443")
	if !q.MatchesHost("launcher.exemple:8443") || q.MatchesHost("launcher.exemple") || q.MatchesHost("launcher.exemple:9") {
		t.Error("correspondance Host de l'adresse publique avec port")
	}
}

// LOT-4.4 audit M1 : « none » refusé dès qu'une adresse publique est en http, même sur une écoute en boucle locale.
func TestAccessNoneRefusalMatrix(t *testing.T) {
	https, _ := ParsePublicURL("https://launcher.exemple")
	http, _ := ParsePublicURL("http://launcher.exemple")
	cases := []struct {
		name     string
		access   string
		loopback bool
		pub      *PublicURL
		refused  bool
	}{
		{"token : jamais de refus", AccessToken, false, nil, false},
		{"none, boucle locale, sans adresse publique", AccessNone, true, nil, false},
		{"none, boucle locale, https public", AccessNone, true, &https, false},
		{"none, boucle locale, http public", AccessNone, true, &http, true},
		{"none, réseau, https public", AccessNone, false, &https, false},
		{"none, réseau, http public", AccessNone, false, &http, true},
		{"none, réseau, sans adresse publique", AccessNone, false, nil, true},
	}
	for _, c := range cases {
		if got := AccessNoneRefusal(c.access, c.loopback, c.pub) != ""; got != c.refused {
			t.Errorf("%s : refusé=%v, attendu %v", c.name, got, c.refused)
		}
	}
	if m := AccessNoneRefusal(AccessNone, true, &http); !strings.Contains(m, "http") || !strings.Contains(m, "https") {
		t.Errorf("message de l'adresse publique en http : %q", m)
	}
}
