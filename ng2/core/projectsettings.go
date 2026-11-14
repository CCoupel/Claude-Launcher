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

var projectColorsDecl = regexp.MustCompile(`(?m)^[ \t]*declare[ \t]+-A[ \t]+PROJECT_COLORS=\(`)

// SetProjectColor : config.conf (src) avec la couleur de name dans PROJECT_COLORS ;
// color vide : l'entrée est retirée (couleur automatique). Seule cette clé est
// touchée : le reste du fichier, commentaires compris, est conservé. Pas de
// déclaration : elle est ajoutée en fin de fichier.
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
	loc := projectColorsDecl.FindStringIndex(src)
	if loc == nil {
		if color == "" {
			return src, nil
		}
		if src != "" && !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		return src + "declare -A PROJECT_COLORS=(\n  " + entry + "\n)\n", nil
	}
	open := loc[1]
	end := open
	for inQ := byte(0); end < len(src); end++ { // « ) » hors guillemets et commentaires
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
		case c == ')':
			goto found
		}
	}
	return "", fmt.Errorf("PROJECT_COLORS : parenthèse fermante introuvable")
found:
	block := src[open:end]
	re := regexp.MustCompile(`\[[ \t]*["']?` + regexp.QuoteMeta(name) + `["']?[ \t]*\][ \t]*=[ \t]*(?:"[^"]*"|'[^']*'|[^\s)]*)`)
	if m := re.FindStringIndex(block); m != nil {
		if color != "" {
			return src[:open] + block[:m[0]] + entry + block[m[1]:] + src[end:], nil
		}
		// retrait : la ligne entière si l'entrée y est seule
		b0, b1 := m[0], m[1]
		ls := strings.LastIndex(block[:b0], "\n") + 1
		le := strings.Index(block[b1:], "\n")
		if le < 0 {
			le = len(block) - b1
		} else {
			le++
		}
		if strings.TrimSpace(block[ls:b0]) == "" && strings.TrimSpace(block[b1:b1+le]) == "" && strings.Contains(block, "\n") {
			return src[:open] + block[:ls] + block[b1+le:] + src[end:], nil
		}
		return src[:open] + block[:b0] + strings.TrimLeft(block[b1:], " \t") + src[end:], nil
	}
	if color == "" {
		return src, nil
	}
	if strings.Contains(block, "\n") {
		// avant la ligne qui porte « ) »
		ls := strings.LastIndex(block, "\n") + 1
		if strings.TrimSpace(block[ls:]) == "" {
			return src[:open] + block[:ls] + "  " + entry + "\n" + block[ls:] + src[end:], nil
		}
		return src[:open] + block + "\n  " + entry + "\n" + src[end:], nil
	}
	sep := " "
	if strings.TrimSpace(block) == "" {
		sep = ""
	}
	return src[:open] + block + sep + entry + src[end:], nil
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
	Env   *EnvResult `json:"env,omitempty"`
	Color bool       `json:"-"` // config.conf écrite (rechargement à chaud côté interface)
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
	// 2. env.yaml du projet
	if in.Env != nil && len(in.Env.Changes) > 0 {
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
			if err == ErrConfigInvalid || err == ErrConfigConflict {
				return fail("config.conf : %v — réessayer, ou « Éditer en YAML »", err)
			}
			return fail("config.conf : %v", err)
		}
		res.Color = true
	}
	// 4. backend et image
	if in.Backend != nil || in.Image != nil {
		if backend == "host" {
			if err := ApplyExec(proj, "", ""); err != nil {
				return fail("%v", err)
			}
		} else if err := ApplyExec(proj, image, backend); err != nil {
			return fail("%v", err)
		}
	}
	return res
}
