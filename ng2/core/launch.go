package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// scriptPath : ~/claude-launcher.sh, réduit depuis la v4 à une redirection de
// l'ancienne commande vers le NG (tenue à jour par la mise à jour automatique).
func scriptPath() string {
	if p := os.Getenv("CL_SCRIPT"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "claude-launcher.sh")
}

func Shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Exports : variables du launcher tapées dans le shell du projet avant claude.
// Elles sont aussi dans settings.json (settingsenv.go), qui seul atteint les
// teammates ; les secrets (token…) n'y sont donc tapés que si cette recopie a
// échoué (inline) — sinon ils resteraient dans l'historique et le scrollback.
func (c Config) Exports(inline bool) string {
	var e strings.Builder
	for _, v := range c.EnvVars() {
		if secretEnv(v.K) && !inline {
			continue
		}
		fmt.Fprintf(&e, "\nexport %s=%s", v.K, Shq(v.V))
	}
	return e.String()
}
