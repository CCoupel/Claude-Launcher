package main

import (
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
}

// loadConfig fait « sourcer » le conf bash par bash lui-même : le format reste
// celui du launcher actuel (tableaux, $HOME, variables), sans le réimplémenter.
// Sortie : une ligne « TAG<TAB>valeur » par information.
func loadConfig() Config {
	home, _ := os.UserHomeDir()
	cfg := Config{UsageRefreshSec: 120, ProjectColors: map[string]string{}}
	path := filepath.Join(home, ".config", "claude-launcher.conf")

	script := `
GITHUB_DIR="$HOME/GITHUB"; GITHUB_DIRS=(); USAGE_REFRESH_SEC=120; GITHUB_TOKEN=""
CLAUDE_DISABLE_MOUSE=1; CLAUDE_EXPERIMENTAL_TEAMS=1; CLAUDE_OPTIONS=""; EXTRA_ENVS=()
COLOR_PALETTE=(17 52 22 53 58 23 54 238 18 59); declare -A PROJECT_COLORS=()
[[ -f "$1" ]] && source "$1" >/dev/null 2>&1
printf 'usage\t%s\n' "$USAGE_REFRESH_SEC"
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
			case "token":
				cfg.GithubToken = f[1]
			case "mouse":
				cfg.DisableMouse = f[1] == "1"
			case "teams":
				cfg.ExpTeams = f[1] == "1"
			case "opts":
				cfg.ClaudeOptions = f[1]
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
