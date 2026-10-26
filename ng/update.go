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
// tag). En cours de route le NG se contente de SIGNALER une nouvelle version ;
// la mise à jour elle-même a lieu au lancement (selfupdate.go) ou d'un clic.
//
// Un tag poussé précède sa release de quelques minutes (CI) : tant que la
// release n'est pas publiée, la version est annoncée en orange, non cliquable ;
// une fois publiée, en vert, un clic l'installe (NG) ou l'adopte (template).

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
// Sans -X (build à la main), « dev » : pas de mise à jour automatique.
var ngVersion = "dev"

type (
	updTickMsg struct{}
	updMsg     struct {
		ok            bool
		launcherTag   string
		launcherReady bool   // fichiers de release (SHA256SUMS) disponibles
		tplTag        string // tag du template plus récent que le cache, pas encore adopté
		tplSha        string
		tplReady      bool // release du template publiée : adoptable d'un clic
		manual        bool // demandée par F5 : hors du cycle de 10 min
	}
	tplAdoptedMsg struct {
		tag    string
		synced bool // init-project.md téléchargé
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

// apiBase : API GitHub (CL_API_BASE la remplace, pour les tests).
var apiBase = func() string {
	if b := os.Getenv("CL_API_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://api.github.com"
}()

func firstTag(repo string) (name, sha string, err error) {
	req, _ := http.NewRequest("GET", apiBase+"/repos/"+repo+"/tags", nil)
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

// checkUpdates interroge GitHub. Un nouveau tag du template n'est pas adopté
// d'office : il l'est d'un clic (adoptTplCmd), sauf sans cache (première
// installation).
func checkUpdates(manual bool) tea.Cmd {
	return func() tea.Msg {
		out := updMsg{manual: manual}
		if tag, _, err := latestTag(launcherRepo, "CL_FAKE_LATEST_LAUNCHER"); err == nil {
			out.launcherTag, out.ok = tag, true
			if versionNewer(ngVersion, tag) {
				out.launcherReady = releaseReady(releaseBase + "/" + tag)
			}
		}
		if tag, sha, err := latestTag(templateRepo, "CL_FAKE_LATEST_TPL"); err == nil {
			out.ok = true
			switch cached := readTrim(tplCommitPath()); {
			case sha == "" || sha == cached:
			case cached == "":
				adoptTemplate(tag, sha)
			default:
				out.tplTag, out.tplSha = tag, sha
				out.tplReady = tplReleased(tag)
			}
		}
		// init-project.md : à chaque démarrage (comme le sync_init_project du bash),
		// au commit adopté
		if c := readTrim(tplCommitPath()); !initSynced && c != "" && syncInitProject(c) {
			initSynced = true
		}
		return out
	}
}

// adoptTemplate : les caches partagés avec le bash (.template-commit /
// .template-version) passent au tag, et init-project.md est resynchronisé ;
// les badges ⚠ des projets en retard en découlent.
func adoptTemplate(tag, sha string) (synced bool) {
	_ = os.WriteFile(tplCommitPath(), []byte(sha), 0o644)
	_ = os.WriteFile(tplVersionPath(), []byte(tag), 0o644)
	if syncInitProject(sha) {
		initSynced = true
		return true
	}
	return false
}

// adoptTplCmd : adoption lancée d'un clic sur l'annonce verte du template.
func adoptTplCmd(tag, sha string) tea.Cmd {
	return func() tea.Msg { return tplAdoptedMsg{tag: tag, synced: adoptTemplate(tag, sha)} }
}

// tplReleased : la release GitHub du tag de template est-elle publiée ?
// CL_FAKE_TPL_RELEASED=<tag> la simule, pour les tests.
func tplReleased(tag string) bool {
	if v := os.Getenv("CL_FAKE_TPL_RELEASED"); v != "" {
		return v == tag
	}
	req, _ := http.NewRequest("GET", apiBase+"/repos/"+templateRepo+"/releases/tags/"+tag, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Claude-Launcher-NG")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
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
