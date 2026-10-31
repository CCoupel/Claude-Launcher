package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

	// reprise de la dernière conversation au lancement (cf. resume.go)
	ResumeMode    string // ask | always | never
	ResumeMaxAgeH int

	// mise en page des teams (cf. layout.go) et teinte des panes teammates
	Layout2RowLeaderPct                                  int
	Layout3RowLeaderNum, Layout3RowMidNum, Layout3RowDen int
	BrightenStep, DarkenStep                             int
}

// loadConfig fait « sourcer » le conf bash par bash lui-même : le format reste
// celui du launcher actuel (tableaux, $HOME, variables), sans le réimplémenter.
// Sortie : une ligne « TAG<TAB>valeur » par information.
func loadConfig() Config {
	home, _ := os.UserHomeDir()
	cfg := Config{UsageRefreshSec: 120, ProjectColors: map[string]string{},
		Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2, Layout3RowDen: 7,
		BrightenStep: 1, DarkenStep: 1, ResumeMode: "ask", ResumeMaxAgeH: 24}
	path := configPath()

	script := `
GITHUB_DIR="$HOME/GITHUB"; GITHUB_DIRS=(); USAGE_REFRESH_SEC=120; GITHUB_TOKEN=""
CLAUDE_DISABLE_MOUSE=1; CLAUDE_EXPERIMENTAL_TEAMS=1; CLAUDE_OPTIONS=""; EXTRA_ENVS=()
COLOR_PALETTE=(17 52 22 53 58 23 54 238 18 59); declare -A PROJECT_COLORS=()
LAYOUT_2ROW_LEADER_PCT=50; LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=2; LAYOUT_3ROW_DEN=7
PANE_ACTIVE_BRIGHTEN_STEP=1; PANE_INACTIVE_DARKEN_STEP=1; RESUME_MODE=ask; RESUME_MAX_AGE_H=24
[[ -f "$1" ]] && source "$1" >/dev/null 2>&1
printf 'layout\t%s %s %s %s\n' "$LAYOUT_2ROW_LEADER_PCT" "$LAYOUT_3ROW_LEADER_NUM" "$LAYOUT_3ROW_MID_NUM" "$LAYOUT_3ROW_DEN"
printf 'steps\t%s %s\n' "$PANE_ACTIVE_BRIGHTEN_STEP" "$PANE_INACTIVE_DARKEN_STEP"
printf 'usage\t%s\n' "$USAGE_REFRESH_SEC"
printf 'resume\t%s %s\n' "$RESUME_MODE" "$RESUME_MAX_AGE_H"
printf 'token\t%s\n' "$GITHUB_TOKEN"
printf 'mouse\t%s\n' "$CLAUDE_DISABLE_MOUSE"
printf 'teams\t%s\n' "$CLAUDE_EXPERIMENTAL_TEAMS"
printf 'opts\t%s\n' "$CLAUDE_OPTIONS"
printf 'palette\t%s\n' "${COLOR_PALETTE[*]}"
for e in "${EXTRA_ENVS[@]}"; do printf 'env\t%s\n' "$e"; done
for k in "${!PROJECT_COLORS[@]}"; do printf 'color\t%s\t%s\n' "$k" "${PROJECT_COLORS[$k]}"; done
if (( ${#GITHUB_DIRS[@]} )); then for d in "${GITHUB_DIRS[@]}"; do printf 'root\t%s\n' "$d"; done
else printf 'root\t%s\n' "$GITHUB_DIR"; fi`
	out, err := exec.Command("bash", "-c", script, "_", path).Output()
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
			case "teams":
				cfg.ExpTeams = f[1] == "1"
			case "opts":
				cfg.ClaudeOptions = f[1]
			case "layout":
				if v := atois(f[1]); len(v) == 4 && v[0] > 0 && v[0] < 100 && v[3] > 0 && v[1]+v[2] < v[3] {
					cfg.Layout2RowLeaderPct, cfg.Layout3RowLeaderNum, cfg.Layout3RowMidNum, cfg.Layout3RowDen = v[0], v[1], v[2], v[3]
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
				if r := strings.TrimSpace(f[1]); r != "" {
					cfg.Roots = append(cfg.Roots, r)
				}
			}
		}
	}
	// Test : CL_ROOTS (séparées par « : ») remplace les racines du conf, pour ne
	// jamais travailler sur de vrais projets.
	if r := os.Getenv("CL_ROOTS"); r != "" {
		cfg.Roots = strings.Split(r, ":")
	}
	if cfg.UsageRefreshSec < 60 {
		cfg.UsageRefreshSec = 60
	}
	if len(cfg.Roots) == 0 {
		cfg.Roots = []string{filepath.Join(home, "GITHUB")}
	}
	if len(cfg.Palette) == 0 {
		cfg.Palette = strings.Fields("17 52 22 53 58 23 54 238 18 59")
	}
	return cfg
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

// projectColor : même hash déterministe que get_project_color du bash
// (nom identique → couleur identique, la config pouvant surcharger).
func (c Config) projectColor(name string) string {
	if v, ok := c.ProjectColors[name]; ok {
		return v
	}
	h := 0
	for _, r := range name {
		h = (h*31 + int(r)) % len(c.Palette)
	}
	return c.Palette[h]
}

func configPath() string {
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

# Consommation du compte Claude : secondes entre deux lectures (min. 60, défaut 120).
# Le milestone-panel des projets lit aussi cette valeur (repli 5 min si absente).
# USAGE_REFRESH_SEC=120

# Surbrillance des panes teammates actifs (agent en train de travailler) :
# variante éclaircie de la couleur du projet. 1 = léger, 5 = blanc/couleur pleine.
# PANE_ACTIVE_BRIGHTEN_STEP=1

# Assombrissement des panes teammates inactifs (agent en IDLE) :
# variante assombrie de la couleur du projet. 1 = léger, 5 = quasi-noir.
# PANE_INACTIVE_DARKEN_STEP=1
# Mise en page des teams : part du leader sur 2 lignes (≤ 5 teammates), en %,
# et fractions leader / ligne du milieu sur 3 lignes (la dernière a le reste).
# LAYOUT_2ROW_LEADER_PCT=50
# LAYOUT_3ROW_LEADER_NUM=3; LAYOUT_3ROW_MID_NUM=2; LAYOUT_3ROW_DEN=7
`

// ensureConfig crée la config par défaut si elle n'existe pas encore.
func ensureConfig() (created bool, err error) {
	path := configPath()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(defaultConfig), 0o600)
}

// runConfigure : crée la config si besoin puis l'ouvre dans $EDITOR (nano par défaut).
func runConfigure() error {
	created, err := ensureConfig()
	if err != nil {
		return err
	}
	if created {
		fmt.Println("✓ Config créée :", configPath())
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}
	c := exec.Command("sh", "-c", editor+` "$1"`, "_", configPath())
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
