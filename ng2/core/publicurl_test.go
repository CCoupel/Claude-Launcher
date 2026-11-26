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
	for _, in := range []string{"", "launcher.exemple", "ftp://h", "https://", "https://u:p@h", "https://h?x=1", "https://h#f", "https://h:0", "https://h:99999", "https://h?"} {
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

// Contrat §9 (addendum 4) : l'adresse publique admet un sous-chemin (la « base ») — segments ^[a-z0-9-]+, sans « / » final, sans « // »
// ni « .. » ; l'origine reste celle d'Origin (jamais de chemin) ; la racine « / » = base vide.
func TestParsePublicURLBase254(t *testing.T) {
	good := map[string][3]string{ // adresse → origine, base, URL()
		"https://coupel.net/claude-qualif": {"https://coupel.net", "/claude-qualif", "https://coupel.net/claude-qualif"},
		"https://Coupel.Net/claude-qualif": {"https://coupel.net", "/claude-qualif", "https://coupel.net/claude-qualif"},
		"https://h/a":                      {"https://h", "/a", "https://h/a"},
		"https://h/a/b-c/d0":               {"https://h", "/a/b-c/d0", "https://h/a/b-c/d0"},
		"https://h:8443/v1/qualif":         {"https://h:8443", "/v1/qualif", "https://h:8443/v1/qualif"},
		"http://10.0.0.5:8080/x":           {"http://10.0.0.5:8080", "/x", "http://10.0.0.5:8080/x"},
		"https://[::1]:9000/x":             {"https://[::1]:9000", "/x", "https://[::1]:9000/x"},
		"https://h":                        {"https://h", "", "https://h"},
		"https://h/":                       {"https://h", "", "https://h"},
		"https://h:443/claude-qualif":      {"https://h", "/claude-qualif", "https://h/claude-qualif"},
	}
	for in, w := range good {
		p, err := ParsePublicURL(in)
		if err != nil || p.Origin != w[0] || p.Base != w[1] || p.URL() != w[2] {
			t.Errorf("%q : %+v %v, attendu origine %s base %q URL %s", in, p, err, w[0], w[1], w[2])
		}
		if strings.Contains(p.Origin, "/claude") || strings.HasSuffix(p.Base, "/") {
			t.Errorf("%q : origine avec chemin ou base avec « / » final : %+v", in, p)
		}
	}
	for _, in := range []string{
		"https://h//", "https://h//a", "https://h/a//b", "https://h/a/../b", "https://h/..", "https://h/.", "https://h/a/./b",
		"https://h/%2e%2e", "https://h/a/%2e%2e/b", "https://h/%2E%2E/a", "https://h/a%2fb", "https://h/a%2Fb", "https://h/a%00", "https://h/%", "https://h/a%", "https://h/a%zz", "https://h/a%2", "https://h/%2f", "https://h/a%25b", "https://h/%61",
		"https://h/a/", "https://h/a/b/", "https://h/A", "https://h/Claude-Qualif", "https://h/a_b", "https://h/a b", "https://h/a%20b",
		"https://h/é", "https://h/a;b", "https://h/a:b", "https://h/a@b", "https://h/a\\b", "https://h/a*", "https://h/a(b)",
		"https://h/a?x=1", "https://h/a?", "https://h/a#f", "https://h/a/#f", "https://h/#f", "https://h?x=1",
		"ftp://h/a", "h/a", "https:///a", "https://h:0/a", "https://h:99999/a", "https://u:p@h/a",
	} {
		if p, err := ParsePublicURL(in); err == nil {
			t.Errorf("%q accepté (%+v), attendu une erreur", in, p)
		}
	}
	// l'en-tête Host ne porte jamais le chemin : la correspondance d'hôte est inchangée par la base
	p, _ := ParsePublicURL("https://coupel.net/claude-qualif")
	if !p.MatchesHost("coupel.net") || !p.MatchesHost("COUPEL.net") || p.MatchesHost("coupel.net/claude-qualif") || p.MatchesHost("autre.net") {
		t.Error("correspondance Host de l'adresse publique avec base")
	}
	// ValidBasePath : même règle, exposée
	for in, want := range map[string]bool{"/claude-qualif": true, "/a/b": true, "": false, "/": false, "/a/": false, "//a": false, "/a/../b": false, "/A": false, "a": false} {
		if got := ValidBasePath(in); got != want {
			t.Errorf("ValidBasePath(%q) = %v, attendu %v", in, got, want)
		}
	}
}
