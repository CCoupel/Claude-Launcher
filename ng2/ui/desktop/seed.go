package desktop

import (
	"fmt"
	"strconv"
	"strings"
)

// Amorçage d'un pane (#134, #135) : l'écran vient de la grille de tmux
// (capture-pane), les modes du terminal de ses formats.
//
// - capture-pane sans -N coupe les espaces de fin de ligne, même colorés :
//   une zone peinte par effacement (BCE, panneau latéral de Claude Code)
//   reprenait la couleur du pane ; l'écran visible est donc capturé avec -N.
// - le terminal de l'interface repart de zéro (reset) : les modes demandés
//   par l'application (suivi de la souris, curseur masqué, écran alternatif…)
//   seraient perdus ; ils sont relus dans tmux et rejoués.
// - l'historique de tmux est rejoué au-dessus de l'écran : la molette, sans
//   suivi de la souris, fait défiler l'historique comme le mode copie de la
//   coque.

// seedHistory : lignes d'historique rejouées au-dessus de l'écran.
const seedHistory = 2000

// modesFormat : position du curseur, taille de l'historique et modes du pane.
const modesFormat = "#{cursor_x} #{cursor_y} #{history_size} #{alternate_on} " +
	"#{mouse_standard_flag} #{mouse_button_flag} #{mouse_all_flag} #{mouse_sgr_flag} #{mouse_utf8_flag} " +
	"#{keypad_cursor_flag} #{keypad_flag} #{insert_flag} #{wrap_flag} #{cursor_flag} " +
	"#{scroll_region_upper} #{scroll_region_lower} #{pane_height}"

type paneModes struct {
	cx, cy, hist                      int
	alt                               bool
	mStd, mBtn, mAll, mSGR, mUTF8     bool
	ckm, keypad, insert, wrap, cursor bool
	top, bottom, height               int
}

// parseModes : réponse de display-message -p modesFormat ; un format inconnu
// d'un vieux tmux vaut 0 (curseur visible et retour à la ligne par défaut).
func parseModes(s string) paneModes {
	f := strings.Fields(s)
	n := func(i int) int {
		if i < len(f) {
			v, _ := strconv.Atoi(f[i])
			return v
		}
		return 0
	}
	b := func(i int) bool { return n(i) != 0 }
	m := paneModes{cx: n(0), cy: n(1), hist: n(2), alt: b(3),
		mStd: b(4), mBtn: b(5), mAll: b(6), mSGR: b(7), mUTF8: b(8),
		ckm: b(9), keypad: b(10), insert: b(11), wrap: b(12), cursor: b(13),
		top: n(14), bottom: n(15), height: n(16)}
	if len(f) < 14 {
		m.wrap, m.cursor = true, true
	}
	return m
}

// trackSGR : séquences SGR en vigueur après la ligne l (depuis la dernière
// remise à zéro) ; les rejouer dans l'ordre redonne les mêmes attributs.
func trackSGR(cur []string, l string) []string {
	for {
		i := strings.Index(l, "\x1b[")
		if i < 0 {
			return cur
		}
		l = l[i+2:]
		j := 0
		for j < len(l) && (l[j] >= '0' && l[j] <= '9' || l[j] == ';' || l[j] == ':') {
			j++
		}
		if j >= len(l) {
			return cur
		}
		if l[j] == 'm' {
			p := l[:j]
			if p == "" || p == "0" || strings.HasPrefix(p, "0;") {
				cur = cur[:0:0]
			}
			cur = append(cur, "\x1b["+p+"m")
		}
		l = l[j+1:]
	}
}

// seedData : flux qui reconstruit le pane dans le terminal de l'interface.
// hist : capture-pane -e de l'historique (nil s'il est vide ou en écran
// alternatif) ; screen : capture-pane -e -N de l'écran visible.
func seedData(m paneModes, hist, screen string) string {
	var b strings.Builder
	b.WriteString("\x1b[0m")
	if m.alt {
		b.WriteString("\x1b[?1049h") // l'écran normal reste dessous : rendu à la sortie
	}
	b.WriteString("\x1b[H\x1b[2J")
	// capture-pane -e n'émet que les changements d'attributs, d'une ligne à
	// l'autre comprise ; le passage à la ligne se fait attributs remis à zéro
	// (un défilement avec un fond courant peindrait la ligne neuve, BCE), puis
	// les attributs de fin de ligne sont rétablis
	var sgr []string
	lines := func(s string) {
		for i, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
			if i > 0 {
				b.WriteString("\x1b[0m\r\n" + strings.Join(sgr, ""))
			}
			b.WriteString(l)
			sgr = trackSGR(sgr, l)
		}
		b.WriteString("\x1b[0m")
		sgr = nil
	}
	if hist != "" && !m.alt {
		lines(hist)
		b.WriteString("\r\n")
	}
	lines(screen)
	if m.height > 0 && (m.top != 0 || m.bottom != m.height-1) && m.bottom > m.top {
		fmt.Fprintf(&b, "\x1b[%d;%dr", m.top+1, m.bottom+1)
	}
	fmt.Fprintf(&b, "\x1b[%d;%dH", m.cy+1, m.cx+1)
	set := func(on bool, code string) {
		if on {
			b.WriteString("\x1b[?" + code + "h")
		}
	}
	set(m.mStd, "1000")
	set(m.mBtn, "1002")
	set(m.mAll, "1003")
	set(m.mUTF8, "1005")
	set(m.mSGR, "1006")
	set(m.ckm, "1")
	if m.keypad {
		b.WriteString("\x1b=")
	}
	if m.insert {
		b.WriteString("\x1b[4h")
	}
	if !m.wrap {
		b.WriteString("\x1b[?7l")
	}
	if !m.cursor {
		b.WriteString("\x1b[?25l")
	}
	return b.String()
}
