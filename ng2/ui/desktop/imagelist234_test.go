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
	if !strings.Contains(body, "el('select'") || !strings.Contains(body, "pjf-image") {
		t.Error("aucun <select> (id pjf-image) proposant la liste complète des images")
	}
}
