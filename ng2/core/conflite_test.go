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
		// tilde : non quoté, en tête de mot ou après = / : d'une affectation
		"tilde": `GITHUB_DIRS=(~/GITHUB ~ "~/quoté" '~/simple' x~y ~/a:~/b)
GITHUB_TOKEN=~/tok
CLAUDE_OPTIONS=a:~/b:~
GITHUB_DIR=~
EXTRA_ENVS=(~autre/x "A=~/x")
`,
		"tilde dir": `GITHUB_DIR=~/projets`,
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
	if out := confLite("GITHUB_DIRS=(~/GITHUB)", `C:\Users\moi`); !strings.Contains(out, "root\tC:\\Users\\moi/GITHUB\n") {
		t.Errorf("tilde : %s", out)
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

// #165 : sous une racine de base, confLite (Windows, sans bash) propose les mêmes défauts que le script bash.
func TestConfLiteRootBaseDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootBaseVar, root)
	out := confLite("", `C:\Users\moi`)
	if want := "root\t" + filepath.Join(root, "PROJETS") + "\n"; !strings.Contains(out, want) {
		t.Errorf("racine de projets sous la racine de base : %q, attendu %q", out, want)
	}
	if strings.Contains(out, "GITHUB") {
		t.Errorf("la racine de projets reste $HOME/GITHUB : %q", out)
	}
	// un GITHUB_DIR du fichier l'emporte sur le défaut
	if out := confLite(`GITHUB_DIR="/x/projets"`, `C:\Users\moi`); !strings.Contains(out, "root\t/x/projets\n") {
		t.Errorf("GITHUB_DIR du fichier : %q", out)
	}
	// CLAUDE_CONFIG_DIR par défaut : posé par LoadConfig sous la racine
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	if cfg := LoadConfig(); cfg.ClaudeConfigDir != filepath.Join(root, ".claude") {
		t.Errorf("CLAUDE_CONFIG_DIR par défaut : %q, attendu %q", cfg.ClaudeConfigDir, filepath.Join(root, ".claude"))
	}
	// sans la variable : inchangé
	t.Setenv(RootBaseVar, "")
	if out := confLite("", `C:\Users\moi`); !strings.Contains(out, `root`+"\t"+`C:\Users\moi\GITHUB`+"\n") {
		t.Errorf("sans racine de base : %q", out)
	}
}

// Revue LOT-4.1 M4 : le config.conf créé sous une racine de base (« ${CL_ROOT_BASE}/PROJETS ») est relu correctement par
// chaque système — Linux (bash) et Windows (confLite) — avec SA forme de la racine ; jamais un chemin d'un autre système.
func TestRootBaseConfigRelativeResolvedPerSystem(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootBaseVar, root)
	conf := defaultConfigFor()
	if strings.Contains(conf, root) || !strings.Contains(conf, "${CL_ROOT_BASE}/PROJETS") || !strings.Contains(conf, "${CL_ROOT_BASE}/.claude") {
		t.Fatalf("la configuration créée doit être relative à la racine (aucun chemin absolu écrit) :\n%s", conf)
	}
	// « Windows » : confLite, racine sous forme Windows
	t.Setenv(RootBaseVar, `D:\base`)
	out := confLite(conf, `C:\Users\moi`)
	if !strings.Contains(out, "root\tD:\\base/PROJETS\n") || !strings.Contains(out, "claudedir\tD:\\base/.claude\n") {
		t.Errorf("relu par Windows (confLite) : %q", out)
	}
	// « Linux » : le même fichier, racine sous forme Linux, relu par bash (script de LoadConfig)
	if runtime.GOOS == "windows" {
		return
	}
	t.Setenv(RootBaseVar, root)
	cfgDir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", cfgDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadConfig()
	if len(cfg.Roots) != 1 || cfg.Roots[0] != filepath.Join(root, "PROJETS") || cfg.ClaudeConfigDir != filepath.Join(root, ".claude") {
		t.Errorf("relu par Linux : racines %v, config de claude %q", cfg.Roots, cfg.ClaudeConfigDir)
	}
}

// Revue LOT-4.1 M5 : un config.conf créé sous une racine (${CL_ROOT_BASE}) relu par un launcher SANS la variable
// (exe Windows, hub en mode portable) : racine déduite de l'emplacement du fichier — mêmes chemins qu'avec la variable —,
// sans exporter la variable ni changer RootBase() ; hors de cette forme : chemins écartés et signalés, jamais « /PROJETS ».
func TestRootBaseConfigReadWithoutVariable(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootBaseVar, root)
	conf := defaultConfigFor()
	cfgDir := filepath.Join(root, ".config", "claude-launcher-ng2")
	_ = os.MkdirAll(cfgDir, 0o755)
	path := filepath.Join(cfgDir, "config.conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	with := loadConfigAt(path)
	// la même lecture sans la variable
	t.Setenv(RootBaseVar, "")
	os.Unsetenv(RootBaseVar)
	without := loadConfigAt(path)
	if len(without.Roots) != 1 || without.Roots[0] != filepath.Join(root, "PROJETS") || without.ClaudeConfigDir != filepath.Join(root, ".claude") ||
		with.Roots[0] != without.Roots[0] || with.ClaudeConfigDir != without.ClaudeConfigDir {
		t.Errorf("sans variable : racines %v, claude %q ; avec : %v, %q", without.Roots, without.ClaudeConfigDir, with.Roots, with.ClaudeConfigDir)
	}
	if without.RootBaseWarn != "" {
		t.Errorf("avertissement inattendu : %s", without.RootBaseWarn)
	}
	if d, _ := RootBase(); d != "" || os.Getenv(RootBaseVar) != "" {
		t.Errorf("la lecture ne doit ni poser ni exporter CL_ROOT_BASE : %q", d)
	}
	// « Windows » : confLite avec la racine déduite, forme Windows
	if out := confLiteWith(conf, `C:\Users\moi`, `D:\base`); !strings.Contains(out, "root\tD:\\base/PROJETS\n") || !strings.Contains(out, "claudedir\tD:\\base/.claude\n") {
		t.Errorf("confLite avec racine déduite : %q", out)
	}
	// forme qui ne correspond pas : écarté et signalé, pas de « /PROJETS »
	other := filepath.Join(t.TempDir(), "config.conf")
	_ = os.WriteFile(other, []byte(conf), 0o600)
	bad := loadConfigAt(other)
	if bad.RootBaseWarn == "" || strings.Contains(strings.Join(bad.Roots, ","), "NON_RESOLUE") || bad.ClaudeConfigDir != "" {
		t.Errorf("racine non résolue : avertissement %q, racines %v, claude %q", bad.RootBaseWarn, bad.Roots, bad.ClaudeConfigDir)
	}
	for _, r := range bad.Roots {
		if r == "/PROJETS" {
			t.Errorf("« /PROJETS » silencieux : %v", bad.Roots)
		}
	}
}
