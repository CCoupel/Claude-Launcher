package desktop

import (
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
	return string(b)
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
