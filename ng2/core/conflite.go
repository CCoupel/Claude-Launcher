package core

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// ── config.conf sans bash (Windows, #110) ─────────────────────────────────
//
// Sous Linux, config.conf est « sourcé » par bash (LoadConfig). Sous Windows il
// n'y a pas de bash fiable (celui de System32 démarre WSL, ce qu'on s'interdit) :
// confLite lit le sous-ensemble du format qu'utilise la config du launcher —
// NOM=valeur, NOM=( … ) sur une ou plusieurs lignes, declare -A NOM=( [clé]=valeur ),
// guillemets simples et doubles, $NOM / ${NOM} (dont $HOME), commentaires, « ; » —
// et produit les mêmes lignes « TAG<TAB>valeur » que le script bash, lues par
// le même code.

func readFileOr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

type confTok struct {
	kind byte // 'w' mot, ';' fin d'instruction, '(' ')'
	s    string
}

// confTokens : mots du fichier, guillemets retirés, variables développées par
// get (au moment de la lecture : les affectations précédentes comptent, donc
// le découpage se fait instruction par instruction, cf. confLite).
func confTokens(src string, i int, get func(string) string) (toks []confTok, next int) {
	var w strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			toks = append(toks, confTok{'w', w.String()})
			w.Reset()
			inWord = false
		}
	}
	expand := func(j int) int { // src[j] == '$'
		if j+1 < len(src) && src[j+1] == '{' {
			end := strings.IndexByte(src[j+2:], '}')
			if end < 0 {
				w.WriteByte('$')
				return j + 1
			}
			name := src[j+2 : j+2+end]
			w.WriteString(get(name))
			return j + 3 + end
		}
		k := j + 1
		for k < len(src) && (src[k] == '_' || src[k] >= 'A' && src[k] <= 'Z' || src[k] >= 'a' && src[k] <= 'z' || k > j+1 && src[k] >= '0' && src[k] <= '9') {
			k++
		}
		if k == j+1 {
			w.WriteByte('$')
			return j + 1
		}
		w.WriteString(get(src[j+1 : k]))
		return k
	}
	depth := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\'':
			inWord = true
			end := strings.IndexByte(src[i+1:], '\'')
			if end < 0 {
				end = len(src) - i - 1
			}
			w.WriteString(src[i+1 : i+1+end])
			i += end + 2
		case c == '"':
			inWord = true
			i++
			for i < len(src) && src[i] != '"' {
				switch {
				case src[i] == '\\' && i+1 < len(src) && strings.IndexByte("\"\\$`", src[i+1]) >= 0:
					w.WriteByte(src[i+1])
					i += 2
				case src[i] == '$':
					i = expand(i)
				default:
					w.WriteByte(src[i])
					i++
				}
			}
			i++
		case c == '\\' && i+1 < len(src):
			if src[i+1] != '\n' {
				inWord = true
				w.WriteByte(src[i+1])
			}
			i += 2
		case c == '$':
			inWord = true
			i = expand(i)
		case c == '#' && !inWord:
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '(' && inWord && strings.HasSuffix(w.String(), "="):
			flush()
			toks = append(toks, confTok{'(', ""})
			depth++
			i++
		case c == ')' && depth > 0:
			flush()
			toks = append(toks, confTok{')', ""})
			depth--
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' && depth > 0:
			flush()
			i++
		case c == '\n' || c == ';':
			flush()
			return append(toks, confTok{';', ""}), i + 1
		case c == '~' && tildeAt(w.String(), inWord) && tildeEnds(src, i+1):
			// ~ non quoté en tête de mot, ou après « = » / « : » d'une affectation : $HOME
			inWord = true
			w.WriteString(get("HOME"))
			i++
		default:
			inWord = true
			w.WriteByte(c)
			i++
		}
	}
	flush()
	return toks, i
}

// tildeAt : un « ~ » à cet endroit est-il développé par bash ? En tête de mot,
// ou dans une affectation (NOM=…) juste après « = » ou « : ».
func tildeAt(cur string, inWord bool) bool {
	if !inWord {
		return true
	}
	name, val, ok := strings.Cut(cur, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return val == "" || strings.HasSuffix(val, ":")
}

// tildeEnds : le « ~ » est seul (pas « ~utilisateur ») : suivi de « / », d'un
// séparateur ou de la fin.
func tildeEnds(src string, j int) bool {
	return j >= len(src) || strings.IndexByte("/ \t\r\n;):", src[j]) >= 0
}

// confLite : lignes « TAG<TAB>valeur » de LoadConfig pour le contenu src.
func confLite(src, home string) string {
	vars := map[string]string{
		"HOME": home, "GITHUB_DIR": home + "/GITHUB", "USAGE_REFRESH_SEC": "120", "GITHUB_TOKEN": "",
		"CLAUDE_DISABLE_MOUSE": "1", "CLAUDE_EXPERIMENTAL_TEAMS": "1", "CLAUDE_OPTIONS": "",
		"LAYOUT_2ROW_LEADER_PCT": "50", "LAYOUT_3ROW_LEADER_NUM": "3", "LAYOUT_3ROW_MID_NUM": "2", "LAYOUT_3ROW_DEN": "7",
		"PANE_ACTIVE_BRIGHTEN_STEP": "1", "PANE_INACTIVE_DARKEN_STEP": "1", "RESUME_MODE": "ask", "RESUME_MAX_AGE_H": "24",
		"CTR_STOP_ON_EXIT": "0",
	}
	if strings.Contains(home, `\`) {
		vars["GITHUB_DIR"] = home + `\GITHUB`
	}
	claudeDirSet := false
	arrays := map[string][]string{"GITHUB_DIRS": nil, "EXTRA_ENVS": nil, "COLOR_PALETTE": strings.Fields("17 52 22 53 58 23 54 238 18 59")}
	colors := map[string]string{}
	get := func(n string) string {
		if v, ok := vars[n]; ok {
			return v
		}
		if a, ok := arrays[n]; ok && len(a) > 0 {
			return a[0]
		}
		return os.Getenv(n)
	}
	isName := func(s string) bool {
		if s == "" {
			return false
		}
		for i, r := range s {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(src); {
		var toks []confTok
		toks, i = confTokens(src, i, get)
		for k := 0; k < len(toks); k++ {
			t := toks[k]
			if t.kind != 'w' {
				continue
			}
			if t.s == "export" || t.s == "declare" || t.s == "local" || t.s == "readonly" || strings.HasPrefix(t.s, "-") {
				continue
			}
			name, val, ok := strings.Cut(t.s, "=")
			if !ok || !isName(name) {
				continue
			}
			if val == "" && k+1 < len(toks) && toks[k+1].kind == '(' {
				var items []string
				k += 2
				for ; k < len(toks) && toks[k].kind != ')'; k++ {
					if toks[k].kind == 'w' {
						items = append(items, toks[k].s)
					}
				}
				if name == "PROJECT_COLORS" {
					for _, it := range items {
						if kv, v, ok := strings.Cut(it, "]="); ok && strings.HasPrefix(kv, "[") {
							colors[kv[1:]] = v
						}
					}
					continue
				}
				arrays[name] = items
				continue
			}
			vars[name] = val
			if name == "CLAUDE_CONFIG_DIR" {
				claudeDirSet = true
			}
		}
	}
	var b strings.Builder
	p := func(tag, v string) { fmt.Fprintf(&b, "%s\t%s\n", tag, v) }
	p("layout", strings.Join([]string{vars["LAYOUT_2ROW_LEADER_PCT"], vars["LAYOUT_3ROW_LEADER_NUM"], vars["LAYOUT_3ROW_MID_NUM"], vars["LAYOUT_3ROW_DEN"]}, " "))
	p("steps", vars["PANE_ACTIVE_BRIGHTEN_STEP"]+" "+vars["PANE_INACTIVE_DARKEN_STEP"])
	p("usage", vars["USAGE_REFRESH_SEC"])
	p("resume", vars["RESUME_MODE"]+" "+vars["RESUME_MAX_AGE_H"])
	p("token", vars["GITHUB_TOKEN"])
	p("mouse", vars["CLAUDE_DISABLE_MOUSE"])
	p("ctrstop", vars["CTR_STOP_ON_EXIT"])
	p("teams", vars["CLAUDE_EXPERIMENTAL_TEAMS"])
	p("opts", vars["CLAUDE_OPTIONS"])
	cd := ""
	if claudeDirSet {
		cd = vars["CLAUDE_CONFIG_DIR"] // seule la valeur du fichier compte (cf. LoadConfig)
	}
	p("claudedir", cd)
	p("palette", strings.Join(arrays["COLOR_PALETTE"], " "))
	for _, e := range arrays["EXTRA_ENVS"] {
		p("env", e)
	}
	keys := make([]string, 0, len(colors))
	for k := range colors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "color\t%s\t%s\n", k, colors[k])
	}
	if dirs := arrays["GITHUB_DIRS"]; len(dirs) > 0 {
		for _, d := range dirs {
			p("root", d)
		}
	} else {
		p("root", vars["GITHUB_DIR"])
	}
	return b.String()
}
