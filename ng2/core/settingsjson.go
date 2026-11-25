package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Réécriture textuelle minimale d'un settings.json (#250, P4) : seules les valeurs des clés de PREMIER niveau
// demandées changent ; une clé absente est insérée en tête de l'objet racine ; tout le reste du fichier (autres
// clés, ordre, espaces, fin de ligne) est conservé octet pour octet.

// settingsSet : nouveau contenu de settings.json avec les clés (string) fixées, dans l'ordre de keys. src nil
// (fichier absent) : fichier neuf. Erreur : JSON illisible, racine non objet, clé en double au premier niveau.
func settingsSet(src []byte, keys []string, vals map[string]string) ([]byte, error) {
	if src == nil { // fichier absent ; un fichier vide est invalide
		var b strings.Builder
		b.WriteString("{\n")
		for i, k := range keys {
			fmt.Fprintf(&b, "  %s: %s", jsonStr(k), jsonStr(vals[k]))
			if i < len(keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("}\n")
		return []byte(b.String()), nil
	}
	if !json.Valid(src) {
		return nil, errors.New("JSON invalide")
	}
	type member struct {
		key        string
		vs, ve     int // étendue de la valeur
		keyStartWS int // début de l'espace précédant la clé
	}
	i := skipWS(src, 0)
	if i >= len(src) || src[i] != '{' {
		return nil, errors.New("la racine n'est pas un objet")
	}
	open := i
	i++
	var ms []member
	seen := map[string]bool{}
	for {
		ws := i
		i = skipWS(src, i)
		if i < len(src) && src[i] == '}' {
			break
		}
		ke, err := scanString(src, i)
		if err != nil {
			return nil, err
		}
		var key string
		if err := json.Unmarshal(src[i:ke], &key); err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fmt.Errorf("clé en double : %s", key)
		}
		seen[key] = true
		i = skipWS(src, ke)
		i++ // ':'
		vs := skipWS(src, i)
		ve, err := scanValue(src, vs)
		if err != nil {
			return nil, err
		}
		ms = append(ms, member{key, vs, ve, ws})
		i = skipWS(src, ve)
		if i < len(src) && src[i] == ',' {
			i++
		}
	}
	closeAt := i

	type edit struct {
		at, end int
		text    string
	}
	var edits []edit
	var missing []string
	for _, k := range keys {
		found := false
		for _, m := range ms {
			if m.key == k {
				edits = append(edits, edit{m.vs, m.ve, jsonStr(vals[k])})
				found = true
			}
		}
		if !found {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		if len(ms) == 0 { // objet vide : réécrit en bloc
			var b strings.Builder
			b.WriteString("{\n")
			for j, k := range missing {
				fmt.Fprintf(&b, "  %s: %s", jsonStr(k), jsonStr(vals[k]))
				if j < len(missing)-1 {
					b.WriteString(",")
				}
				b.WriteString("\n")
			}
			b.WriteString("}")
			edits = append(edits, edit{open, closeAt + 1, b.String()})
		} else {
			gap := string(src[open+1 : skipWS(src, open+1)]) // espace avant la première clé : indentation détectée
			var b strings.Builder
			for _, k := range missing {
				fmt.Fprintf(&b, "%s%s: %s,", gap, jsonStr(k), jsonStr(vals[k]))
			}
			edits = append(edits, edit{open + 1, open + 1, b.String()})
		}
	}
	// application de la fin vers le début
	for a := 0; a < len(edits); a++ {
		for b := a + 1; b < len(edits); b++ {
			if edits[b].at > edits[a].at {
				edits[a], edits[b] = edits[b], edits[a]
			}
		}
	}
	out := append([]byte(nil), src...)
	for _, e := range edits {
		out = append(out[:e.at], append([]byte(e.text), out[e.end:]...)...)
	}
	return out, nil
}

// settingsGet : valeur string d'une clé de premier niveau ("" si absente ou non string).
func settingsGet(src []byte, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(src, &m) != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func skipWS(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// scanString : fin (exclue) de la chaîne JSON qui commence en i.
func scanString(b []byte, i int) (int, error) {
	if i >= len(b) || b[i] != '"' {
		return 0, errors.New("chaîne attendue")
	}
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1, nil
		}
	}
	return 0, errors.New("chaîne non terminée")
}

// scanValue : fin (exclue) de la valeur JSON qui commence en i (le JSON est déjà validé).
func scanValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errors.New("valeur attendue")
	}
	switch b[i] {
	case '"':
		return scanString(b, i)
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '"':
				e, err := scanString(b, i)
				if err != nil {
					return 0, err
				}
				i = e - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, errors.New("valeur non terminée")
	}
	for i < len(b) && !strings.ContainsRune(",}] \t\r\n", rune(b[i])) { // nombre, true, false, null
		i++
	}
	return i, nil
}
