package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Versions et mises à jour : même logique que le menu bash (tags GitHub du
// launcher et du template, comparaison du template sur le commit du dernier
// tag), mais ici le Go se contente de SIGNALER — il ne se met pas à jour.

const (
	launcherRepo = "CCoupel/Claude-Launcher"
	templateRepo = "CCoupel/claude_project_template"
)

const ngName = "Claude-Launcher-NG"

// ngVersion : version du launcher NG, surchargée au build :
//
//	go build -ldflags "-X main.ngVersion=v3.0.1"
//
// Les alertes de mise à jour la comparent au dernier tag de CCoupel/Claude-Launcher.
var ngVersion = "v3.0.0"

type (
	updTickMsg struct{}
	updMsg     struct {
		ok          bool
		launcherTag string
		tplTag      string
		tplChanged  bool // nouveau tag du template détecté (cache réécrit)
	}
)

func updTickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return updTickMsg{} })
}

func cacheBase() string { return filepath.Dir(scriptPath()) }

func tplCommitPath() string  { return filepath.Join(cacheBase(), ".template-commit") }
func tplVersionPath() string { return filepath.Join(cacheBase(), ".template-version") }

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var scriptVerRe = regexp.MustCompile(`^SCRIPT_VERSION="([^"]+)"`)

// localLauncherVersion : version du launcher déployé (SCRIPT_VERSION du script bash).
func localLauncherVersion() string {
	f, err := os.Open(scriptPath())
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < 200 && sc.Scan(); i++ {
		if m := scriptVerRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return ""
}

// versionNewer : b est-il strictement plus récent que a ? (vX.Y.Z)
func versionNewer(a, b string) bool {
	pa, pb := verParts(a), verParts(b)
	for i := 0; i < 3; i++ {
		if pb[i] != pa[i] {
			return pb[i] > pa[i]
		}
	}
	return false
}

func verParts(v string) [3]int {
	var out [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3) {
		n, _ := strconv.Atoi(strings.TrimFunc(p, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

func firstTag(repo string) (name, sha string, err error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/tags", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var tags []struct {
		Name   string `json:"name"`
		Commit struct {
			Sha string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil || len(tags) == 0 {
		return "", "", fmt.Errorf("pas de tag")
	}
	return tags[0].Name, tags[0].Commit.Sha, nil
}

// checkUpdates interroge GitHub. Si le template a un nouveau tag, les caches
// partagés avec le bash (.template-commit / .template-version) sont mis à jour :
// les badges ✓/⚠ des deux versions restent ainsi cohérents.
func checkUpdates() tea.Cmd {
	return func() tea.Msg {
		var out updMsg
		if tag, _, err := latestTag(launcherRepo, "CL_FAKE_LATEST_LAUNCHER"); err == nil {
			out.launcherTag, out.ok = tag, true
		}
		if tag, sha, err := latestTag(templateRepo, "CL_FAKE_LATEST_TPL"); err == nil {
			out.tplTag, out.ok = tag, true
			changed := sha != "" && sha != readTrim(tplCommitPath())
			if changed {
				_ = os.WriteFile(tplCommitPath(), []byte(sha), 0o644)
				_ = os.WriteFile(tplVersionPath(), []byte(tag), 0o644)
				out.tplChanged = true
			}
			// init-project.md : à chaque démarrage (comme le sync_init_project du bash)
			// puis à chaque nouveau tag du template
			if sha != "" && (!initSynced || changed) && syncInitProject(sha) {
				initSynced = true
			}
		}
		return out
	}
}

// rawBase : serveur des fichiers bruts GitHub (CL_RAW_BASE le remplace, pour les tests).
var rawBase = func() string {
	if b := os.Getenv("CL_RAW_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://raw.githubusercontent.com"
}()

// initSynced : init-project.md a été resynchronisé depuis le démarrage.
var initSynced bool

func initProjectPath() string { return filepath.Join(cacheBase(), "init-project.md") }

// syncInitProject télécharge init-project.md au commit taggé du template et le
// dépose à côté du script (là où le lancement d'un projet le copie dans
// .claude/commands/). Le cache n'est écrasé qu'en cas de réponse non vide : hors
// ligne, l'ancienne version est conservée. Le script bash le faisait à chaque
// lancement ; avec la redirection vers le NG, c'est maintenant le NG.
func syncInitProject(ref string) bool {
	url := rawBase + "/" + templateRepo + "/" + ref + "/init-project.md"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return false
	}
	tmp := initProjectPath() + ".tmp"
	if os.WriteFile(tmp, body, 0o600) != nil {
		return false
	}
	return os.Rename(tmp, initProjectPath()) == nil
}

// latestTag : dernier tag GitHub. La variable d'environnement envKey (ex.
// CL_FAKE_LATEST_TPL=v9.9.9) simule une nouvelle version, pour les tests.
func latestTag(repo, envKey string) (name, sha string, err error) {
	if v := os.Getenv(envKey); v != "" {
		return v, "fake-" + v, nil
	}
	return firstTag(repo)
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
