package desktop

import (
	"bytes"
	"reflect"
	"testing"
)

// Séquence réelle de tmux 3.6 en mode contrôle (attach, refresh-client,
// list-panes, sortie d'un pane), capturée sur un serveur de test.
func TestDecoderSequence(t *testing.T) {
	lines := []string{
		"%begin 1794564291 280 0",
		"%end 1794564291 280 0",
		"%session-changed $0 cl",
		"%begin 1794564291 285 1",
		"%end 1794564291 285 1",
		"%layout-change @0 baaa,120x40,0,0[120x20,0,0,0,120x19,0,21,1] baaa,120x40,0,0[120x20,0,0,0,120x19,0,21,1] *",
		`%output %0 \015\033[K\015bash-5.3$ `,
		"%begin 1794564291 289 1",
		"%0 120x20 0,0",
		"%1 120x19 0,21",
		"%end 1794564291 289 1",
		`%output %0 echo h\134llo\015\012`,
		"%begin 1 2 1",
		"parse error",
		"%error 1 2 1",
		"%exit",
	}
	var d decoder
	var replies []Reply
	var notifs []Notif
	for _, l := range lines {
		r, n := d.feed(l)
		if r != nil {
			replies = append(replies, *r)
		}
		if n != nil {
			notifs = append(notifs, *n)
		}
	}
	if len(replies) != 4 {
		t.Fatalf("réponses = %d, attendu 4", len(replies))
	}
	if string(replies[2].Body) != "%0 120x20 0,0\n%1 120x19 0,21\n" || replies[2].Err {
		t.Errorf("list-panes = %q", replies[2].Body)
	}
	if !replies[3].Err || string(replies[3].Body) != "parse error\n" {
		t.Errorf("erreur = %+v", replies[3])
	}
	var names []string
	for _, n := range notifs {
		names = append(names, n.Name)
	}
	if !reflect.DeepEqual(names, []string{"session-changed", "layout-change", "output", "output", "exit"}) {
		t.Errorf("notifications = %v", names)
	}
	if o := notifs[2]; o.Pane != "%0" || !bytes.Equal(o.Data, []byte("\r\x1b[K\rbash-5.3$ ")) {
		t.Errorf("output = %q %q", o.Pane, o.Data)
	}
	if o := notifs[3]; !bytes.Equal(o.Data, []byte("echo h\\llo\r\n")) {
		t.Errorf("antislash = %q", o.Data)
	}
	if l := notifs[1]; l.Args[0] != "@0" || l.Args[3] != "*" {
		t.Errorf("layout-change = %v", l.Args)
	}
}

func TestUnescapeOutput(t *testing.T) {
	for in, want := range map[string]string{
		`abc`:            "abc",
		`\033[1m\134x`:   "\x1b[1m\\x",
		`fin\0`:          `fin\0`,
		`\377\303\251`:   "\xff\xc3\xa9",
		`é\012`:          "é\n",
		`\08\0123`:       "\\08\n3",
		`trailing\`:      `trailing\`,
		`\00`:            `\00`,
		`\000x`:          "\x00x",
		`a\134\134b`:     `a\\b`,
		`\1340`:          `\0`,
		`\033]0;t\007`:   "\x1b]0;t\x07",
		`multi \011 tab`: "multi \t tab",
	} {
		if got := string(unescapeOutput(in)); got != want {
			t.Errorf("unescape(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestExtendedOutput(t *testing.T) {
	var d decoder
	_, n := d.feed(`%extended-output %4 12 : x\015\012`)
	if n == nil || n.Name != "output" || n.Pane != "%4" || string(n.Data) != "x\r\n" {
		t.Errorf("extended-output = %+v", n)
	}
}

func TestParseLayout(t *testing.T) {
	w, h, panes, err := parseLayout("baaa,120x40,0,0[120x20,0,0,0,120x19,0,21,1]")
	if err != nil || w != 120 || h != 40 {
		t.Fatalf("taille %dx%d err %v", w, h, err)
	}
	want := []PaneGeom{{"%0", 0, 0, 120, 20}, {"%1", 0, 21, 120, 19}}
	if !reflect.DeepEqual(panes, want) {
		t.Errorf("panes = %+v", panes)
	}
	// imbrication : leader + 3 teammates sur la 2e ligne
	_, _, panes, err = parseLayout("80x24,0,0[80x12,0,0,3,80x11,0,13{26x11,0,13,5,26x11,27,13,6,26x11,54,13,7}]")
	if err != nil || len(panes) != 4 || panes[3] != (PaneGeom{"%7", 54, 13, 26, 11}) {
		t.Errorf("imbriqué : %+v %v", panes, err)
	}
	// un seul pane, sans somme
	if _, _, p, err := parseLayout("80x24,0,0,12"); err != nil || len(p) != 1 || p[0].ID != "%12" {
		t.Errorf("seul : %+v %v", p, err)
	}
	for _, bad := range []string{"", "80x24", "80x24,0,0{", "abcd,80x24,0,0,1x"} {
		if _, _, _, err := parseLayout(bad); err == nil {
			t.Errorf("parseLayout(%q) sans erreur", bad)
		}
	}
}

func TestHexKeys(t *testing.T) {
	got := hexKeys([]byte("a\x1b"))
	if !reflect.DeepEqual(got, [][]string{{"61", "1b"}}) {
		t.Errorf("hexKeys = %v", got)
	}
	if n := len(hexKeys(make([]byte, 600))); n != 3 {
		t.Errorf("découpage : %d commandes", n)
	}
}

func TestUTF8Carry(t *testing.T) {
	e := []byte("é") // c3 a9
	c, r := utf8Carry(append([]byte("ab"), e[0]))
	if string(c) != "ab" || !bytes.Equal(r, e[:1]) {
		t.Errorf("coupé : %q %q", c, r)
	}
	euro := []byte("€") // 3 octets
	c, r = utf8Carry(append([]byte("x"), euro[:2]...))
	if string(c) != "x" || len(r) != 2 {
		t.Errorf("coupé à 2/3 : %q %q", c, r)
	}
	c, r = utf8Carry([]byte("x€"))
	if string(c) != "x€" || r != nil {
		t.Errorf("complet : %q %q", c, r)
	}
	c, r = utf8Carry([]byte("abc"))
	if string(c) != "abc" || r != nil {
		t.Errorf("ascii : %q %q", c, r)
	}
}

// Une ligne de sortie qui commence par « %end » (capture-pane d'un pane qui
// l'affiche) ne ferme pas la réponse : seul %end avec l'horodatage et le
// numéro du %begin la ferme (sinon toutes les réponses suivantes seraient
// décalées d'un appel).
func TestDecoderEndMatchesBegin(t *testing.T) {
	var d decoder
	var got []Reply
	for _, l := range []string{"%begin 1700 42 1", "%end 1 2 1", "%error 1700 43 1", "texte", "%end 1700 42 1", "%begin 1700 43 1", "%end 1700 43 1"} {
		if r, _ := d.feed(l); r != nil {
			got = append(got, *r)
		}
	}
	if len(got) != 2 || string(got[0].Body) != "%end 1 2 1\n%error 1700 43 1\ntexte\n" || got[0].Err || len(got[1].Body) != 0 {
		t.Errorf("réponses : %+v", got)
	}
}
