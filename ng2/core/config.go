package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"claude-launcher-ng2/platform"
)

// Config reprend les clés utiles du ~/.config/claude-launcher.conf existant.
type Config struct {
	Roots           []string
	UsageRefreshSec int

	GithubToken   string
	DisableMouse  bool
	ExpTeams      bool
	ClaudeOptions string
	ExtraEnvs     []string
	Palette       []string
	ProjectColors map[string]string
	// organisation des fenêtres de projet (#153) : LAYOUT_MODE (top | left), largeur du leader en
	// « left », exceptions par projet (PROJECT_LAYOUTS) ; effective : LayoutOf
	LayoutMode          string
	LayoutLeftLeaderPct int
	ProjectLayouts      map[string]string
	ProjectHideIdle     map[string]string // PROJECT_HIDE_IDLE : auto | none par projet (#209) ; absent = default
	layoutWarns         []string          // valeurs d'organisation inconnues, ignorées (validation de config.conf)

	// reprise de la dernière conversation au lancement (cf. resume.go)
	ResumeMode    string // ask | always | never
	ResumeMaxAgeH int

	// mise en page des teams (cf. layout.go) et teinte des panes teammates
	Layout2RowLeaderPct                                  int
	Layout3RowLeaderNum, Layout3RowMidNum, Layout3RowDen int
	BrightenStep, DarkenStep                             int

	// teammates inactifs cachés après ce délai en secondes (0 : jamais), #136
	TeamHideIdle int
	// TeamHideIdleDisabled : TEAM_HIDE_IDLE_ENABLED=0 (#188) — masquage automatique désactivé ; le délai TeamHideIdle est
	// conservé mais rien n'est caché (voir HideIdleArg). Valeur zéro = activé (une Config littérale garde la v6.2.0).
	TeamHideIdleDisabled bool

	// config de claude (CLAUDE_CONFIG_DIR) imposée à toutes les sessions du
	// launcher ; relative : par rapport au dossier de config.conf
	ClaudeConfigDir string

	// projet en conteneur : arrêter le conteneur quand claude se termine
	// proprement de lui-même (#50 ; la fermeture Ctrl+clic l'arrête toujours)
	CtrStopOnExit bool

	// Windows : réglages-chemins Linux (/…) sans correspondance déclarée dans
	// hubs.<machine>.paths (applyConfPaths), « RÉGLAGE chemin »
	Unmapped []string

	// RootBaseWarn : config.conf utilise ${CL_ROOT_BASE} sans que la racine soit connue (ni variable, ni emplacement
	// <racine>/.config/claude-launcher-ng2/) : les chemins concernés sont écartés (#165)
	RootBaseWarn string
}

// loadConfigScript : config.conf sourcé par bash, réglages imprimés (LoadConfig).
const loadConfigScript = `
GITHUB_DIR="${CL_ROOT_BASE:+$CL_ROOT_BASE/PROJETS}"; GITHUB_DIR="${GITHUB_DIR:-$HOME/GITHUB}"; GITHUB_DIRS=(); USAGE_REFRESH_SEC=120; GITHUB_TOKEN=""
CLAUDE_DISABLE_MOUSE=1; CLAUDE_EXPERIMENTAL_TEAMS=1; CLAUDE_OPTIONS=""; EXTRA_ENVS=()
COLOR_PALETTE=(17 52 22 53 58 23 54 238 18 59); declare -A PROJECT_COLORS=(); declare -A PROJECT_LAYOUTS=(); declare -A PROJECT_HIDE_IDLE=()
LAYOUT_MODE=top; LAYOUT_LEFT_LEADER_PCT=46
LAYOUT_2ROW_LEADER_PCT=50; LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=2; LAYOUT_3ROW_DEN=7
PANE_ACTIVE_BRIGHTEN_STEP=1; PANE_INACTIVE_DARKEN_STEP=1; RESUME_MODE=ask; RESUME_MAX_AGE_H=24; CTR_STOP_ON_EXIT=0; TEAM_HIDE_IDLE=30; TEAM_HIDE_IDLE_ENABLED=1
[[ -f "$1" ]] && source "$1" >/dev/null 2>&1
printf 'layout\t%s %s %s %s\n' "$LAYOUT_2ROW_LEADER_PCT" "$LAYOUT_3ROW_LEADER_NUM" "$LAYOUT_3ROW_MID_NUM" "$LAYOUT_3ROW_DEN"
printf 'layoutmode\t%s %s\n' "$LAYOUT_MODE" "$LAYOUT_LEFT_LEADER_PCT"
printf 'steps\t%s %s\n' "$PANE_ACTIVE_BRIGHTEN_STEP" "$PANE_INACTIVE_DARKEN_STEP"
printf 'hideidle\t%s\n' "$TEAM_HIDE_IDLE"
printf 'hideidleon\t%s\n' "$TEAM_HIDE_IDLE_ENABLED"
printf 'usage\t%s\n' "$USAGE_REFRESH_SEC"
printf 'resume\t%s %s\n' "$RESUME_MODE" "$RESUME_MAX_AGE_H"
printf 'token\t%s\n' "$GITHUB_TOKEN"
printf 'mouse\t%s\n' "$CLAUDE_DISABLE_MOUSE"
printf 'ctrstop\t%s\n' "$CTR_STOP_ON_EXIT"
printf 'teams\t%s\n' "$CLAUDE_EXPERIMENTAL_TEAMS"
printf 'opts\t%s\n' "$CLAUDE_OPTIONS"
printf 'claudedir\t%s\n' "${CLAUDE_CONFIG_DIR-}"
printf 'palette\t%s\n' "${COLOR_PALETTE[*]}"
for e in "${EXTRA_ENVS[@]}"; do printf 'env\t%s\n' "$e"; done
for k in "${!PROJECT_COLORS[@]}"; do printf 'color\t%s\t%s\n' "$k" "${PROJECT_COLORS[$k]}"; done
for k in "${!PROJECT_LAYOUTS[@]}"; do printf 'playout\t%s\t%s\n' "$k" "${PROJECT_LAYOUTS[$k]}"; done
for k in "${!PROJECT_HIDE_IDLE[@]}"; do printf 'phide\t%s\t%s\n' "$k" "${PROJECT_HIDE_IDLE[$k]}"; done
if (( ${#GITHUB_DIRS[@]} )); then for d in "${GITHUB_DIRS[@]}"; do printf 'root\t%s\n' "$d"; done
else printf 'root\t%s\n' "$GITHUB_DIR"; fi`

// LoadConfig fait « sourcer » le conf bash par bash lui-même : le format reste
// celui du launcher actuel (tableaux, $HOME, variables), sans le réimplémenter.
// Sortie : une ligne « TAG<TAB>valeur » par information.
func LoadConfig() Config { return loadConfigAt(ConfigPath()) }

// loadConfigAt : LoadConfig pour le fichier path (validation de l'éditeur de
// configuration, configedit.go : même lecture que le chargement réel).
func loadConfigAt(path string) Config {
	home, _ := os.UserHomeDir()
	cfg := Config{UsageRefreshSec: 120, ProjectColors: map[string]string{}, ProjectLayouts: map[string]string{}, ProjectHideIdle: map[string]string{}, LayoutMode: LayoutTop, LayoutLeftLeaderPct: 46,
		Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2, Layout3RowDen: 7,
		BrightenStep: 1, DarkenStep: 1, TeamHideIdle: 30, ResumeMode: "ask", ResumeMaxAgeH: 24}

	var out []byte
	var err error
	src := readFileOr(path)
	// ${CL_ROOT_BASE} dans le fichier, variable absente : racine déduite de l'emplacement du fichier (sans exporter la
	// variable ni changer RootBase()) ; sinon chemins écartés et signalés
	fbRoot, rbUnresolved := rootBaseFallback(path, src)
	if rbUnresolved {
		cfg.RootBaseWarn = "config.conf utilise ${CL_ROOT_BASE}, non résolue (variable absente, fichier hors <racine>/.config/claude-launcher-ng2/) : chemins concernés ignorés — poser CL_ROOT_BASE ou --root-base"
	}
	if runtime.GOOS == "windows" {
		// pas de bash fiable sous Windows (celui de System32 lance WSL) :
		// lecture du sous-ensemble utile du format, en Go (conflite.go)
		out = []byte(confLiteWith(src, home, fbRoot))
	} else {
		cmd := exec.Command("bash", "-c", loadConfigScript, "_", path)
		cmd.Env = EnvWithout("CLAUDE_CONFIG_DIR") // seule la valeur du fichier compte ici
		if rb, _ := RootBase(); rb != "" {
			cmd.Env = append(cmd.Env, RootBaseVar+"="+rb) // racine nettoyée : bash et Go donnent le même chemin (« /r/ » ≡ « /r »)
		} else if fbRoot != "" {
			cmd.Env = append(cmd.Env, RootBaseVar+"="+fbRoot) // pour ce sous-processus seulement
		}
		out, err = cmd.Output()
	}
	if err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.SplitN(l, "\t", 3)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "usage":
				if n, e := strconv.Atoi(f[1]); e == nil {
					cfg.UsageRefreshSec = n
				}
			case "hideidle":
				if n, e := strconv.Atoi(strings.TrimSpace(f[1])); e == nil && n >= 0 {
					cfg.TeamHideIdle = n
				}
			case "hideidleon":
				cfg.TeamHideIdleDisabled = strings.TrimSpace(f[1]) == "0" // absent ou autre que 0 : activé (v6.2.0)
			case "resume":
				if v := strings.Fields(f[1]); len(v) == 2 {
					if v[0] == "ask" || v[0] == "always" || v[0] == "never" {
						cfg.ResumeMode = v[0]
					}
					if n, e := strconv.Atoi(v[1]); e == nil && n > 0 {
						cfg.ResumeMaxAgeH = n
					}
				}
			case "token":
				cfg.GithubToken = f[1]
			case "mouse":
				cfg.DisableMouse = f[1] == "1"
			case "ctrstop":
				cfg.CtrStopOnExit = f[1] == "1"
			case "teams":
				cfg.ExpTeams = f[1] == "1"
			case "opts":
				cfg.ClaudeOptions = f[1]
			case "layout":
				if v := atois(f[1]); len(v) == 4 && v[0] > 0 && v[0] < 100 && v[3] > 0 && v[1]+v[2] < v[3] {
					cfg.Layout2RowLeaderPct, cfg.Layout3RowLeaderNum, cfg.Layout3RowMidNum, cfg.Layout3RowDen = v[0], v[1], v[2], v[3]
				}
			case "layoutmode":
				m, pct, _ := strings.Cut(f[1], " ")
				if !ValidLayout(m) {
					cfg.layoutWarns = append(cfg.layoutWarns, fmt.Sprintf("LAYOUT_MODE « %s » inconnu : top ou left attendu (top retenu)", m))
				}
				cfg.LayoutMode = NormLayout(m)
				if n := atois(pct); len(n) == 1 && n[0] >= 20 && n[0] <= 80 {
					cfg.LayoutLeftLeaderPct = n[0]
				} else {
					cfg.layoutWarns = append(cfg.layoutWarns, fmt.Sprintf("LAYOUT_LEFT_LEADER_PCT « %s » : un entier de 20 à 80 est attendu (46 retenu)", pct))
				}
			case "playout":
				if len(f) == 3 && ValidLayout(f[2]) {
					cfg.ProjectLayouts[f[1]] = f[2]
				} else if len(f) == 3 {
					cfg.layoutWarns = append(cfg.layoutWarns, fmt.Sprintf("PROJECT_LAYOUTS[%s] « %s » inconnu : top ou left attendu (réglage global retenu)", f[1], f[2]))
				}
			case "phide":
				if len(f) == 3 && ValidHideIdle(f[2]) {
					cfg.ProjectHideIdle[f[1]] = f[2]
				} else if len(f) == 3 {
					cfg.layoutWarns = append(cfg.layoutWarns, fmt.Sprintf("PROJECT_HIDE_IDLE[%s] « %s » inconnu : auto ou none attendu (réglage global retenu)", f[1], f[2]))
				}
			case "steps":
				if v := atois(f[1]); len(v) == 2 {
					cfg.BrightenStep, cfg.DarkenStep = v[0], v[1]
				}
			case "palette":
				cfg.Palette = strings.Fields(f[1])
			case "env":
				cfg.ExtraEnvs = append(cfg.ExtraEnvs, f[1])
			case "color":
				if len(f) == 3 {
					cfg.ProjectColors[f[1]] = f[2]
				}
			case "root":
				if r := strings.TrimSpace(f[1]); r != "" && !strings.Contains(r, rootBaseUnresolved) {
					// Windows : « D:\base/PROJETS » (${CL_ROOT_BASE}/PROJETS développé par confLite) → séparateurs normalisés ;
					// un chemin Linux (« /… », converti par applyConfPaths) n'est pas touché
					if runtime.GOOS == "windows" && filepath.VolumeName(r) != "" {
						r = filepath.Clean(r)
					}
					cfg.Roots = append(cfg.Roots, r)
				}
			case "claudedir":
				if d := strings.TrimSpace(f[1]); d != "" && !strings.Contains(d, rootBaseUnresolved) {
					// « /… » : absolu aussi sous Windows (chemin Linux des backends, cf. applyConfPaths)
					if !filepath.IsAbs(d) && !strings.HasPrefix(d, "/") && !strings.HasPrefix(d, `\`) {
						d = filepath.Join(filepath.Dir(path), d)
					}
					if runtime.GOOS == "windows" && strings.HasPrefix(d, "/") {
						cfg.ClaudeConfigDir = d // converti par applyConfPaths, pas filepath.Clean (« \share\… »)
					} else {
						cfg.ClaudeConfigDir = filepath.Clean(d)
					}
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		applyConfPaths(runtime.GOOS, hubSettings().paths, &cfg)
	}
	// Test : CL_ROOTS (séparées par « : », « ; » sous Windows) remplace les
	// racines du conf, pour ne jamais travailler sur de vrais projets.
	if r := os.Getenv("CL_ROOTS"); r != "" {
		cfg.Roots = filepath.SplitList(r)
	}
	if cfg.UsageRefreshSec < 60 {
		cfg.UsageRefreshSec = 60
	}
	if len(cfg.Roots) == 0 {
		cfg.Roots = []string{filepath.Join(home, "GITHUB")}
		if p := RootBaseProjects(); p != "" {
			cfg.Roots = []string{p}
		}
	}
	if cfg.ClaudeConfigDir == "" { // sous une racine de base : la config de claude y est aussi
		cfg.ClaudeConfigDir = RootBaseClaudeDir()
	}
	if len(cfg.Palette) == 0 {
		cfg.Palette = strings.Fields("17 52 22 53 58 23 54 238 18 59")
	}
	return cfg
}

// applyConfPaths : réglages-chemins de config.conf (racines, CLAUDE_CONFIG_DIR)
// rendus locaux (confLocalPath) ; ceux qui restent Linux faute de
// correspondance sont notés dans cfg.Unmapped (signalés une fois par
// « backends », PrintHubPaths). Logique pure, sans effet hors Windows.
func applyConfPaths(goos string, m PathMapper, cfg *Config) {
	for i, r := range cfg.Roots {
		l, ok := confLocalPath(goos, m, r)
		if !ok {
			cfg.Unmapped = append(cfg.Unmapped, "GITHUB_DIR(S) "+l)
		}
		cfg.Roots[i] = l
	}
	if d := cfg.ClaudeConfigDir; d != "" {
		l, ok := confLocalPath(goos, m, d)
		if !ok {
			cfg.Unmapped = append(cfg.Unmapped, "CLAUDE_CONFIG_DIR "+l)
		}
		cfg.ClaudeConfigDir = l
	}
}

// atois : entiers séparés par des espaces (nil si l'un ne l'est pas).
func atois(s string) []int {
	var v []int
	for _, f := range strings.Fields(s) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		v = append(v, n)
	}
	return v
}

// LayoutOf : organisation effective d'un projet (PROJECT_LAYOUTS, puis LAYOUT_MODE, puis top).
// Fonction unique, appelée par la coque Linux et par l'interface native.
func (c Config) LayoutOf(project string) string {
	if m, ok := c.ProjectLayouts[project]; ok && ValidLayout(m) {
		return m
	}
	return NormLayout(c.LayoutMode)
}

// ProjectColor : même hash déterministe que get_project_color du bash
// (nom identique → couleur identique, la config pouvant surcharger).
func (c Config) ProjectColor(name string) string {
	if v, ok := c.ProjectColors[name]; ok {
		return v
	}
	h := 0
	for _, r := range name {
		h = (h*31 + int(r)) % len(c.Palette)
	}
	return c.Palette[h]
}

// ConfigPath : config.conf du dossier de config (ng2ConfigDir).
func ConfigPath() string { return filepath.Join(Ng2ConfigDir(), "config.conf") }

// Ng2ConfigDir : dossier de config du launcher (config.conf, backends.yaml).
// D'abord à côté du binaire — <dossier du binaire>/.config/claude-launcher-ng2,
// s'il contient un config.conf : launcher portable, posé avec sa config sur un
// stockage partagé —, sinon ~/.config/claude-launcher-ng2 (Windows :
// %AppData%\claude-launcher-ng2). CL_CONFIG_DIR force un dossier (tests).
func Ng2ConfigDir() string {
	if d := os.Getenv("CL_CONFIG_DIR"); d != "" {
		return d
	}
	// racine de base (#165) : retenue même sans config.conf encore créé
	if d := RootBaseConfigDir(); d != "" {
		return d
	}
	if d := portableConfigDir(); d != "" {
		if _, err := os.Stat(filepath.Join(d, "config.conf")); err == nil {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	ucd := ""
	if runtime.GOOS == "windows" {
		ucd, _ = os.UserConfigDir()
	}
	return defaultConfigDir(runtime.GOOS, home, ucd)
}

// defaultConfigDir : dossier de config par défaut (logique pure).
func defaultConfigDir(goos, home, userConfigDir string) string {
	if goos == "windows" && userConfigDir != "" {
		return filepath.Join(userConfigDir, "claude-launcher-ng2")
	}
	return filepath.Join(home, ".config", "claude-launcher-ng2")
}

// portableConfigDir : <dossier du binaire>/.config/claude-launcher-ng2 (liens résolus).
func portableConfigDir() string {
	exe, err := platform.SelfExe()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), ".config", "claude-launcher-ng2")
}

// v4ConfigPath : config du launcher v4, importée une fois à la création (ensureConfig).
func v4ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-launcher.conf")
}

const defaultConfig = `# claude-launcher — configuration utilisateur
# Éditer puis relancer le launcher.

# Répertoire contenant vos projets (utilisé si GITHUB_DIRS est vide)
GITHUB_DIR="$HOME/GITHUB"

# Plusieurs dossiers racine — si renseigné, remplace GITHUB_DIR
# Les projets seront listés groupés par dossier dans le menu
# GITHUB_DIRS=("$HOME/GITHUB" "/mnt/c/Users/username/Documents/VScode/GITHUB")
GITHUB_DIRS=()

# Token GitHub (gh CLI + Claude Code MCP)
GITHUB_TOKEN=""

# Config de claude pour toutes les sessions du launcher (hôte et conteneurs) —
# ex. sur un stockage partagé. Relative : par rapport au dossier de ce fichier.
# Une variable CLAUDE_CONFIG_DIR déjà définie au lancement l'emporte.
# CLAUDE_CONFIG_DIR="/chemin/partage/.claude"

# Options Claude Code
CLAUDE_DISABLE_MOUSE=1        # 1 = désactive la souris dans le terminal
CLAUDE_EXPERIMENTAL_TEAMS=1  # 1 = active les agent teams
CLAUDE_OPTIONS="--allow-dangerously-skip-permissions"

# Reprise de la dernière conversation à l'ouverture d'un projet (après un crash,
# un terminal fermé…) : ask = question [O/n], always = sans question, never = jamais.
# Proposée seulement si la conversation date de moins de RESUME_MAX_AGE_H heures
# et n'a pas été close par /end-session ;
# la team d'une telle session (liste de tâches) est épargnée par le nettoyage.
# RESUME_MODE=ask
# RESUME_MAX_AGE_H=24

# Variables d'environnement supplémentaires passées à Claude Code
# Format : "NOM=valeur"  (une par ligne, sans export)
# Exemple :
#   EXTRA_ENVS=(
#     "ANTHROPIC_API_KEY=sk-ant-..."
#     "MY_API_URL=https://api.example.com"
#   )
EXTRA_ENVS=()

# Couleurs de fond par projet (optionnel — sinon assigné automatiquement)
# Valeurs : indices tmux 256 couleurs (ex: 17=bleu nuit, 52=bordeaux, 22=vert, 53=prune)
# declare -A PROJECT_COLORS=(
#   ["mon-projet"]="53"
#   ["autre-projet"]="17"
# )

# Organisation des fenêtres de projet : top (leader en haut, défaut) ou left (leader à gauche, teammates
# à droite ; une colonne jusqu'à 4 teammates, deux à partir de 5). Largeur du leader en « left » : 20 à 80 %.
# LAYOUT_MODE=top
# LAYOUT_LEFT_LEADER_PCT=46
# Exceptions par projet (optionnel) : top ou left
# declare -A PROJECT_LAYOUTS=(
#   ["mon-projet"]="left"
# )

# Consommation du compte Claude : secondes entre deux lectures (min. 60, défaut 120).
# Le milestone-panel des projets lit aussi cette valeur (repli 5 min si absente).
# USAGE_REFRESH_SEC=120

# Surbrillance des panes teammates actifs (agent en train de travailler) :
# variante éclaircie de la couleur du projet. 1 = léger, 5 = blanc/couleur pleine.
# PANE_ACTIVE_BRIGHTEN_STEP=1

# Assombrissement des panes teammates inactifs (agent en IDLE) :
# variante assombrie de la couleur du projet. 1 = léger, 5 = quasi-noir.
# PANE_INACTIVE_DARKEN_STEP=1

# Teammates inactifs : cachés (mis de côté, ils continuent de tourner) après ce
# délai en secondes sans activité ; ramenés dès qu'ils travaillent. 0 = jamais.
# Double-clic sur un teammate de la carte TEAM : l'afficher / le cacher à la main.
# TEAM_HIDE_IDLE=30
# Masquage automatique activé (1, défaut) ou désactivé (0) : désactivé, aucun teammate n'est caché, le délai ci-dessus
# est conservé pour le rallumage et le masquage à la main (double-clic) est refusé.
# TEAM_HIDE_IDLE_ENABLED=1
# Exceptions par projet (optionnel) : auto (masque, quel que soit le réglage ci-dessus) ou none (ne masque jamais) ;
# absent = suit TEAM_HIDE_IDLE_ENABLED. Le délai reste TEAM_HIDE_IDLE (0 = jamais).
# declare -A PROJECT_HIDE_IDLE=(
#   ["mon-projet"]="none"
# )
# Mise en page des teams : part du leader sur 2 lignes (≤ 5 teammates), en %,
# et fractions leader / ligne du milieu sur 3 lignes (la dernière a le reste).
# LAYOUT_2ROW_LEADER_PCT=50
# LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=2; LAYOUT_3ROW_DEN=7
`

// ApplyClaudeConfigDir : CLAUDE_CONFIG_DIR de la config, posé dans
// l'environnement du launcher (donc de ses sous-commandes et de ses sessions)
// s'il n'est pas déjà défini. Appelé au tout début de main.
func ApplyClaudeConfigDir() {
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return
	}
	if d := LoadConfig().ClaudeConfigDir; d != "" {
		_ = os.Setenv("CLAUDE_CONFIG_DIR", d)
	}
}

// EnsureConfig crée la config par défaut si elle n'existe pas encore.
func EnsureConfig() (created bool, err error) {
	path := ConfigPath()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	content := []byte(defaultConfigFor())
	if rb, _ := RootBase(); rb == "" { // sous une racine de base : jamais la config v4 du dossier personnel
		if b, err := os.ReadFile(v4ConfigPath()); err == nil {
			content = b // passage v4 → v5 : on reprend les réglages existants
		}
	}
	sweepConfigTmp() // restes d'écritures interrompues (#217)
	// création exclusive : deux launchers qui démarrent ensemble sur une racine neuve n'écrasent pas la config de l'autre
	created, err = createExclusiveAtomic(path, content, 0o600)
	return created, err
}

// RunConfigure : crée la config si besoin puis l'ouvre dans $EDITOR (nano par défaut).
func RunConfigure() error {
	created, err := EnsureConfig()
	if err != nil {
		return err
	}
	if created {
		fmt.Println("✓ Config créée :", ConfigPath())
	}
	a := editorArgv(runtime.GOOS, os.Getenv("EDITOR"), ConfigPath())
	c := exec.Command(a[0], a[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// editorArgv : commande d'édition de path ($EDITOR, nano par défaut ; Windows :
// pas de sh, notepad par défaut).
func editorArgv(goos, editor, path string) []string {
	if goos == "windows" {
		a := strings.Fields(editor)
		if len(a) == 0 {
			a = []string{"notepad"}
		}
		return append(a, path)
	}
	if editor == "" {
		editor = "nano"
	}
	return []string{"sh", "-c", editor + ` "$1"`, "_", path}
}

// HideIdleProjectDisabledMsg : même refus quand le projet est en « no hide » (PROJECT_HIDE_IDLE=none, #209).
const HideIdleProjectDisabledMsg = "masquage désactivé pour ce projet : choisir « auto hide » dans le titre de la carte TEAM"

// Choix de masquage d'un projet (PROJECT_HIDE_IDLE) : auto = masque, none = ne masque jamais, default = suit le global.
const (
	HideIdleAuto    = "auto"
	HideIdleNone    = "none"
	HideIdleDefault = "default"
)

// ValidHideIdle : valeur reconnue de PROJECT_HIDE_IDLE (« default » = entrée absente, jamais écrite).
func ValidHideIdle(v string) bool { return v == HideIdleAuto || v == HideIdleNone }

// HideIdleInfo : état du masquage pour la carte TEAM (champ teamHead.hideIdle) — Enabled : valeur effective (pour le
// projet via HideIdleOf), Delay : TEAM_HIDE_IDLE conservé même éteint, Mode : choix du projet (auto | default | none),
// Global : TEAM_HIDE_IDLE_ENABLED (infobulle). Masquage effectif : Enabled et Delay > 0.
type HideIdleInfo struct {
	Enabled bool   `json:"enabled"`
	Delay   int    `json:"delay"`
	Mode    string `json:"mode,omitempty"`
	Global  bool   `json:"global"`
}

// HideIdleModeOf : choix du projet (clé = nom du dossier, comme PROJECT_COLORS) : auto, none ou default.
func (c Config) HideIdleModeOf(project string) string {
	if m, ok := c.ProjectHideIdle[filepath.Base(project)]; ok && ValidHideIdle(m) {
		return m
	}
	return HideIdleDefault
}

// hideIdleOn : le masquage est-il activé pour ce projet ? auto → oui ; none → non ; default → TEAM_HIDE_IDLE_ENABLED (#188).
// Le délai n'intervient pas ici : un délai 0 (jamais) laisse le masquage à la main permis (#188).
func (c Config) hideIdleOn(project string) bool {
	switch c.HideIdleModeOf(project) {
	case HideIdleAuto:
		return true
	case HideIdleNone:
		return false
	}
	return !c.TeamHideIdleDisabled
}

// HideIdleOf : LA fonction du masquage d'un projet (règle de visibilité, carte TEAM de l'interface native et de la coque
// Linux, refus de teamToggle). Enabled = masquage EFFECTIF : activé pour le projet (auto → oui, none → non, default →
// TEAM_HIDE_IDLE_ENABLED) ET délai TEAM_HIDE_IDLE > 0 (un délai 0 désactive dans tous les cas).
func (c Config) HideIdleOf(project string) HideIdleInfo {
	return HideIdleInfo{Enabled: c.hideIdleOn(project) && c.TeamHideIdle > 0, Delay: c.TeamHideIdle,
		Mode: c.HideIdleModeOf(project), Global: !c.TeamHideIdleDisabled}
}

// HideIdleArgOf : valeur transmise à la règle de visibilité (DecidePaneVis, TeamLinesFor) pour ce projet — le délai en
// secondes quand le masquage est activé pour lui, -1 quand il est désactivé (rien n'est caché, les choix « caché à la
// main » sont ignorés et effacés).
func (c Config) HideIdleArgOf(project string) int {
	if !c.hideIdleOn(project) {
		return -1
	}
	return c.TeamHideIdle
}

// HideIdleRefusal : message de refus de la bascule manuelle d'un teammate pour ce projet, "" si le masquage est activé.
func (c Config) HideIdleRefusal(project string) string {
	if c.hideIdleOn(project) {
		return ""
	}
	return HideIdleProjectDisabledMsg
}
