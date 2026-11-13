package desktop

import (
	"fmt"
	"strconv"
	"strings"
)

// ── Protocole du mode contrôle de tmux (tmux -C) ──────────────────────────
//
// Le client de contrôle reçoit des lignes :
//   - réponses aux commandes, encadrées par « %begin t n f » … « %end t n f »
//     (ou « %error t n f »), une par commande, dans l'ordre d'envoi ;
//   - notifications, hors de ces blocs : « %output %3 données » (sortie d'un
//     pane, octets < 32 et « \ » en octal \ooo), « %layout-change @1 layout
//     visible flags », « %window-add @2 », « %exit », etc.
// Décodage pur, testé sans tmux.

// Notif : une notification (Name sans « % », Args : le reste de la ligne
// découpé en mots, sauf pour %output dont Data porte les octets décodés).
type Notif struct {
	Name string
	Args []string
	Pane string // %output, %extended-output : pane
	Data []byte // %output : octets décodés
}

// Reply : réponse à une commande.
type Reply struct {
	Body []byte
	Err  bool // %error
}

// decoder : lignes → réponses et notifications.
type decoder struct {
	in   bool
	tag  string // « horodatage numéro » du %begin en cours
	body []byte
}

// blockTag : « horodatage numéro » d'une ligne %begin / %end / %error
// (le drapeau qui suit est ignoré).
func blockTag(rest string) string {
	f := strings.Fields(rest)
	if len(f) < 2 {
		return ""
	}
	return f[0] + " " + f[1]
}

// feed : une ligne (sans le saut de ligne) ; rend une réponse complète ou une
// notification (au plus une des deux). Dans un bloc, seule la ligne %end ou
// %error qui porte l'horodatage et le numéro du %begin le ferme : une ligne
// de sortie qui commence par « %end » (capture-pane…) reste dans la réponse.
func (d *decoder) feed(line string) (r *Reply, n *Notif) {
	line = strings.TrimSuffix(line, "\r")
	if d.in {
		for _, end := range []string{"%end ", "%error "} {
			if rest, ok := strings.CutPrefix(line, end); ok && blockTag(rest) == d.tag {
				d.in = false
				return &Reply{Body: d.body, Err: end == "%error "}, nil
			}
		}
		d.body = append(d.body, line...)
		d.body = append(d.body, '\n')
		return nil, nil
	}
	if rest, ok := strings.CutPrefix(line, "%begin "); ok {
		d.in, d.body, d.tag = true, nil, blockTag(rest)
		return nil, nil
	}
	if !strings.HasPrefix(line, "%") {
		return nil, nil // hors protocole (DCS de -CC, bruit) : ignoré
	}
	name, rest, _ := strings.Cut(line[1:], " ")
	switch name {
	case "output":
		pane, data, _ := strings.Cut(rest, " ")
		return nil, &Notif{Name: name, Pane: pane, Data: unescapeOutput(data)}
	case "extended-output": // %extended-output %3 age ... : données
		f := strings.SplitN(rest, " : ", 2)
		pane, _, _ := strings.Cut(f[0], " ")
		var data []byte
		if len(f) == 2 {
			data = unescapeOutput(f[1])
		}
		return nil, &Notif{Name: "output", Pane: pane, Data: data}
	}
	return nil, &Notif{Name: name, Args: strings.Fields(rest)}
}

// unescapeOutput : « \ooo » (octal, 3 chiffres) → octet ; le reste tel quel.
func unescapeOutput(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && isOct(s, i+1) {
			v, _ := strconv.ParseUint(s[i+1:i+4], 8, 8)
			out = append(out, byte(v))
			i += 3
			continue
		}
		out = append(out, c)
	}
	return out
}

func isOct(s string, i int) bool {
	if i+3 > len(s) {
		return false
	}
	for k := i; k < i+3; k++ {
		if s[k] < '0' || s[k] > '7' {
			return false
		}
	}
	return true
}

// ── Chaînes de layout ─────────────────────────────────────────────────────

// PaneGeom : position et taille d'un pane, en cellules.
type PaneGeom struct {
	ID         string `json:"id"` // « %3 »
	X, Y, W, H int
}

// parseLayout : panes d'une chaîne de layout tmux (« b25d,80x24,0,0{40x24,0,0,1,39x24,41,0,2} »),
// somme de contrôle en tête facultative ; taille de la fenêtre et feuilles.
func parseLayout(s string) (w, h int, panes []PaneGeom, err error) {
	if i := strings.IndexByte(s, ','); i == 4 && !strings.Contains(s[:i], "x") {
		s = s[i+1:]
	}
	p := &layoutParser{s: s}
	cell, err := p.cell()
	if err != nil {
		return 0, 0, nil, err
	}
	if p.i != len(p.s) {
		return 0, 0, nil, fmt.Errorf("layout : reste %q", p.s[p.i:])
	}
	var walk func(c layoutCell)
	walk = func(c layoutCell) {
		if c.kids == nil {
			panes = append(panes, PaneGeom{ID: "%" + strconv.Itoa(c.id), X: c.x, Y: c.y, W: c.w, H: c.h})
			return
		}
		for _, k := range c.kids {
			walk(k)
		}
	}
	walk(cell)
	return cell.w, cell.h, panes, nil
}

type layoutCell struct {
	w, h, x, y, id int
	kids           []layoutCell
}

type layoutParser struct {
	s string
	i int
}

func (p *layoutParser) num() (int, error) {
	j := p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	if j == p.i {
		return 0, fmt.Errorf("layout : nombre attendu en %d", j)
	}
	return strconv.Atoi(p.s[j:p.i])
}

func (p *layoutParser) want(c byte) error {
	if p.i >= len(p.s) || p.s[p.i] != c {
		return fmt.Errorf("layout : %q attendu en %d", c, p.i)
	}
	p.i++
	return nil
}

func (p *layoutParser) cell() (layoutCell, error) {
	var c layoutCell
	var err error
	if c.w, err = p.num(); err != nil {
		return c, err
	}
	if err = p.want('x'); err != nil {
		return c, err
	}
	if c.h, err = p.num(); err != nil {
		return c, err
	}
	for _, f := range []*int{&c.x, &c.y} {
		if err = p.want(','); err != nil {
			return c, err
		}
		if *f, err = p.num(); err != nil {
			return c, err
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == '{' || p.s[p.i] == '[') {
		end := byte('}')
		if p.s[p.i] == '[' {
			end = ']'
		}
		p.i++
		c.kids = []layoutCell{}
		for {
			k, err := p.cell()
			if err != nil {
				return c, err
			}
			c.kids = append(c.kids, k)
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if err := p.want(end); err != nil {
				return c, err
			}
			return c, nil
		}
	}
	if err = p.want(','); err != nil {
		return c, err
	}
	c.id, err = p.num()
	return c, err
}

// ── Commandes ─────────────────────────────────────────────────────────────

// hexKeys : arguments de « send-keys -H » pour des octets (au plus 256 par
// commande : les longues saisies sont découpées).
func hexKeys(b []byte) [][]string {
	var out [][]string
	for len(b) > 0 {
		n := min(len(b), 256)
		args := make([]string, 0, n)
		for _, c := range b[:n] {
			args = append(args, fmt.Sprintf("%02x", c))
		}
		out = append(out, args)
		b = b[n:]
	}
	return out
}

// utf8Carry : découpe b en une partie UTF-8 complète et un reliquat (suite
// d'octets de tête d'un caractère coupé), à recoller au paquet suivant : la
// sortie d'un pane arrive par morceaux arbitraires.
func utf8Carry(b []byte) (complete, rest []byte) {
	n := len(b)
	for k := 1; k <= 3 && k <= n; k++ {
		c := b[n-k]
		if c&0xC0 == 0x80 {
			continue // octet de suite : remonter
		}
		need := 0
		switch {
		case c&0xE0 == 0xC0:
			need = 2
		case c&0xF0 == 0xE0:
			need = 3
		case c&0xF8 == 0xF0:
			need = 4
		}
		if need > k {
			return b[:n-k], b[n-k:]
		}
		break
	}
	return b, nil
}
