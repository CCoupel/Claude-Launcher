package core

import "testing"

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
