package desktop

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func asset(t *testing.T, name string) string {
	t.Helper()
	b, err := frontendFS.ReadFile("frontend/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n") // extraction Windows : CRLF possibles
}

// #132 : arbre compact à la densité de la coque (une ligne de texte, pas de
// marge verticale), décalage d'environ 1 à 1,5 caractère par niveau, chevron
// compris (la pastille d'un projet prend la place du chevron) ; une seule
// densité (le commutateur a été retiré à la demande de l'utilisateur).
func TestTreeCompactIndent(t *testing.T) {
	css, js, html := asset(t, "style.css"), asset(t, "app.js"), asset(t, "index.html")
	m := regexp.MustCompile(`:root \{ (--rowh:[^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("variables de l'arbre absentes de style.css")
	}
	c := map[string]int{}
	for _, v := range regexp.MustCompile(`--([a-z]+): (\d+)px`).FindAllStringSubmatch(m[1], -1) {
		c[v[1]], _ = strconv.Atoi(v[2])
	}
	const char = 8 // largeur moyenne d'un caractère à 12,5 px (7,6 px mesurés)
	if c["rowh"] > 18 || c["rowgap"] != 0 {
		t.Errorf("ligne : %d px, marge %d px", c["rowh"], c["rowgap"])
	}
	// niveau : décalage ; catégorie → projet : décalage + pastille − chevron
	if c["indent"] < char || c["indent"] > 3*char/2 || c["indent"]+c["dotw"]-c["chevw"] > 3*char/2 {
		t.Errorf("décalage : %d px par niveau (chevron %d, pastille %d)", c["indent"], c["chevw"], c["dotw"])
	}
	if !strings.Contains(css, "height: var(--rowh)") || !strings.Contains(js, "calc(var(--indent) * ") || !strings.Contains(css, ".row .chev.none { display: none; }") {
		t.Error("hauteur, décalage par niveau ou pastille à la place du chevron")
	}
	for _, w := range []string{"comfortable", "densityseg", "setDensity"} {
		if strings.Contains(css+js+html, w) {
			t.Errorf("commutateur de densité encore présent : %s", w)
		}
	}
}

// En-tête des panes (retour du test de desktop6) : bandeau clair, texte
// foncé (contraste fort sur le fond coloré des panes), couleur du pane en
// barre à gauche ; pane actif en blanc, souligné.
func TestPaneHeaderVisible(t *testing.T) {
	css, js := asset(t, "style.css"), asset(t, "app.js")
	m := regexp.MustCompile(`\.phead \{[^}]*background: #([0-9A-Fa-f]{6}); color: #([0-9A-Fa-f]{6});[^}]*border-left: 6px solid var\(--pbg`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("en-tête : fond, texte ou barre de couleur absents")
	}
	lum := func(h string) float64 {
		n, _ := strconv.ParseUint(h, 16, 32)
		return (0.2126*float64(n>>16) + 0.7152*float64(n>>8&255) + 0.0722*float64(n&255)) / 255
	}
	if lum(m[1]) < 0.8 || lum(m[2]) > 0.2 {
		t.Errorf("contraste de l'en-tête : fond #%s, texte #%s", m[1], m[2])
	}
	if !strings.Contains(css, ".phead.focus { background: #FFFFFF;") || !strings.Contains(js, "pv.head.style.setProperty('--pbg', p.bg") {
		t.Error("pane actif en blanc, couleur du pane transmise à l'en-tête")
	}
}

// #133 : thème Auto / Jour / Nuit — Jour et Nuit forcés par data-theme, Auto
// suit le système (prefers-color-scheme, sans data-theme) ; choix mémorisé,
// barre de titre de la fenêtre native suivie (setTheme), terminaux compris.
func TestThemeSwitch(t *testing.T) {
	css, js := asset(t, "style.css"), asset(t, "app.js")
	for _, want := range []string{
		`:root[data-theme="light"] {`,
		"@media (prefers-color-scheme: light) {\n  :root:not([data-theme=\"dark\"]) {",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css : %q absent", want)
		}
	}
	for _, want := range []string{"setTheme(prefs.get('theme', 'auto'), true)", "darkMQ.addEventListener('change'", "theme: termTheme()"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js : %q absent", want)
		}
	}
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	var got []string
	themeHook = func(m string) { got = append(got, m) }
	t.Cleanup(func() { themeHook = nil })
	for _, m := range []string{"light", "dark", "auto"} {
		if _, err := a.Dispatch("setTheme", json.RawMessage(`{"mode":"`+m+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(got, ",") != "light,dark,auto" {
		t.Errorf("fenêtre : %v", got)
	}
}
