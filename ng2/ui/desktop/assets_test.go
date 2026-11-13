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

// #132 : arbre compact par défaut (hauteur de ligne proche de la coque),
// confortable sur demande ; la ligne prend sa hauteur de la densité.
func TestTreeDensity(t *testing.T) {
	css, js := asset(t, "style.css"), asset(t, "app.js")
	px := func(re string) int {
		m := regexp.MustCompile(re).FindStringSubmatch(css)
		if m == nil {
			t.Fatalf("%s absent de style.css", re)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	compact := px(`:root \{ --rowh: (\d+)px;`)
	comfortable := px(`body\.comfortable \{ --rowh: (\d+)px;`)
	if compact > 24 || comfortable < 28 || compact >= comfortable {
		t.Errorf("hauteurs : compacte %d px, confortable %d px", compact, comfortable)
	}
	if !strings.Contains(css, "height: var(--rowh)") {
		t.Error("la ligne de l'arbre ne suit pas la densité")
	}
	if !strings.Contains(js, "setDensity(prefs.get('density', 'compact'), true)") {
		t.Error("densité par défaut : compacte, mémorisée")
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
	a := NewApp(r.emit)
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
