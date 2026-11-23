package desktop

import (
	"strings"
	"testing"
)

// #234 : « Image de base » des réglages projet. Un <input list=…> pré-rempli
// n'affiche dans son datalist que les options contenant la valeur saisie : seule
// l'image courante était proposée. La liste complète doit être proposée quelle
// que soit la valeur pré-remplie (select des images + « Autre… »).
func TestProjectImageFieldProposesFullList234(t *testing.T) {
	js := asset(t, "settings.js")
	i := strings.Index(js, "function stPageProjet(")
	if i < 0 {
		t.Fatal("stPageProjet absente de settings.js")
	}
	body := js[i:]
	if j := strings.Index(body[1:], "\nfunction "); j >= 0 {
		body = body[:j+1]
	}
	if strings.Contains(body, "setAttribute('list', 'pjimages')") {
		t.Error("l'image de base reste un <input list=pjimages> pré-rempli : le datalist filtre sur la valeur et ne montre que l'image courante")
	}
	k := strings.Index(body, "const im = el('select'")
	if k < 0 {
		t.Fatal("le champ pjf-image n'est pas un <select>")
	}
	blk := body[k:]
	if e := strings.Index(blk, "const imBox"); e >= 0 {
		blk = blk[:e]
	}
	for _, want := range []string{"im.id = 'pjf-image'", "r.images", "d.image", "Autre…"} {
		if !strings.Contains(blk, want) {
			t.Errorf("select pjf-image : %q absent (liste complète, image courante ou saisie libre)", want)
		}
	}
	if !strings.Contains(body, "['Image de base', imBox") {
		t.Error("la ligne « Image de base » n'utilise pas le select")
	}
}
