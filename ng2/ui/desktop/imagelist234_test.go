package desktop

import (
	"os"
	"os/exec"
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
	for _, want := range []string{"im.id = 'pjf-image'", "r.images", "d.image", "Autre…", "(aucune)", "pj.orig.image"} {
		if !strings.Contains(blk, want) {
			t.Errorf("select pjf-image : %q absent (liste complète, image courante ou saisie libre)", want)
		}
	}
	if !strings.Contains(body, "['Image de base', imBox") {
		t.Error("la ligne « Image de base » n'utilise pas le select")
	}
}

// Comportement du gestionnaire de changement (#234) : le bloc du <select> est
// extrait de settings.js et exécuté par node avec un DOM minimal simulé.
func TestProjectImageSelectBehaviour234(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node absent")
	}
	js := asset(t, "settings.js")
	a := strings.Index(js, "const im = el('select'")
	b := strings.Index(js, "const imBox")
	if a < 0 || b < a {
		t.Fatal("bloc du select pjf-image introuvable")
	}
	script := `
const el = (tag, cls, text) => ({ tag, cls, text, style: {}, children: [], handlers: {}, value: '',
  appendChild(c) { this.children.push(c); return c; },
  addEventListener(n, f) { this.handlers[n] = f; }, focus() {}, set textContent(v) {}, });
const stInput = (v, on) => { const i = el('input'); i.value = v; i.on = on; return i; };
const stRenderFoot = () => {};
const assert = require('assert');
function make(image) {
  const d = { image }, pj = { orig: { image: 'orig:1' } }, r = { images: ['a:1', 'b:2'] }, open = false;
  let im, imo;
  eval(process.env.SNIP.replace("const im =", "im =").replace("const imo =", "imo =") + "; globalThis.R = { im, imo };");
  return { d, im: R.im, imo: R.imo };
}
let x = make('a:1');
assert.deepStrictEqual(x.im.children.map(o => o.value), ['', 'a:1', 'b:2', '\u0000other']);
x.im.value = 'b:2'; x.im.handlers.change(); assert.strictEqual(x.d.image, 'b:2', 'image choisie');
x.im.value = ''; x.im.handlers.change(); assert.strictEqual(x.d.image, '', '(aucune)');
x.im.value = '\u0000other'; x.im.handlers.change(); assert.strictEqual(x.d.image, 'orig:1', 'Autre… vide');
assert.strictEqual(x.imo.style.display, '');
x.imo.on('  c:3 '); assert.strictEqual(x.d.image, 'c:3', 'saisie libre');
x.imo.on('  '); assert.strictEqual(x.d.image, 'orig:1', 'saisie vidée');
x.im.value = 'a:1'; x.im.handlers.change(); assert.strictEqual(x.imo.style.display, 'none');
x = make('zz:9'); assert.ok(x.im.children.some(o => o.value === 'zz:9'), 'image courante ajoutée');
`
	cmd := exec.Command(node, "-e", script)
	cmd.Env = append(os.Environ(), "SNIP="+js[a:b])
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
