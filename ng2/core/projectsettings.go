package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Réglages d'un projet (#148, LOT-3.2) ──────────────────────────────────
//
// Ce que la fenêtre Réglages montre et écrit pour UN projet : backend attribué,
// image, couleur, env.yaml du projet (modèle + écriture par yaml.v3 Node, comme
// backends.yaml) et l'environnement fusionné avec la provenance de chaque
// élément. Mêmes validations que le chargement réel (un seul code).

// EnvModel : env.yaml du projet en modèle (clés absentes : listes vides).
type EnvModel struct {
	Image             string              `json:"image"`
	Packages          []string            `json:"packages"`
	PackagesByManager map[string][]string `json:"packages_by_manager"`
	Setup             []string            `json:"setup"`
	Watch             []string            `json:"watch"`
	OnStart           []string            `json:"on_start"`
	Local             []string            `json:"local"`
	Mounts            []MountModel        `json:"mounts"`
	Prefer            string              `json:"prefer"`
}

// MountModel : un montage en plus ; RO absent = lecture seule par défaut.
type MountModel struct {
	Path string `json:"path"`
	RO   *bool  `json:"ro,omitempty"`
}

// ReadEnvModel : modèle d'un env.yaml (contenu vide : modèle vide).
func ReadEnvModel(content []byte) (EnvModel, error) {
	m := EnvModel{Packages: []string{}, PackagesByManager: map[string][]string{}, Setup: []string{}, Watch: []string{},
		OnStart: []string{}, Local: []string{}, Mounts: []MountModel{}}
	var e EnvSpec
	if err := yaml.Unmarshal(content, &e); err != nil {
		return m, fmt.Errorf("env.yaml : %w", err)
	}
	m.Image, m.Prefer = e.Image, e.Prefer
	for _, p := range []struct {
		dst *[]string
		src []string
	}{{&m.Packages, e.Packages}, {&m.Setup, e.Setup}, {&m.Watch, e.Watch}, {&m.OnStart, e.OnStart}, {&m.Local, e.Local}} {
		if p.src != nil {
			*p.dst = p.src
		}
	}
	for k, v := range e.PackagesByManager {
		m.PackagesByManager[k] = v
	}
	for _, mt := range e.Mounts {
		m.Mounts = append(m.Mounts, MountModel{Path: mt.Path, RO: mt.RO})
	}
	return m, nil
}

// MergedLine : une ligne de l'aperçu fusionné ; From : template, launcher ou
// project pour un élément, vide pour une ligne d'intitulé.
type MergedLine struct {
	Text string `json:"text"`
	From string `json:"from"`
}

// MergedEnvLines : environnement fusionné du projet — env-template.yaml, env.yaml
// du launcher (étapes et paquets ajoutés à tous les conteneurs) et env.yaml du
// projet (projectContent : son texte, éventuellement modifié) — ligne par
// ligne avec la provenance de chaque élément. Même fusion que le démarrage d'un
// conteneur (withProject puis withLauncher) : listes réunies sans doublon (le
// premier qui porte l'élément le revendique), étapes du launcher, du template
// puis du projet, image et prefer du projet prioritaires, montages réunis par
// chemin.
func MergedEnvLines(proj string, projectContent []byte) ([]MergedLine, error) {
	var p, t, l EnvSpec
	if len(strings.TrimSpace(string(projectContent))) > 0 {
		var err error
		if p, err = parseEnvFile(projectContent, envFile); err != nil {
			return nil, err
		}
	}
	if tb, err := os.ReadFile(envTemplatePath(proj)); err == nil {
		var err error
		if t, err = parseEnvFile(tb, envTemplateFile); err != nil {
			return nil, err
		}
	}
	l, err := loadLauncherEnv()
	if err != nil {
		return nil, err
	}
	var out []MergedLine
	add := func(text, from string) { out = append(out, MergedLine{text, from}) }

	switch {
	case p.Image != "":
		add("image: "+p.Image, "project")
	case t.Image != "":
		add("image: "+t.Image, "template")
	}
	if p.Prefer != "" {
		add("prefer: "+p.Prefer, "project")
	}
	type item struct{ text, from string }
	union3 := func(srcs ...[]item) []item {
		seen := map[string]bool{}
		var all []item
		for _, s := range srcs {
			for _, it := range s {
				if !seen[it.text] {
					seen[it.text] = true
					all = append(all, it)
				}
			}
		}
		return all
	}
	tag := func(from string, l []string) []item {
		var r []item
		for _, s := range l {
			r = append(r, item{s, from})
		}
		return r
	}
	list := func(name string, items []item) {
		if len(items) == 0 {
			return
		}
		add(name+":", "")
		for _, it := range items {
			add("  - "+it.text, it.from)
		}
	}
	list("packages", union3(tag("launcher", l.Packages), tag("template", t.Packages), tag("project", p.Packages)))
	mgrs := map[string]bool{}
	for _, e := range []EnvSpec{l, t, p} {
		for k := range e.PackagesByManager {
			mgrs[k] = true
		}
	}
	names := make([]string, 0, len(mgrs))
	for k := range mgrs {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) > 0 {
		add("packages_by_manager:", "")
		for _, k := range names {
			if items := union3(tag("launcher", l.PackagesByManager[k]), tag("template", t.PackagesByManager[k]), tag("project", p.PackagesByManager[k])); len(items) > 0 {
				add("  "+k+":", "")
				for _, it := range items {
					add("    - "+it.text, it.from)
				}
			}
		}
	}
	// étapes : jamais dédoublonnées, dans l'ordre d'exécution
	steps := func(name string, ls, ts, ps []string) {
		all := append(append(tag("launcher", ls), tag("template", ts)...), tag("project", ps)...)
		list(name, all)
	}
	steps("setup", l.Setup, t.Setup, p.Setup)
	steps("on_start", l.OnStart, t.OnStart, p.OnStart)
	list("watch", union3(tag("template", t.Watch), tag("project", p.Watch)))
	list("local", union3(tag("template", t.Local), tag("project", p.Local)))
	var mounts []item
	idx := map[string]int{}
	for _, src := range []struct {
		from string
		ms   []EnvMount
	}{{"template", t.Mounts}, {"project", p.Mounts}} {
		for _, m := range src.ms {
			txt := "{ path: " + m.Path
			if m.RO != nil {
				txt += ", ro: " + strconv.FormatBool(*m.RO)
			}
			txt += " }"
			if i, ok := idx[m.Path]; ok {
				if m.RO != nil {
					mounts[i] = item{txt, src.from} // le projet l'emporte
				}
				continue
			}
			idx[m.Path] = len(mounts)
			mounts = append(mounts, item{txt, src.from})
		}
	}
	list("mounts", mounts)
	return out, nil
}

// ProjectSettings : ce que projectSettings rend (hors « open », connu de
// l'interface).
type ProjectSettings struct {
	Assigned     bool            `json:"assigned"`
	Backend      string          `json:"backend"`
	Backends     []string        `json:"backends"`
	Image        string          `json:"image"`
	Images       []string        `json:"images"`
	Color        string          `json:"color"`
	ColorAuto    bool            `json:"colorAuto"`
	Palette      []string        `json:"palette"`
	Env          EnvModel        `json:"env"`
	EnvPath      string          `json:"envPath"`
	EnvMtime     string          `json:"envMtime"`
	EnvExists    bool            `json:"envExists"`
	TemplatePath string          `json:"templatePath"`
	HasTemplate  bool            `json:"hasTemplate"`
	Merged       []MergedLine    `json:"merged"`
	Problems     []ConfigProblem `json:"problems"`
}

// ReadProjectSettings : réglages du projet proj. changes (facultatif) : aperçu
// d'un env.yaml modifié, calculé sans rien écrire (modèle, aperçu fusionné et
// problèmes sont ceux du texte modifié).
func ReadProjectSettings(proj string, cfg Config, changes []ConfigChange) ProjectSettings {
	s := ProjectSettings{Problems: []ConfigProblem{}, Merged: []MergedLine{}, EnvPath: EnvPath(proj), TemplatePath: envTemplatePath(proj)}
	name := ReadAssignments()
	s.Backend, s.Assigned = name[proj]
	s.Backends = []string{}
	if _, refused := LocalRefusal(BackendConf{Name: "host", Type: "host"}); !refused {
		s.Backends = append(s.Backends, "host")
	}
	for _, b := range CtrBackends() {
		s.Backends = append(s.Backends, b.Name)
	}
	if e, err := LoadEnv(proj); err == nil {
		s.Image = e.Image
	}
	s.Images = append([]string(nil), BaseImages()...)
	if s.Image != "" {
		found := false
		for _, im := range s.Images {
			found = found || im == s.Image
		}
		if !found {
			s.Images = append([]string{s.Image}, s.Images...)
		}
	}
	base := filepath.Base(proj)
	_, explicit := cfg.ProjectColors[base]
	s.Color, s.ColorAuto = cfg.ProjectColor(base), !explicit
	s.Palette = cfg.Palette
	if s.Palette == nil {
		s.Palette = []string{}
	}
	_, terr := os.Stat(envTemplatePath(proj))
	s.HasTemplate = terr == nil
	var content []byte
	if b, err := os.ReadFile(EnvPath(proj)); err == nil {
		content = b
		s.EnvExists = true
		if st, err := os.Stat(EnvPath(proj)); err == nil {
			s.EnvMtime = st.ModTime().Format(time.RFC3339Nano)
		}
	}
	if len(changes) > 0 {
		if out, err := ApplyConfigChanges(ConfProjectEnv, content, changes); err != nil {
			s.Problems = append(s.Problems, ConfigProblem{Severity: SevError, Message: err.Error()})
		} else {
			content = out
		}
	}
	var err error
	if s.Env, err = ReadEnvModel(content); err != nil {
		s.Problems = append(s.Problems, ConfigProblem{Severity: SevError, Message: err.Error()})
	}
	if m, err := MergedEnvLines(proj, content); err == nil {
		s.Merged = m
	} else {
		s.Problems = append(s.Problems, ConfigProblem{Severity: SevError, Message: err.Error()})
	}
	if len(strings.TrimSpace(string(content))) > 0 || s.EnvExists {
		s.Problems = append(s.Problems, ValidateProjectEnv(proj, content)...)
	}
	return s
}

// ── Couleur du projet : PROJECT_COLORS de config.conf ─────────────────────

// Formes bash reconnues d'affectation de PROJECT_COLORS :
//   - un littéral de tableau, seul ou précédé de declare/typeset/local/export
//     (avec ses options, -A, -gA…) : « PROJECT_COLORS=(["a"]="1" …) », « += » compris ;
//   - une déclaration sans valeur (« declare -A PROJECT_COLORS ») ;
//   - des affectations d'éléments (« PROJECT_COLORS["a"]=1 », « PROJECT_COLORS[a]=1 »).
//
// Toute autre ligne qui cite PROJECT_COLORS (boucle, eval, unset…) n'est pas
// reconnue : SetProjectColor refuse plutôt que d'écrire à côté.
var (
	colorsLiteral = regexp.MustCompile(`(?m)^[ \t]*(?:(?:declare|typeset|local|export|readonly)[ \t]+(?:-[A-Za-z]+[ \t]+)*)?PROJECT_COLORS\+?=\(`)
	colorsDeclare = regexp.MustCompile(`^[ \t]*(?:declare|typeset|local)[ \t]+(?:-[A-Za-z]+[ \t]+)+PROJECT_COLORS[ \t]*(?:#.*)?$`)
	colorsElement = regexp.MustCompile(`^[ \t]*PROJECT_COLORS\[[^\]]+\]\+?=`)
)

// errColorsForm : forme d'affectation non reconnue.
var errColorsForm = fmt.Errorf("PROJECT_COLORS est écrit dans config.conf d'une façon que l'interface ne sait pas modifier (boucle, eval, unset…) : modifier le fichier à la main")

// SetProjectColor : config.conf (src) avec la couleur de name dans PROJECT_COLORS ;
// color vide : l'entrée est retirée (couleur automatique). Seule cette clé est
// touchée : le reste du fichier, commentaires compris, et les couleurs des autres
// projets sont conservés, quelle que soit la forme bash (littéral de tableau,
// affectations d'éléments, déclaration seule). Forme non reconnue : erreur,
// jamais une seconde déclaration qui écraserait le tableau. Aucune mention de
// PROJECT_COLORS : une déclaration est ajoutée en fin de fichier.
func SetProjectColor(src, name, color string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\"'\\$`\n\r") {
		return "", fmt.Errorf("nom de projet « %s » non utilisable comme clé de PROJECT_COLORS", name)
	}
	if color != "" {
		if n, err := strconv.Atoi(color); err != nil || n < 0 || n > 255 || strconv.Itoa(n) != color {
			return "", fmt.Errorf("couleur « %s » : un indice de 0 à 255 est attendu", color)
		}
	}
	entry := `["` + name + `"]="` + color + `"`
	nameRe := `\[[ \t]*["']?` + regexp.QuoteMeta(name) + `["']?[ \t]*\]`
	valRe := `(?:"[^"]*"|'[^']*'|[^\s)#]*)`

	// 1. littéraux de tableau : étendues [open, end[ (entre « ( » et « ) »)
	type span struct{ start, open, end int }
	var blocks []span
	for _, loc := range colorsLiteral.FindAllStringIndex(src, -1) {
		end := loc[1]
		closed := false
		for inQ := byte(0); end < len(src) && !closed; end++ {
			c := src[end]
			switch {
			case inQ != 0:
				if c == inQ {
					inQ = 0
				}
			case c == '"' || c == '\'':
				inQ = c
			case c == '#':
				for end < len(src) && src[end] != '\n' {
					end++
				}
				end--
			case c == ')':
				closed = true
				end--
			}
		}
		if !closed {
			return "", fmt.Errorf("PROJECT_COLORS : parenthèse fermante introuvable")
		}
		blocks = append(blocks, span{loc[0], loc[1], end})
	}
	inBlock := func(pos int) bool {
		for _, b := range blocks {
			if pos >= b.start && pos <= b.end {
				return true
			}
		}
		return false
	}
	// 2. lignes hors littéral : déclaration seule, éléments, ou forme inconnue
	var declEnd []int // fin des lignes de déclaration / d'élément (pour l'insertion)
	type elemLine struct{ start, end int }
	var elems []elemLine
	for pos := 0; pos < len(src); {
		le := strings.IndexByte(src[pos:], '\n')
		next := len(src)
		if le >= 0 {
			next = pos + le + 1
		} else {
			le = len(src) - pos
		}
		line := src[pos : pos+le]
		code := strings.TrimLeft(line, " \t")
		if code != "" && !strings.HasPrefix(code, "#") && strings.Contains(code, "PROJECT_COLORS") && !inBlock(pos+len(line)-len(code)) {
			switch {
			case colorsDeclare.MatchString(line):
				declEnd = append(declEnd, next)
			case colorsElement.MatchString(line):
				declEnd = append(declEnd, next)
				elems = append(elems, elemLine{pos, next})
			default:
				return "", errColorsForm
			}
		}
		pos = next
	}
	// 3. l'entrée existe-t-elle ? toutes ses occurrences sont mises à jour (la dernière gagne en bash)
	found := false
	var edits []struct {
		from, to int
		with     string
	}
	for _, b := range blocks {
		blk := src[b.open:b.end]
		re := regexp.MustCompile(nameRe + `[ \t]*=[ \t]*` + valRe)
		for _, m := range re.FindAllStringIndex(blk, -1) {
			found = true
			from, to, with := b.open+m[0], b.open+m[1], entry
			if color == "" { // retrait : la ligne entière si l'entrée y est seule
				with = ""
				ls := strings.LastIndex(src[:from], "\n") + 1
				le := strings.IndexByte(src[to:], '\n')
				if le >= 0 && strings.TrimSpace(src[ls:from]) == "" && strings.TrimSpace(src[to:to+le]) == "" {
					from, to = ls, to+le+1
				}
			}
			edits = append(edits, struct {
				from, to int
				with     string
			}{from, to, with})
		}
	}
	for _, e := range elems {
		line := src[e.start:e.end]
		re := regexp.MustCompile(`^([ \t]*PROJECT_COLORS)` + nameRe + `(\+?=[ \t]*)` + valRe)
		if m := re.FindStringSubmatchIndex(line); m != nil {
			found = true
			if color == "" {
				edits = append(edits, struct {
					from, to int
					with     string
				}{e.start, e.end, ""})
			} else {
				edits = append(edits, struct {
					from, to int
					with     string
				}{e.start + m[3], e.start + m[1], `["` + name + `"]` + line[m[4]:m[5]] + `"` + color + `"`})
			}
		}
	}
	apply := func() string {
		sort.Slice(edits, func(i, j int) bool { return edits[i].from > edits[j].from })
		out := src
		for _, e := range edits {
			out = out[:e.from] + e.with + out[e.to:]
		}
		return out
	}
	if found {
		return apply(), nil
	}
	if color == "" {
		return src, nil
	}
	// 4. entrée absente : dans le dernier littéral, sinon après la dernière
	//    déclaration / affectation d'élément, sinon nouvelle déclaration
	if n := len(blocks); n > 0 {
		b := blocks[n-1]
		blk := src[b.open:b.end]
		switch {
		case strings.Contains(blk, "\n"):
			ls := strings.LastIndex(blk, "\n") + 1
			if strings.TrimSpace(blk[ls:]) == "" {
				return src[:b.open+ls] + "  " + entry + "\n" + src[b.open+ls:], nil
			}
			return src[:b.end] + "\n  " + entry + "\n" + src[b.end:], nil
		case strings.TrimSpace(blk) == "":
			return src[:b.open] + blk + entry + src[b.end:], nil
		}
		return src[:b.end] + " " + entry + src[b.end:], nil
	}
	if n := len(declEnd); n > 0 {
		at := declEnd[0]
		for _, d := range declEnd {
			at = max(at, d)
		}
		line := `PROJECT_COLORS["` + name + `"]="` + color + `"`
		if at > 0 && src[at-1] != '\n' {
			return src + "\n" + line + "\n", nil
		}
		return src[:at] + line + "\n" + src[at:], nil
	}
	if src != "" && !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	return src + "declare -A PROJECT_COLORS=(\n  " + entry + "\n)\n", nil
}

// ApplyProjectColor : fixe (ou, color vide, retire) la couleur du projet name
// dans config.conf, par SaveConfig (validation, conflit, copie .bak).
func ApplyProjectColor(name, color string) ([]ConfigProblem, error) {
	path := ConfigPath()
	src, expected := defaultConfig, time.Time{}
	if b, err := os.ReadFile(path); err == nil {
		src = string(b)
		if st, err := os.Stat(path); err == nil {
			expected = st.ModTime()
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	out, err := SetProjectColor(src, name, color)
	if err != nil {
		return nil, err
	}
	if out == src && !expected.IsZero() {
		return nil, nil
	}
	return SaveConfig(ConfConf, path, []byte(out), expected)
}

// ── Application des réglages d'un projet ──────────────────────────────────

// ProjectChange : ce que projectApply demande ; chaque partie est facultative.
type ProjectChange struct {
	Backend *string // « host » : sur l'hôte
	Image   *string
	Color   *string // vide : couleur automatique
	Env     *EnvChange
}

// EnvChange : changements de l'env.yaml du projet, avec la date d'ouverture.
type EnvChange struct {
	MTime   string
	Changes []ConfigChange
}

// EnvResult : issue de l'écriture de l'env.yaml (comme configSave).
type EnvResult struct {
	Saved    bool            `json:"saved,omitempty"`
	Invalid  bool            `json:"invalid,omitempty"`
	Conflict bool            `json:"conflict,omitempty"`
	Problems []ConfigProblem `json:"problems,omitempty"`
	MTime    string          `json:"mtime,omitempty"`
}

// ProjectResult : résultat de ApplyProjectSettings.
type ProjectResult struct {
	OK    bool       `json:"ok"`
	Msg   string     `json:"msg,omitempty"`
	Note  string     `json:"note,omitempty"`
	Env   *EnvResult `json:"env,omitempty"`        // présent aussi sur un échec POSTÉRIEUR à son écriture (résultat partiel)
	Color bool       `json:"colorSaved,omitempty"` // config.conf écrite (relue à chaud côté interface)
}

// ApplyProjectSettings applique les parties demandées. open : le projet est
// ouvert (backend et image refusés, comme F2). Ordre : contrôles sans écriture,
// puis env.yaml (un échec arrête tout), puis couleur, puis backend et image.
func ApplyProjectSettings(proj string, open bool, in ProjectChange) ProjectResult {
	fail := func(f string, a ...any) ProjectResult { return ProjectResult{Msg: fmt.Sprintf(f, a...)} }
	// 1. contrôles
	backend, image := "", ""
	if in.Backend != nil || in.Image != nil {
		if open {
			return fail("projet ouvert — le fermer avant de changer de backend")
		}
		if in.Backend == nil {
			cur, ok := ReadAssignments()[proj]
			if !ok || cur == "host" {
				return fail("backend à choisir avec l'image")
			}
			backend = cur
		} else {
			backend = *in.Backend
		}
		if backend != "host" {
			if _, ok := FindBackend(backend); !ok {
				return fail("backend « %s » inconnu (backends.yaml)", backend)
			}
			if b, _ := FindBackend(backend); b.Type == "host" {
				backend = "host"
			} else if _, ok := EngineOf(b); !ok {
				return fail("backend « %s » sans conteneurs", backend)
			}
		}
		if backend != "host" {
			if in.Image != nil {
				image = *in.Image
			} else if e, err := LoadEnv(proj); err == nil {
				image = e.Image
			}
			if strings.TrimSpace(image) == "" {
				return fail("image de base à choisir pour un backend conteneur")
			}
		}
	}
	if in.Color != nil {
		if _, err := SetProjectColor("", filepath.Base(proj), *in.Color); err != nil {
			return fail("%v", err)
		}
	}
	res := ProjectResult{OK: true}
	// 2. env.yaml du projet — l'image demandée y entre par les mêmes changements :
	//    une seule écriture du fichier
	if in.Env != nil && len(in.Env.Changes) > 0 {
		if backend != "" && backend != "host" && image != "" {
			in.Env.Changes = append(append([]ConfigChange(nil), in.Env.Changes...), ConfigChange{Op: "set", At: []string{"image"}, Value: image})
		}
		var expected time.Time
		if in.Env.MTime != "" {
			t, err := time.Parse(time.RFC3339Nano, in.Env.MTime)
			if err != nil {
				return fail("date d'ouverture invalide : %v", err)
			}
			expected = t
		}
		var content []byte
		if b, err := os.ReadFile(EnvPath(proj)); err == nil {
			content = b
		}
		out, err := ApplyConfigChanges(ConfProjectEnv, content, in.Env.Changes)
		if err != nil {
			return ProjectResult{Env: &EnvResult{Invalid: true, Problems: []ConfigProblem{{Severity: SevError, Message: err.Error()}}}}
		}
		ps, err := SaveConfig(ConfProjectEnv, EnvPath(proj), out, expected)
		switch {
		case err == ErrConfigConflict:
			cur := ""
			if st, e := os.Stat(EnvPath(proj)); e == nil {
				cur = st.ModTime().Format(time.RFC3339Nano)
			}
			return ProjectResult{Env: &EnvResult{Conflict: true, MTime: cur, Problems: ps}}
		case err == ErrConfigInvalid:
			return ProjectResult{Env: &EnvResult{Invalid: true, Problems: ps}}
		case err != nil:
			return fail("env.yaml : %v", err)
		}
		er := &EnvResult{Saved: true, Problems: ps}
		if st, e := os.Stat(EnvPath(proj)); e == nil {
			er.MTime = st.ModTime().Format(time.RFC3339Nano)
		}
		res.Env, res.Note = er, ConfigReloadNote(ConfProjectEnv)
	}
	// 3. couleur
	if in.Color != nil {
		if _, err := ApplyProjectColor(filepath.Base(proj), *in.Color); err != nil {
			// partiel : l'env.yaml est déjà écrit, l'interface doit en connaître le résultat
			res.OK, res.Msg = false, "config.conf : "+err.Error()
			if err == ErrConfigInvalid || err == ErrConfigConflict {
				res.Msg += " — réessayer, ou éditer config.conf à la main"
			}
			return res
		}
		res.Color = true
	}
	// 4. backend et image
	if in.Backend != nil || in.Image != nil {
		var err error
		if backend == "host" {
			err = ApplyExec(proj, "", "")
		} else {
			err = ApplyExec(proj, image, backend)
		}
		if err != nil {
			res.OK, res.Msg = false, err.Error() // partiel : env.yaml et couleur déjà écrits
			return res
		}
	}
	return res
}
