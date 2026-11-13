package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// bashConfLines : sortie du script bash de LoadConfig pour le fichier src
// (référence de confLite), lignes triées (ordre des tableaux associatifs libre).
func bashConfLines(t *testing.T, src, home string) []string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.conf")
	_ = os.WriteFile(p, []byte(src), 0o644)
	// même script que LoadConfig, extrait par la fonction de test
	cmd := exec.Command("bash", "-c", loadConfigScript, "_", p)
	cmd.Env = append(EnvWithout("CLAUDE_CONFIG_DIR"), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return sortedLines(string(out))
}

func sortedLines(s string) []string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	sort.Strings(l)
	return l
}

// confLite (Windows, sans bash) lit config.conf comme bash
func TestConfLiteMatchesBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("référence : bash")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash absent")
	}
	home := "/home/moi"
	cases := map[string]string{
		"défaut": defaultConfig,
		"vide":   "",
		"complet": `# commentaire
GITHUB_DIRS=("$HOME/GITHUB" '/share/MFS/ROOT/CLAUDE PROJETS'
  "${HOME}/autre")   # fin
GITHUB_TOKEN="ghp_abc" ; USAGE_REFRESH_SEC=30
export CLAUDE_OPTIONS="--allow-dangerously-skip-permissions --x"
CLAUDE_CONFIG_DIR="/share/MFS/ROOT/CLAUDE_PROJETS/.claude"
EXTRA_ENVS=(
  "ANTHROPIC_API_KEY=sk-ant-1"
  "MY_URL=https://x.example.com/a#b"
)
declare -A PROJECT_COLORS=(
  ["mon-projet"]="53"
  ["autre"]="17"
)
COLOR_PALETTE=(1 2 3)
RESUME_MODE=never; RESUME_MAX_AGE_H=6
LAYOUT_2ROW_LEADER_PCT=60
CTR_STOP_ON_EXIT=1
CLAUDE_DISABLE_MOUSE=0
`,
		"windows": `GITHUB_DIRS=('\\wsl.localhost\Ubuntu\share\MFS\ROOT\CLAUDE_PROJETS' "C:\Users\moi\GITHUB")
CLAUDE_CONFIG_DIR='\\wsl.localhost\Ubuntu\share\MFS\ROOT\CLAUDE_PROJETS\.claude'
`,
		"dir seul": `GITHUB_DIR="$HOME/projets"`,
	}
	for name, src := range cases {
		want := bashConfLines(t, src, home)
		got := sortedLines(confLite(src, home))
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s :\nconfLite :\n%s\nbash :\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// home Windows : racine par défaut sous %USERPROFILE%
func TestConfLiteWindowsHome(t *testing.T) {
	out := confLite("", `C:\Users\moi`)
	if !strings.Contains(out, "root\tC:\\Users\\moi\\GITHUB\n") {
		t.Errorf("racine par défaut : %s", out)
	}
	out = confLite(`GITHUB_DIRS=('\\wsl.localhost\Ubuntu\share\MFS\ROOT')`, `C:\Users\moi`)
	if !strings.Contains(out, "root\t\\\\wsl.localhost\\Ubuntu\\share\\MFS\\ROOT\n") {
		t.Errorf("racine UNC : %s", out)
	}
}

func TestDefaultDirs(t *testing.T) {
	if d := defaultConfigDir("linux", "/home/moi", "/ignoré"); d != filepath.Join("/home/moi", ".config", "claude-launcher-ng2") {
		t.Errorf("linux : %s", d)
	}
	if d := defaultConfigDir("windows", `C:\Users\moi`, "AppData"); d != filepath.Join("AppData", "claude-launcher-ng2") {
		t.Errorf("windows : %s", d)
	}
	if d := cacheRoot("linux", "", "/home/moi", "x"); d != filepath.Join("/home/moi", ".cache") {
		t.Errorf("cache linux : %s", d)
	}
	if d := cacheRoot("linux", "/xdg", "/home/moi", ""); d != "/xdg" {
		t.Errorf("cache xdg : %s", d)
	}
	if d := cacheRoot("windows", "", `C:\Users\moi`, "LocalAppData"); d != "LocalAppData" {
		t.Errorf("cache windows : %s", d)
	}
	if a := editorArgv("linux", "", "/c.conf"); strings.Join(a, "|") != `sh|-c|nano "$1"|_|/c.conf` {
		t.Errorf("éditeur linux : %q", a)
	}
	if a := editorArgv("windows", "", `C:\c.conf`); strings.Join(a, "|") != `notepad|C:\c.conf` {
		t.Errorf("éditeur windows : %q", a)
	}
	if a := editorArgv("windows", "code --wait", `C:\c.conf`); strings.Join(a, "|") != `code|--wait|C:\c.conf` {
		t.Errorf("éditeur windows : %q", a)
	}
}
