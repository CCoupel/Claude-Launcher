package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Fichiers de configuration en texte brut (#117) ────────────────────────
//
// Consulter, valider et enregistrer les fichiers de configuration du launcher
// depuis l'interface : config.conf, backends.yaml, env.yaml (outillage de
// l'utilisateur) et le env.yaml d'un projet. La validation passe par le code
// du chargement réel (bash ou confLite pour config.conf, backendsOf pour
// backends.yaml, parseEnvFile et la fusion de LoadEnv pour env.yaml) ; les
// erreurs bloquent l'enregistrement, les avertissements non. L'enregistrement
// est atomique, garde une copie .bak et refuse d'écraser un fichier modifié
// sur le disque depuis son ouverture. projects.json et les caches sont des
// états, pas de la configuration : exclus. API réutilisée par l'interface
// structurée (v6.1.0).

// ConfigKind : type d'un fichier de configuration.
type ConfigKind string

const (
	ConfConf        ConfigKind = "config.conf"   // Ng2ConfigDir()/config.conf
	ConfBackends    ConfigKind = "backends.yaml" // Ng2ConfigDir()/backends.yaml
	ConfLauncherEnv ConfigKind = "env.yaml"      // Ng2ConfigDir()/env.yaml (bootstrap du launcher)
	ConfProjectEnv  ConfigKind = "project-env"   // <projet>/.claude-launcher/env.yaml
)

// ConfigFile : un fichier de configuration, présent ou non.
type ConfigFile struct {
	Kind    ConfigKind `json:"kind"`
	Label   string     `json:"label"`
	Path    string     `json:"path"`
	Exists  bool       `json:"exists"`
	ModTime time.Time  `json:"modTime"`
	Size    int64      `json:"size"`
	// projet : env-template.yaml du template (lecture seule), s'il existe
	Template string `json:"template,omitempty"`
}

// ConfigProblem : erreur (bloque l'enregistrement) ou avertissement, avec sa
// ligne (0 : tout le fichier).
type ConfigProblem struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"` // error, warning
	Message  string `json:"message"`
}

const (
	SevError   = "error"
	SevWarning = "warning"
)

// HasErrors : au moins une erreur (l'enregistrement est refusé).
func HasErrors(ps []ConfigProblem) bool {
	for _, p := range ps {
		if p.Severity == SevError {
			return true
		}
	}
	return false
}

var (
	// ErrConfigConflict : le fichier a changé sur le disque depuis son ouverture.
	ErrConfigConflict = errors.New("le fichier a été modifié sur le disque depuis son ouverture")
	// ErrConfigInvalid : erreurs de validation, rien n'est écrit.
	ErrConfigInvalid = errors.New("erreurs dans le fichier : enregistrement refusé")
)

func configFile(kind ConfigKind, label, path string) ConfigFile {
	f := ConfigFile{Kind: kind, Label: label, Path: path}
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		f.Exists, f.ModTime, f.Size = true, st.ModTime(), st.Size()
	}
	return f
}

// ConfigFiles : fichiers de configuration du launcher, et ceux du projet si
// project n'est pas vide.
func ConfigFiles(project string) []ConfigFile {
	out := []ConfigFile{
		configFile(ConfConf, "config.conf — réglages du launcher", ConfigPath()),
		configFile(ConfBackends, "backends.yaml — backends, pools, réglages par machine", BackendsPath()),
		configFile(ConfLauncherEnv, "env.yaml — outillage ajouté à chaque conteneur", LauncherEnvPath()),
	}
	if project != "" {
		f := configFile(ConfProjectEnv, "env.yaml du projet "+filepath.Base(project), EnvPath(project))
		if _, err := os.Stat(envTemplatePath(project)); err == nil {
			f.Template = envTemplatePath(project)
		}
		out = append(out, f)
	}
	return out
}

// configPathOK : path est bien le fichier de ce type (l'éditeur n'écrit rien
// d'autre que ces fichiers).
func configPathOK(kind ConfigKind, path string) bool {
	clean := filepath.Clean(path)
	switch kind {
	case ConfConf:
		return clean == filepath.Clean(ConfigPath())
	case ConfBackends:
		return clean == filepath.Clean(BackendsPath())
	case ConfLauncherEnv:
		return clean == filepath.Clean(LauncherEnvPath())
	case ConfProjectEnv:
		return filepath.Base(clean) == "env.yaml" && filepath.Base(filepath.Dir(clean)) == ".claude-launcher" &&
			filepath.IsAbs(clean)
	}
	return false
}

// ConfigTemplate : contenu proposé pour un fichier absent.
func ConfigTemplate(kind ConfigKind) string {
	switch kind {
	case ConfConf:
		return defaultConfig
	case ConfBackends:
		return backendsExample
	case ConfLauncherEnv:
		return launcherEnvExample
	case ConfProjectEnv:
		return projectEnvExample
	}
	return ""
}

const backendsExample = `# backends.yaml — backends d'exécution des projets (claude-launcher-ng2 backends : diagnostic)
# Sans ce fichier : l'hôte, et podman / docker s'ils répondent (Linux).
#
# backends:
#   srv1-docker: { type: docker, host: tcp://192.168.1.211:2375, shared: [/share/MFS/ROOT] }
#   srv1-sshd:   { type: ssh, host: srv1, engine: docker, shared: [/share/MFS/ROOT] }
#   lab:         { type: k8s, context: k8s, namespace: claude-ng2,
#                  volumes: { /share/MFS/ROOT: claude-shared } }
#
# defaults:
#   backend: srv1-docker          # exécution présélectionnée à la création d'un projet
#   image: docker.io/library/debian:12
#
# hubs:                           # réglages propres à une machine (hostname)
#   MON-PC:
#     paths:
#       - { local: '\\wsl.localhost\Ubuntu\share\MFS', remote: /share/MFS }
backends: {}
`

const launcherEnvExample = `# env.yaml du launcher — ajouté au bootstrap de chaque conteneur (paquets, setup, on_start)
# packages: [ripgrep, jq]
# setup:
#   - echo "outillage personnel"
# on_start:
#   - echo "à chaque démarrage de session"
`

const projectEnvExample = `# env.yaml du projet — fusionné après env-template.yaml (fourni par le template)
image: docker.io/library/debian:12
# packages: [nodejs, npm]
# setup:
#   - npm ci
# local: [node_modules]     # dossiers privés au conteneur
# watch: [package-lock.json] # rejouer setup quand ces fichiers changent
`

var lineRe = regexp.MustCompile(`line (\d+)`)

// problemFromErr : erreur de chargement → problème, ligne tirée du message
// (« yaml: line 3: … », « line 4: cannot unmarshal … »), sinon cherchée dans
// le contenu (nom de backend, valeur citée « … »).
func problemFromErr(err error, content []byte, find ...string) ConfigProblem {
	msg := err.Error()
	p := ConfigProblem{Severity: SevError, Message: msg}
	if m := lineRe.FindStringSubmatch(msg); m != nil {
		p.Line, _ = strconv.Atoi(m[1])
		return p
	}
	// d'abord une clé YAML de ce nom (« bad: »), puis le texte n'importe où
	for _, s := range find {
		if l := keyLine(content, s); s != "" && l > 0 {
			p.Line = l
			return p
		}
	}
	for _, s := range find {
		if s == "" {
			continue
		}
		if l := lineOf(content, s); l > 0 {
			p.Line = l
			return p
		}
	}
	return p
}

// keyLine : première ligne (1…) où name est une clé YAML (« name: », « - name: »).
func keyLine(content []byte, name string) int {
	re, err := regexp.Compile(`^\s*(-\s*)?["']?` + regexp.QuoteMeta(name) + `["']?\s*:`)
	if err != nil {
		return 0
	}
	for i, l := range strings.Split(string(content), "\n") {
		if re.MatchString(l) {
			return i + 1
		}
	}
	return 0
}

// lineOf : première ligne (1…) qui contient s.
func lineOf(content []byte, s string) int {
	for i, l := range strings.Split(string(content), "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	return 0
}

var quotedRe = regexp.MustCompile(`[«"]\s?([^»"]+?)\s?[»"]`)

// quotedNames : noms cités dans un message (« backend "x" », « local « y » »).
func quotedNames(msg string) []string {
	var out []string
	for _, m := range quotedRe.FindAllStringSubmatch(msg, -1) {
		out = append(out, m[1])
	}
	return out
}

// ValidateConfig : problèmes du contenu d'un fichier de configuration, par le
// code du chargement réel. Sans erreur, le fichier peut être enregistré ; les
// avertissements (backend refusé sous Windows, racine introuvable…) n'y
// font pas obstacle.
func ValidateConfig(kind ConfigKind, content []byte) []ConfigProblem {
	ps := validateConfig(kind, content)
	sortProblems(ps)
	return ps
}

func validateConfig(kind ConfigKind, content []byte) []ConfigProblem {
	switch kind {
	case ConfConf:
		return validateConf(content)
	case ConfBackends:
		return validateBackends(content, runtime.GOOS, hubName())
	case ConfLauncherEnv:
		if _, err := parseEnvFile(content, "env.yaml"); err != nil {
			return []ConfigProblem{problemFromErr(err, content, quotedNames(err.Error())...)}
		}
		// mêmes clés que loadLauncherEnv : les autres sont réservées au env.yaml d'un
		// projet, et le bootstrap des conteneurs échouerait au lancement suivant
		var keys map[string]any
		_ = yaml.Unmarshal(content, &keys)
		var ps []ConfigProblem
		for k := range keys {
			if !launcherEnvKeys[k] {
				ps = append(ps, ConfigProblem{Line: keyLine(content, k), Severity: SevError,
					Message: "clé « " + k + " » réservée au env.yaml d'un projet (ici : packages, packages_by_manager, setup, on_start)"})
			}
		}
		return ps
	case ConfProjectEnv:
		return validateProjectEnv(content, "")
	}
	return []ConfigProblem{{Severity: SevError, Message: "type de fichier inconnu : " + string(kind)}}
}

// ValidateProjectEnv : env.yaml d'un projet, fusionné avec son
// env-template.yaml (même fusion que LoadEnv : « image » obligatoire au total).
func ValidateProjectEnv(project string, content []byte) []ConfigProblem {
	ps := validateProjectEnv(content, project)
	sortProblems(ps)
	return ps
}

func validateProjectEnv(content []byte, project string) []ConfigProblem {
	p, err := parseEnvFile(content, envFile)
	if err != nil {
		return []ConfigProblem{problemFromErr(err, content, quotedNames(err.Error())...)}
	}
	var ps []ConfigProblem
	var keys map[string]any
	_ = yaml.Unmarshal(content, &keys)
	e := p
	if project != "" {
		if tb, err := os.ReadFile(envTemplatePath(project)); err == nil {
			t, err := parseEnvFile(tb, envTemplateFile)
			if err != nil {
				ps = append(ps, ConfigProblem{Severity: SevWarning, Message: "env-template.yaml du template : " + err.Error()})
			} else {
				e = t.withProject(p)
			}
		}
	}
	if e.Image == "" {
		ps = append(ps, ConfigProblem{Severity: SevError, Message: "« image » manquante (ni dans env.yaml ni dans env-template.yaml)"})
	}
	if pr, ok := keys["prefer"].(string); ok && pr != "" {
		if _, found := FindBackend(pr); !found {
			ps = append(ps, ConfigProblem{Line: lineOf(content, "prefer"), Severity: SevWarning, Message: "prefer : backend « " + pr + " » absent de backends.yaml"})
		}
	}
	return ps
}

// MergedProjectEnv : environnement fusionné (env-template.yaml puis env.yaml
// édité), en YAML, pour l'affichage en lecture seule.
func MergedProjectEnv(project string, content []byte) (string, error) {
	p, err := parseEnvFile(content, envFile)
	if err != nil {
		return "", err
	}
	e := p
	if tb, err := os.ReadFile(envTemplatePath(project)); err == nil {
		t, err := parseEnvFile(tb, envTemplateFile)
		if err != nil {
			return "", err
		}
		e = t.withProject(p)
	}
	e.raw = nil
	b, err := yaml.Marshal(e)
	return string(b), err
}

// validateBackends : même contrôle que LoadBackends (backendsOf), plus les
// avertissements : backends refusés sous Windows, réglages de la machine
// (hubs) ignorés, serveur web du navigateur.
func validateBackends(content []byte, goos, host string) []ConfigProblem {
	var f backendsFile
	if err := yaml.Unmarshal(content, &f); err != nil {
		return []ConfigProblem{problemFromErr(fmt.Errorf("backends.yaml : %w", err), content)}
	}
	bs, err := backendsOf(f)
	if err != nil {
		return []ConfigProblem{problemFromErr(err, content, quotedNames(err.Error())...)}
	}
	var ps []ConfigProblem
	// références : le backend par défaut doit exister (une suppression ou un
	// renommage qui l'oublie est refusé, par l'éditeur texte comme par Réglages)
	if d := f.Defaults.Backend; d != "" {
		known := false
		for _, b := range bs {
			known = known || b.Name == d
		}
		if !known {
			ps = append(ps, ConfigProblem{Line: lineOf(content, d), Severity: SevError,
				Message: "defaults.backend désigne un backend inexistant : " + d})
		}
	}
	if d := f.Defaults.Image; d != "" && len(f.Images) > 0 {
		in := false
		for _, im := range f.Images {
			in = in || im == d
		}
		if !in {
			ps = append(ps, ConfigProblem{Line: lineOf(content, d), Severity: SevWarning,
				Message: "defaults.image « " + d + " » n'est pas dans la liste images (elle sera proposée en plus)"})
		}
	}
	ev := runtimeEngineEnv()
	for _, b := range bs {
		if b.Name == "host" && goos == "windows" {
			continue // host implicite : refusé sans bruit
		}
		if pr, refused := localRefusal(goos, b, ev); refused {
			ps = append(ps, ConfigProblem{Line: keyLine(content, b.Name), Severity: SevWarning, Message: "backend " + b.Name + " : " + pr.What})
		}
	}
	for _, w := range resolveHubSettingsFor(f, host, goos).warns {
		ps = append(ps, ConfigProblem{Line: lineOf(content, "paths"), Severity: SevWarning, Message: "hubs." + host + " : " + w})
	}
	if _, err := resolveBrowserServer(f, host); err != nil {
		ps = append(ps, ConfigProblem{Line: lineOf(content, "browser_server"), Severity: SevError, Message: err.Error()})
	}
	return ps
}

// validateConf : config.conf. Linux/macOS : syntaxe par bash -n, puis le
// fichier sourcé comme au chargement (ses messages d'erreur sont des
// avertissements : le chargement les ignore) ; Windows : guillemets et
// parenthèses (lecture de confLite). Puis le chargement lui-même : racines
// introuvables, chemins Linux sans correspondance.
func validateConf(content []byte) []ConfigProblem {
	// fichier écrit sous Windows (CRLF) : jugé sur ses lignes, bash ne lit pas le « \r »
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	dir, err := os.MkdirTemp("", "cl-conf-")
	if err != nil {
		return []ConfigProblem{{Severity: SevError, Message: err.Error()}}
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "config.conf")
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return []ConfigProblem{{Severity: SevError, Message: err.Error()}}
	}
	var ps []ConfigProblem
	if runtime.GOOS == "windows" {
		ps = append(ps, confLiteCheck(string(content))...)
	} else if _, err := exec.LookPath("bash"); err == nil {
		var syn bytes.Buffer
		c := exec.Command("bash", "-n", tmp)
		c.Stderr = &syn
		if c.Run() != nil {
			ps = append(ps, bashProblems(syn.String(), tmp, SevError)...)
			if len(ps) == 0 {
				ps = append(ps, ConfigProblem{Severity: SevError, Message: "syntaxe bash invalide"})
			}
			return ps
		}
		var run bytes.Buffer
		c = exec.Command("bash", "-c", `source "$1" >/dev/null`, "_", tmp)
		c.Env = EnvWithout("CLAUDE_CONFIG_DIR")
		c.Stderr = &run
		_ = c.Run()
		ps = append(ps, bashProblems(run.String(), tmp, SevWarning)...)
	}
	if HasErrors(ps) {
		return ps
	}
	cfg := loadConfigAt(tmp)
	if os.Getenv("CL_ROOTS") == "" {
		for _, r := range cfg.Roots {
			if st, err := os.Stat(r); err != nil || !st.IsDir() {
				ps = append(ps, ConfigProblem{Line: lineOf(content, "GITHUB_DIR"), Severity: SevWarning, Message: "racine de projets introuvable : " + r})
			}
		}
	}
	for _, u := range cfg.Unmapped {
		ps = append(ps, ConfigProblem{Line: lineOf(content, strings.Fields(u)[0]), Severity: SevWarning, Message: u + " : aucune correspondance Linux → Windows (hubs.<machine>.paths de backends.yaml)"})
	}
	return ps
}

var bashLineRe = regexp.MustCompile(`line (\d+): (.*)`)

// bashProblems : messages de bash (« fichier: line 3: … ») → problèmes.
func bashProblems(stderr, file, sev string) []ConfigProblem {
	var out []ConfigProblem
	for _, l := range strings.Split(strings.TrimSpace(stderr), "\n") {
		l = strings.TrimSpace(strings.ReplaceAll(l, file+": ", ""))
		if l == "" {
			continue
		}
		p := ConfigProblem{Severity: sev, Message: l}
		if m := bashLineRe.FindStringSubmatch(l); m != nil {
			p.Line, _ = strconv.Atoi(m[1])
			p.Message = m[2]
		}
		out = append(out, p)
	}
	return out
}

// confLiteCheck : ce que confLite ne sait pas lire — guillemet non fermé,
// parenthèse non fermée ou en trop —, ligne par ligne.
func confLiteCheck(src string) []ConfigProblem {
	var ps []ConfigProblem
	line, quote, quoteLine, depth, openLine := 1, byte(0), 0, 0, 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\n':
			line++
		case quote != 0:
			if c == '\\' && quote == '"' && i+1 < len(src) {
				i++
				if src[i] == '\n' {
					line++
				}
			} else if c == quote {
				quote = 0
			}
		case c == '#' && (i == 0 || strings.IndexByte(" \t\n;(", src[i-1]) >= 0):
			for i < len(src) && src[i] != '\n' {
				i++
			}
			i--
		case c == '\\' && i+1 < len(src):
			i++
			if src[i] == '\n' {
				line++
			}
		case c == '\'' || c == '"':
			quote, quoteLine = c, line
		case c == '(':
			if depth == 0 {
				openLine = line
			}
			depth++
		case c == ')':
			if depth == 0 {
				ps = append(ps, ConfigProblem{Line: line, Severity: SevError, Message: "« ) » sans « ( »"})
			} else {
				depth--
			}
		}
	}
	if quote != 0 {
		ps = append(ps, ConfigProblem{Line: quoteLine, Severity: SevError, Message: "guillemet " + string(quote) + " non fermé"})
	}
	if depth > 0 {
		ps = append(ps, ConfigProblem{Line: openLine, Severity: SevError, Message: "« ( » non fermée"})
	}
	return ps
}

// SaveConfig : enregistre content dans path (fichier de type kind) — validé
// d'abord (erreurs : rien n'est écrit, ErrConfigInvalid), refusé si le fichier
// a changé sur le disque depuis son ouverture (expectedMtime : sa date à
// l'ouverture, zéro s'il était absent ; ErrConfigConflict), copie .bak de la
// version précédente, écriture atomique (fichier temporaire puis rename).
// Rend les problèmes (avertissements compris).
func SaveConfig(kind ConfigKind, path string, content []byte, expectedMtime time.Time) ([]ConfigProblem, error) {
	if !configPathOK(kind, path) {
		return nil, fmt.Errorf("%s : pas un fichier de configuration %s", path, kind)
	}
	var ps []ConfigProblem
	if kind == ConfProjectEnv {
		ps = ValidateProjectEnv(filepath.Dir(filepath.Dir(path)), content)
	} else {
		ps = ValidateConfig(kind, content)
	}
	if HasErrors(ps) {
		return ps, ErrConfigInvalid
	}
	st, err := os.Stat(path)
	switch {
	case err == nil && (expectedMtime.IsZero() || !st.ModTime().Equal(expectedMtime)):
		return ps, ErrConfigConflict
	case err != nil && !os.IsNotExist(err):
		return ps, err
	case err != nil && !expectedMtime.IsZero():
		return ps, ErrConfigConflict // supprimé depuis l'ouverture
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ps, err
	}
	mode := os.FileMode(0o644)
	if kind == ConfConf {
		mode = 0o600 // jeton GitHub possible
	}
	if err == nil {
		if kind != ConfConf { // config.conf : toujours 0600 (jeton), même si elle était plus ouverte
			mode = st.Mode().Perm()
		}
		old, rerr := os.ReadFile(path)
		if rerr != nil {
			return ps, rerr
		}
		bak := path + ".bak"
		if lst, lerr := os.Lstat(bak); lerr == nil && lst.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(bak) // jamais écrire à travers un lien
		}
		if werr := os.WriteFile(bak, old, mode); werr != nil {
			return ps, fmt.Errorf("copie .bak : %w", werr)
		}
		_ = os.Chmod(bak, mode) // une copie préexistante plus ouverte (config.conf.bak d'avant) est resserrée
	}
	return ps, writeFileAtomic(path, content, mode)
}

// writeFileAtomic : fichier temporaire dans le même dossier, puis rename.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmp, mode)
	return os.Rename(tmp, path)
}

// ConfigReloadNote : ce qu'un enregistrement change sans redémarrer, et ce
// qui attend le prochain lancement.
func ConfigReloadNote(kind ConfigKind) string {
	switch kind {
	case ConfConf:
		return "relu : options de claude, couleurs, mise en page, consommation, reprise ; au prochain lancement : racines de projets (surveillance), CLAUDE_CONFIG_DIR"
	case ConfBackends:
		return "relu : backends, pools, correspondance de chemins, outils requis, serveur web du navigateur"
	case ConfLauncherEnv, ConfProjectEnv:
		return "pris en compte à la prochaine ouverture du projet (bootstrap du conteneur)"
	}
	return ""
}

// sortProblems : par ligne, erreurs d'abord.
func sortProblems(ps []ConfigProblem) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Line != ps[j].Line {
			return ps[i].Line < ps[j].Line
		}
		return ps[i].Severity == SevError && ps[j].Severity != SevError
	})
}
