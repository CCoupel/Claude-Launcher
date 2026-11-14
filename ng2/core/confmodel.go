package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ── config.conf en réglages typés (#149, LOT-3.3) ─────────────────────────
//
// L'écran « Launcher » lit config.conf en modèle (ReadConfModel) et réécrit UNE
// clé à la fois (ApplyConfConfChanges) : seule la ligne de la clé change,
// commentaires et autres lignes sont conservés. Le fichier est du bash : une
// affectation qui n'est pas d'une forme simple (boucle, eval, « += », valeur
// composée) rend le champ en lecture seule plutôt que de risquer de l'abîmer.
// Les valeurs écrites sont contrôlées : le fichier est « sourcé » par bash, une
// valeur ne doit jamais pouvoir y ajouter une commande.

// confSetting : un réglage connu du launcher.
type confSetting struct {
	Key, Group, Label, Help, Type string
	Default                       any
	Min, Max                      *int
	Options                       []string
}

func ip(n int) *int { return &n }

// confGroups : rubriques de l'écran, dans l'ordre.
var confGroups = []ConfGroup{
	{"projets", "Projets"}, {"claude", "Claude Code"}, {"github", "GitHub"}, {"reprise", "Reprise de session"},
	{"teams", "Teams et panes"}, {"couleurs", "Couleurs"}, {"conteneurs", "Conteneurs"}, {"conso", "Consommation"},
}

var confSettings = []confSetting{
	{Key: "GITHUB_DIR", Group: "projets", Type: "path", Label: "Dossier des projets",
		Help: "Utilisé si « Dossiers des projets » est vide.", Default: "$HOME/GITHUB"},
	{Key: "GITHUB_DIRS", Group: "projets", Type: "list", Label: "Dossiers des projets (plusieurs)",
		Help: "S'il est renseigné, remplace le dossier ci-dessus ; les projets sont groupés par dossier.", Default: []string{}},
	{Key: "CLAUDE_OPTIONS", Group: "claude", Type: "string", Label: "Options de claude",
		Help: "Ajoutées à chaque lancement (ex. --allow-dangerously-skip-permissions).", Default: ""},
	{Key: "CLAUDE_DISABLE_MOUSE", Group: "claude", Type: "bool", Label: "Désactiver la souris de claude",
		Help: "Désactive la souris dans le terminal de claude.", Default: true},
	{Key: "CLAUDE_EXPERIMENTAL_TEAMS", Group: "claude", Type: "bool", Label: "Activer les agent teams", Default: true},
	{Key: "CLAUDE_CONFIG_DIR", Group: "claude", Type: "path", Label: "Dossier de config de claude",
		Help: "Commun à toutes les sessions du launcher (hôte et conteneurs), ex. sur un stockage partagé. Relatif : par rapport au dossier de config.conf. Une variable déjà définie au lancement l'emporte.", Default: ""},
	{Key: "EXTRA_ENVS", Group: "claude", Type: "list", Label: "Variables d'environnement ajoutées",
		Help: "Une par ligne, forme NOM=valeur ; passées à claude et aux teammates.", Default: []string{}},
	{Key: "GITHUB_TOKEN", Group: "github", Type: "secret", Label: "Jeton GitHub",
		Help: "Pour gh et le MCP de claude. Jamais affiché : saisir un nouveau jeton pour le remplacer."},
	{Key: "RESUME_MODE", Group: "reprise", Type: "enum", Label: "Reprise de la dernière conversation",
		Help: "ask : question à l'ouverture ; always : sans question ; never : jamais.", Default: "ask", Options: []string{"ask", "always", "never"}},
	{Key: "RESUME_MAX_AGE_H", Group: "reprise", Type: "int", Label: "Âge maximal d'une reprise (heures)",
		Help: "Proposée seulement si la conversation a moins de ces heures et n'a pas été close par /end-session.", Default: 24, Min: ip(1)},
	{Key: "TEAM_HIDE_IDLE", Group: "teams", Type: "int", Label: "Cacher les teammates inactifs après (s)",
		Help: "Mis de côté (ils continuent de tourner), ramenés dès qu'ils travaillent. 0 = jamais.", Default: 30, Min: ip(0)},
	{Key: "LAYOUT_2ROW_LEADER_PCT", Group: "teams", Type: "int", Label: "Part du leader sur 2 lignes (%)",
		Help: "Jusqu'à 5 teammates.", Default: 50, Min: ip(1), Max: ip(99)},
	{Key: "LAYOUT_3ROW_LEADER_NUM", Group: "teams", Type: "int", Label: "Leader sur 3 lignes : numérateur",
		Help: "Fractions de la hauteur : leader, ligne du milieu, la dernière a le reste.", Default: 3, Min: ip(1)},
	{Key: "LAYOUT_3ROW_MID_NUM", Group: "teams", Type: "int", Label: "Ligne du milieu : numérateur", Default: 2, Min: ip(1)},
	{Key: "LAYOUT_3ROW_DEN", Group: "teams", Type: "int", Label: "Dénominateur", Help: "Doit dépasser la somme des deux numérateurs.", Default: 7, Min: ip(2)},
	{Key: "PANE_ACTIVE_BRIGHTEN_STEP", Group: "couleurs", Type: "int", Label: "Éclaircissement d'un teammate actif",
		Help: "1 = léger, 5 = couleur pleine.", Default: 1, Min: ip(0), Max: ip(5)},
	{Key: "PANE_INACTIVE_DARKEN_STEP", Group: "couleurs", Type: "int", Label: "Assombrissement d'un teammate inactif",
		Help: "1 = léger, 5 = quasi noir.", Default: 1, Min: ip(0), Max: ip(5)},
	{Key: "COLOR_PALETTE", Group: "couleurs", Type: "intlist", Label: "Palette des couleurs de projet",
		Help: "Indices tmux 256 couleurs (0 à 255) attribués aux projets.", Default: []string{"17", "52", "22", "53", "58", "23", "54", "238", "18", "59"}},
	{Key: "CTR_STOP_ON_EXIT", Group: "conteneurs", Type: "bool", Label: "Arrêter le conteneur à la fin de claude",
		Help: "Quand claude se termine proprement de lui-même (la fermeture explicite l'arrête toujours).", Default: false},
	{Key: "USAGE_REFRESH_SEC", Group: "conso", Type: "int", Label: "Consommation : secondes entre deux lectures",
		Help: "Minimum 60.", Default: 120, Min: ip(60)},
}

func confSettingOf(key string) (confSetting, bool) {
	for _, s := range confSettings {
		if s.Key == key {
			return s, true
		}
	}
	return confSetting{}, false
}

// ConfGroup : une rubrique de l'écran.
type ConfGroup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ConfSettingView : un réglage tel que l'interface le reçoit. Le jeton n'a
// jamais de Value.
type ConfSettingView struct {
	Key      string   `json:"key"`
	Group    string   `json:"group"`
	Label    string   `json:"label"`
	Help     string   `json:"help,omitempty"`
	Type     string   `json:"type"`
	Value    any      `json:"value,omitempty"`
	Default  any      `json:"default,omitempty"`
	Set      bool     `json:"set"`
	Editable bool     `json:"editable"`
	Why      string   `json:"why,omitempty"`
	Min      *int     `json:"min,omitempty"`
	Max      *int     `json:"max,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// ConfModel : config.conf lu en modèle.
type ConfModel struct {
	Groups       []ConfGroup       `json:"groups"`
	Settings     []ConfSettingView `json:"settings"`
	Unrecognized []string          `json:"unrecognized"`
}

// ── Lecture : affectations bash simples ───────────────────────────────────

// confStmt : une affectation « NOM=valeur » reconnue.
type confStmt struct {
	Name       string
	Scalar     string   // valeur sans guillemets (scalaire)
	Items      []string // éléments d'un tableau
	Array      bool
	PlusEq     bool // NOM+=…
	Compound   bool // valeur composée ($(…), guillemets accolés…) : non modifiable
	Start      int  // début de l'instruction
	ValStart   int  // début de la valeur
	End        int  // fin de la valeur (= fin de l'instruction)
	ArrayLines bool // tableau écrit sur plusieurs lignes
}

var confNameRe = regexp.MustCompile(`^(?:(?:export|declare|typeset|local|readonly)[ \t]+(?:-[A-Za-z]+[ \t]+)*)?([A-Za-z_][A-Za-z0-9_]*)(\+?)=`)

// scanConf : affectations simples de src, et le texte des autres instructions
// (pour repérer une clé citée ailleurs : boucle, eval…). Les commentaires sont ignorés.
func scanConf(src string) (stmts []confStmt, other string) {
	var oth strings.Builder
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ';':
			i++
			continue
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		loc := confNameRe.FindStringSubmatchIndex(src[i:])
		if loc == nil {
			j := skipConfCommand(src, i)
			oth.WriteString(src[i:j])
			oth.WriteByte('\n')
			i = j
			continue
		}
		st := confStmt{Name: src[i+loc[2] : i+loc[3]], PlusEq: loc[5] > loc[4], Start: i, ValStart: i + loc[1]}
		end := parseConfValue(src, st.ValStart, &st)
		st.End = end
		stmts = append(stmts, st)
		i = end
	}
	return stmts, oth.String()
}

// skipConfCommand : fin d'une instruction qui n'est pas une affectation simple.
func skipConfCommand(src string, i int) int {
	for q := byte(0); i < len(src); i++ {
		c := src[i]
		switch {
		case q != 0:
			if c == '\\' && q == '"' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '\n' || c == ';':
			return i
		}
	}
	return i
}

// parseConfValue : valeur commençant en v ; rend sa fin.
func parseConfValue(src string, v int, st *confStmt) int {
	endsWord := func(j int) bool {
		return j >= len(src) || strings.IndexByte(" \t\r\n;#", src[j]) >= 0
	}
	switch {
	case v < len(src) && src[v] == '(':
		j := v + 1
		for q := byte(0); j < len(src); j++ {
			c := src[j]
			switch {
			case q != 0:
				if c == '\\' && q == '"' {
					j++
				} else if c == q {
					q = 0
				}
			case c == '"' || c == '\'':
				q = c
			case c == '#' && (src[j-1] == ' ' || src[j-1] == '\t' || src[j-1] == '\n' || src[j-1] == '('):
				for j < len(src) && src[j] != '\n' {
					j++
				}
				j--
			case c == ')':
				inner := src[v+1 : j]
				st.Array, st.Items, st.ArrayLines = true, confItems(inner), strings.Contains(inner, "\n")
				for _, it := range st.Items {
					if strings.Contains(it, "$(") || strings.Contains(it, "`") {
						st.Compound = true
					}
				}
				return j + 1
			}
		}
		st.Compound = true // parenthèse jamais fermée
		return len(src)
	case v < len(src) && (src[v] == '"' || src[v] == '\''):
		q := src[v]
		j := v + 1
		for ; j < len(src) && src[j] != q; j++ {
			if q == '"' && src[j] == '\\' {
				j++
			}
		}
		if j >= len(src) {
			st.Compound = true
			return len(src)
		}
		st.Scalar = src[v+1 : j]
		if q == '"' && (strings.Contains(st.Scalar, "$(") || strings.Contains(st.Scalar, "`")) || !endsWord(j+1) {
			st.Compound = true
		}
		return j + 1
	}
	j := v
	for j < len(src) && strings.IndexByte(" \t\r\n;", src[j]) < 0 {
		j++
	}
	st.Scalar = src[v:j]
	if strings.ContainsAny(st.Scalar, "\"'`()<>|&") || strings.Contains(st.Scalar, "$(") {
		st.Compound = true
	}
	return j
}

// confItems : éléments d'un tableau (guillemets ôtés, commentaires ignorés).
func confItems(inner string) []string {
	var out []string
	for i := 0; i < len(inner); {
		c := inner[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(inner) && inner[i] != '\n' {
				i++
			}
		default:
			var b strings.Builder
			for ; i < len(inner) && strings.IndexByte(" \t\r\n", inner[i]) < 0; i++ {
				switch inner[i] {
				case '"', '\'':
					q := inner[i]
					i++
					for ; i < len(inner) && inner[i] != q; i++ {
						if q == '"' && inner[i] == '\\' && i+1 < len(inner) {
							i++
						}
						b.WriteByte(inner[i])
					}
				default:
					b.WriteByte(inner[i])
				}
			}
			out = append(out, b.String())
		}
	}
	return out
}

// ReadConfModel : modèle de config.conf (src vide ou fichier absent : valeurs par défaut).
func ReadConfModel(src string) ConfModel {
	stmts, other := scanConf(src)
	m := ConfModel{Groups: confGroups, Settings: []ConfSettingView{}, Unrecognized: []string{}}
	for _, s := range confSettings {
		v := ConfSettingView{Key: s.Key, Group: s.Group, Label: s.Label, Help: s.Help, Type: s.Type, Default: s.Default,
			Editable: true, Min: s.Min, Max: s.Max, Options: s.Options}
		var last *confStmt
		bad := ""
		for k := range stmts {
			st := &stmts[k]
			if st.Name != s.Key {
				continue
			}
			switch {
			case st.PlusEq:
				bad = "ajouts « += » : forme non modifiable ici"
			case st.Compound:
				bad = "valeur composée (commande ou guillemets accolés) : forme non modifiable ici"
			default:
				last = st
			}
		}
		if bad == "" && regexp.MustCompile(`\b`+s.Key+`\b`).MatchString(other) {
			bad = "clé citée par une autre instruction du fichier (boucle, eval…) : forme non modifiable ici"
		}
		if bad != "" {
			v.Editable, v.Why = false, bad
			m.Unrecognized = append(m.Unrecognized, s.Key)
		}
		if last != nil {
			v.Set = true
			switch s.Type {
			case "list", "intlist":
				items := last.Items
				if !last.Array {
					items = []string{last.Scalar}
					if last.Scalar == "" {
						items = []string{}
					}
				}
				v.Value = items
			case "int":
				if n, err := strconv.Atoi(strings.TrimSpace(last.Scalar)); err == nil {
					v.Value = n
				} else {
					v.Value = last.Scalar
				}
			case "bool":
				v.Value = strings.TrimSpace(last.Scalar) == "1"
			case "secret":
				v.Set = last.Scalar != ""
				v.Value = nil // jamais renvoyé
			default:
				v.Value = last.Scalar
			}
		} else if s.Type != "secret" {
			v.Value = s.Default
		}
		if s.Type == "secret" {
			v.Value, v.Default = nil, nil
		}
		m.Settings = append(m.Settings, v)
	}
	return m
}

// ConfTokenValue : valeur du jeton GitHub écrite dans src ("" : aucun). Pour
// masquer le jeton dans les messages ; jamais renvoyée à l'interface.
func ConfTokenValue(src string) string {
	stmts, _ := scanConf(src)
	tok := ""
	for _, st := range stmts {
		if st.Name == "GITHUB_TOKEN" && !st.Array && !st.PlusEq {
			tok = st.Scalar
		}
	}
	return tok
}

// ── Écriture d'une clé ────────────────────────────────────────────────────

var (
	confSimpleVar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	confTokenRe   = regexp.MustCompile(`^[A-Za-z0-9_.\-=/+]+$`)
	confEnvRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// checkConfString : valeur sûre pour une chaîne entre guillemets doubles d'un
// fichier sourcé par bash : ni guillemet, ni antislash, ni guillemet inverse, ni
// retour à la ligne, ni substitution de commande ; « $NOM » et « ${NOM} » seuls.
// confAllowedVars : variables développables dans une valeur (« $NOM », « ${NOM} ») :
// l'environnement de base et les réglages de chemin ; jamais un secret
// (GITHUB_TOKEN, GH_TOKEN…) qui serait recopié dans une autre valeur.
var confAllowedVars = map[string]bool{
	"HOME": true, "USER": true, "LOGNAME": true, "PWD": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true,
	"XDG_DATA_HOME": true, "XDG_STATE_HOME": true, "GITHUB_DIR": true, "CLAUDE_CONFIG_DIR": true,
}

func confVarName(rest string) string {
	i := 0
	for i < len(rest) && (rest[i] == '_' || rest[i] >= 'A' && rest[i] <= 'Z' || rest[i] >= 'a' && rest[i] <= 'z' || i > 0 && rest[i] >= '0' && rest[i] <= '9') {
		i++
	}
	return rest[:i]
}

func checkConfString(v string) error {
	if strings.ContainsAny(v, "\"\\`\n\r\x00") {
		return fmt.Errorf("guillemet, antislash, guillemet inverse et retour à la ligne interdits")
	}
	for i := 0; i < len(v); i++ {
		if v[i] != '$' {
			continue
		}
		rest := v[i+1:]
		switch {
		case strings.HasPrefix(rest, "{"):
			e := strings.IndexByte(rest, '}')
			if e < 0 || !confSimpleVar.MatchString(rest[1:e]) {
				return fmt.Errorf("« ${…} » : seul un nom de variable est permis")
			}
			if !confAllowedVars[rest[1:e]] {
				return fmt.Errorf("variable « %s » non autorisée dans une valeur (permises : HOME, USER, LOGNAME, PWD, XDG_*, GITHUB_DIR, CLAUDE_CONFIG_DIR)", rest[1:e])
			}
		case confVarName(rest) != "":
			if n := confVarName(rest); !confAllowedVars[n] {
				return fmt.Errorf("variable « %s » non autorisée dans une valeur (permises : HOME, USER, LOGNAME, PWD, XDG_*, GITHUB_DIR, CLAUDE_CONFIG_DIR)", n)
			}
		default:
			return fmt.Errorf("« $ » : seul « $NOM » ou « ${NOM} » est permis (pas de substitution de commande)")
		}
	}
	return nil
}

func confInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), x == float64(int(x))
	case int:
		return x, true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil
	}
	return 0, false
}

func confStrings(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			switch s := e.(type) {
			case string:
				out[i] = s
			case float64:
				out[i] = strconv.Itoa(int(s))
			default:
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

// renderConfValue : texte à écrire après « = » (valeur contrôlée par type).
func renderConfValue(s confSetting, v any, multiline bool, eol string) (string, error) {
	fail := func(f string, a ...any) (string, error) {
		return "", fmt.Errorf("%s : %s", s.Key, fmt.Sprintf(f, a...))
	}
	switch s.Type {
	case "int":
		n, ok := confInt(v)
		if !ok {
			return fail("un nombre entier est attendu")
		}
		if s.Min != nil && n < *s.Min || s.Max != nil && n > *s.Max {
			lo, hi := "", ""
			if s.Min != nil {
				lo = strconv.Itoa(*s.Min)
			}
			if s.Max != nil {
				hi = strconv.Itoa(*s.Max)
			}
			return fail("valeur hors bornes (%s … %s)", lo, hi)
		}
		return strconv.Itoa(n), nil
	case "bool":
		switch x := v.(type) {
		case bool:
			if x {
				return "1", nil
			}
			return "0", nil
		}
		if n, ok := confInt(v); ok && (n == 0 || n == 1) {
			return strconv.Itoa(n), nil
		}
		return fail("vrai ou faux attendu")
	case "enum":
		str, _ := v.(string)
		for _, o := range s.Options {
			if o == str {
				return str, nil
			}
		}
		return fail("« %v » : l'une de %s est attendue", v, strings.Join(s.Options, ", "))
	case "string", "path":
		str, ok := v.(string)
		if !ok {
			return fail("texte attendu")
		}
		if err := checkConfString(str); err != nil {
			return fail("%v", err)
		}
		return `"` + str + `"`, nil
	case "secret":
		str, ok := v.(string)
		if !ok || str == "" || !confTokenRe.MatchString(str) {
			return fail("jeton invalide (lettres, chiffres et _ . - = / + seulement)")
		}
		return `"` + str + `"`, nil
	case "list", "intlist":
		items, ok := confStrings(v)
		if !ok {
			return fail("liste attendue")
		}
		for i, it := range items {
			if s.Type == "intlist" {
				if n, err := strconv.Atoi(it); err != nil || n < 0 || n > 255 || strconv.Itoa(n) != it {
					return fail("« %s » : un indice de 0 à 255 est attendu", it)
				}
				continue
			}
			if strings.TrimSpace(it) == "" {
				return fail("élément %d vide", i+1)
			}
			if err := checkConfString(it); err != nil {
				return fail("« %s » : %v", it, err)
			}
			if s.Key == "EXTRA_ENVS" {
				if !confEnvRe.MatchString(it) {
					return fail("« %s » : la forme NOM=valeur est attendue", it)
				}
				if name, _, _ := strings.Cut(it, "="); confDangerousEnv(name) {
					return fail("variable « %s » interdite : elle permet d'exécuter du code au lancement de claude", name)
				}
			}
		}
		q := make([]string, len(items))
		for i, it := range items {
			q[i] = `"` + it + `"`
			if s.Type == "intlist" {
				q[i] = it
			}
		}
		if multiline || len(items) > 4 {
			return "(" + eol + "  " + strings.Join(q, eol+"  ") + eol + ")", nil
		}
		return "(" + strings.Join(q, " ") + ")", nil
	}
	return fail("type %s inconnu", s.Type)
}

// ApplyConfConfChanges : changements de config.conf (set / delete d'une clé,
// at = [CLÉ]). Chaque valeur est contrôlée avant toute écriture ; seule la ligne
// de la clé change. Le texte rendu n'est pas validé ici (ValidateConfig puis
// SaveConfig s'en chargent).
func ApplyConfConfChanges(src string, changes []ConfigChange) (string, error) {
	eol := "\n"
	if strings.Contains(src, "\r\n") {
		eol = "\r\n" // fichier écrit sous Windows : ses fins de ligne sont conservées
	}
	for i, ch := range changes {
		if len(ch.At) != 1 {
			return "", fmt.Errorf("changement %d : « at » doit être [CLÉ]", i+1)
		}
		s, ok := confSettingOf(ch.At[0])
		if !ok {
			return "", fmt.Errorf("changement %d : réglage « %s » inconnu", i+1, ch.At[0])
		}
		stmts, other := scanConf(src)
		var mine []confStmt
		for _, st := range stmts {
			if st.Name == s.Key {
				if st.PlusEq || st.Compound {
					return "", fmt.Errorf("%s : forme d'affectation non modifiable ici — « Éditer en YAML »", s.Key)
				}
				mine = append(mine, st)
			}
		}
		if regexp.MustCompile(`\b` + s.Key + `\b`).MatchString(other) {
			return "", fmt.Errorf("%s : clé citée par une autre instruction du fichier — « Éditer en YAML »", s.Key)
		}
		switch ch.Op {
		case "delete":
			for k := len(mine) - 1; k >= 0; k-- {
				src = confRemoveStmt(src, mine[k])
			}
		case "set":
			multi := len(mine) > 0 && mine[len(mine)-1].ArrayLines
			txt, err := renderConfValue(s, ch.Value, multi, eol)
			if err != nil {
				return "", fmt.Errorf("changement %d : %w", i+1, err)
			}
			if n := len(mine); n > 0 {
				last := mine[n-1]
				src = src[:last.ValStart] + txt + src[last.End:]
			} else {
				src = confInsertStmt(src, s.Key, txt, eol)
			}
		default:
			return "", fmt.Errorf("changement %d : opération %q inconnue (set, delete)", i+1, ch.Op)
		}
	}
	// cohérence de la mise en page des teams (comme au chargement)
	m := ReadConfModel(src)
	get := func(k string) (int, bool) {
		for _, s := range m.Settings {
			if s.Key == k {
				n, ok := s.Value.(int)
				return n, ok
			}
		}
		return 0, false
	}
	l, ok1 := get("LAYOUT_3ROW_LEADER_NUM")
	mid, ok2 := get("LAYOUT_3ROW_MID_NUM")
	den, ok3 := get("LAYOUT_3ROW_DEN")
	if ok1 && ok2 && ok3 && l+mid >= den {
		return "", fmt.Errorf("mise en page des teams : le dénominateur (%d) doit dépasser la somme des numérateurs (%d + %d)", den, l, mid)
	}
	return src, nil
}

// confRemoveStmt : retire l'affectation (et sa ligne si elle y est seule).
func confRemoveStmt(src string, st confStmt) string {
	ls := strings.LastIndex(src[:st.Start], "\n") + 1
	le := strings.IndexByte(src[st.End:], '\n')
	if le < 0 {
		le = len(src) - st.End
	}
	if strings.TrimSpace(src[ls:st.Start]) == "" && strings.TrimSpace(src[st.End:st.End+le]) == "" {
		end := st.End + le
		if end < len(src) {
			end++
		}
		return src[:ls] + src[end:]
	}
	return src[:st.Start] + strings.TrimLeft(src[st.End:], " \t") // « A=1; B=2 » : seule A disparaît
}

// confInsertStmt : ajoute « CLÉ=valeur » sous le dernier exemple commenté de la
// clé, sinon en fin de fichier.
func confInsertStmt(src, key, txt, eol string) string {
	line := key + "=" + txt + eol
	re := regexp.MustCompile(`(?m)^[ \t]*#.*\b` + key + `=.*$`)
	if locs := re.FindAllStringIndex(src, -1); len(locs) > 0 {
		end := locs[len(locs)-1][1]
		if end < len(src) {
			end++ // après le retour à la ligne
			return src[:end] + line + src[end:]
		}
		return src + eol + line
	}
	if src != "" && !strings.HasSuffix(src, "\n") {
		src += eol
	}
	return src + line
}

// confDangerousEnv : variables d'environnement qui changent ce qui s'exécute
// (chargement de bibliothèques, shell, PATH, options de node ou de git…) : refusées
// dans EXTRA_ENVS, passé à claude et aux teammates.
func confDangerousEnv(name string) bool {
	n := strings.ToUpper(name)
	switch n {
	case "PATH", "HOME", "SHELL", "BASH_ENV", "ENV", "IFS", "PS4", "PROMPT_COMMAND", "SHELLOPTS", "BASHOPTS",
		"EDITOR", "VISUAL", "BROWSER", "GIT_EXEC_PATH", "GIT_EXTERNAL_DIFF", "NODE_OPTIONS", "NODE_PATH", "NODE_EXTRA_CA_CERTS",
		"PYTHONSTARTUP", "PYTHONPATH", "PYTHONHOME", "PERL5OPT", "PERL5LIB", "RUBYOPT", "RUBYLIB",
		"JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "LUA_INIT", "CURL_HOME", "OPENSSL_CONF":
		return true
	}
	for _, p := range []string{"LD_", "DYLD_", "GIT_SSH", "GIT_CONFIG", "GIT_PROXY", "BASH_FUNC_", "ANTHROPIC_"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	for _, sfx := range []string{"_ASKPASS", "_PROXY", "_PROXY_COMMAND", "PAGER", "_EDITOR"} {
		if strings.HasSuffix(n, sfx) {
			return true
		}
	}
	return false
}

// ── Jeton GitHub dans l'éditeur de texte ──────────────────────────────────

// ConfTokenKeep : valeur affichée à la place du jeton dans l'éditeur de texte ;
// la laisser telle quelle à l'enregistrement conserve le jeton actuel.
const ConfTokenKeep = "__GARDER__"

// MaskConfToken : src avec le jeton GitHub remplacé par ConfTokenKeep partout où
// il figure — la valeur de GITHUB_TOKEN, mais aussi un commentaire, un tableau ou
// une autre clé qui le recopie (jeton d'au moins 8 caractères : une chaîne plus
// courte masquerait n'importe quel texte). Le jeton ne quitte jamais le poste.
func MaskConfToken(src string) string {
	stmts, _ := scanConf(src)
	tok := ""
	for i := len(stmts) - 1; i >= 0; i-- {
		st := stmts[i]
		if st.Name == "GITHUB_TOKEN" && !st.Array && !st.PlusEq && !st.Compound && st.Scalar != "" && st.Scalar != ConfTokenKeep {
			if tok == "" {
				tok = st.Scalar
			}
			src = src[:st.ValStart] + `"` + ConfTokenKeep + `"` + src[st.End:]
		}
	}
	if len(tok) >= 8 {
		src = strings.ReplaceAll(src, tok, ConfTokenKeep)
	}
	return src
}

// RestoreConfToken : le texte rendu par l'éditeur (edited) où GITHUB_TOKEN vaut
// encore ConfTokenKeep reprend le jeton écrit dans current ; sans jeton actuel,
// la valeur devient vide (le marqueur n'est jamais enregistré comme jeton). Un
// jeton remplacé, ou vidé, par l'utilisateur est laissé tel quel.
//
// Le jeton n'est JAMAIS réécrit ailleurs que dans GITHUB_TOKEN : le marqueur
// laissé dans un commentaire y reste (le commentaire perd l'ancien jeton) ; dans
// toute autre instruction (autre clé, tableau, commande), l'enregistrement est
// refusé. Un jeton actuel dont la forme sortirait des guillemets doubles (écrit à
// la main entre apostrophes avec « $ », « ` » ou « " ») est refusé aussi.
func RestoreConfToken(edited, current string) (string, error) {
	cur := ConfTokenValue(current)
	stmts, other := scanConf(edited)
	restore := false
	for _, st := range stmts {
		if st.Name == "GITHUB_TOKEN" && !st.Array && !st.PlusEq && st.Scalar == ConfTokenKeep {
			restore = true
			continue
		}
		hit := strings.Contains(st.Scalar, ConfTokenKeep)
		for _, it := range st.Items {
			hit = hit || strings.Contains(it, ConfTokenKeep)
		}
		if hit {
			return "", fmt.Errorf("le marqueur %s ne convient que pour GITHUB_TOKEN (ligne %d) : saisir la valeur voulue", ConfTokenKeep, 1+strings.Count(edited[:st.Start], "\n"))
		}
	}
	if strings.Contains(other, ConfTokenKeep) {
		return "", fmt.Errorf("le marqueur %s ne convient que pour GITHUB_TOKEN : il figure dans une autre instruction", ConfTokenKeep)
	}
	if !restore {
		return edited, nil
	}
	if cur != "" && !confTokenRe.MatchString(cur) {
		return "", fmt.Errorf("le jeton actuel a une forme que l'éditeur ne sait pas conserver : le remplacer par un nouveau jeton, ou éditer le fichier à la main")
	}
	for i := len(stmts) - 1; i >= 0; i-- {
		st := stmts[i]
		if st.Name == "GITHUB_TOKEN" && !st.Array && !st.PlusEq && st.Scalar == ConfTokenKeep {
			edited = edited[:st.ValStart] + `"` + cur + `"` + edited[st.End:]
		}
	}
	return edited, nil
}

// MaskSecret : msg sans la valeur du jeton. Un jeton court (moins de 8
// caractères : tout autre texte le contiendrait) ne se masque pas par
// remplacement : le message est alors remplacé en entier par un texte générique.
func MaskSecret(msg, token string) string {
	switch {
	case token == "" || !strings.Contains(msg, token):
		return msg
	case len(token) < 8:
		return "problème dans config.conf (détail masqué)"
	}
	return strings.ReplaceAll(msg, token, "***")
}
