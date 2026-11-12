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
	LauncherRepo = "CCoupel/Claude-Launcher"
	TemplateRepo = "CCoupel/claude_project_template"
)

const NgName = "Claude-Launcher-NG2"

// ngVersion : version du launcher NG, surchargée au build :
//
//	go build -ldflags "-X main.ngVersion=v3.0.1"
//
// Les alertes de mise à jour la comparent au dernier tag de CCoupel/Claude-Launcher.
// Sans -X (build à la main), « dev » : pas de mise à jour automatique.
var NgVersion = "dev"

type (
	UpdTickMsg struct{}
	UpdMsg     struct {
		Ok            bool
		LauncherTag   string
		LauncherReady bool   // release publiée (SHA256SUMS et binaire listés par l'API)
		TplTag        string // tag du template plus récent que le cache, pas encore adopté
		TplSha        string
		TplReady      bool // release du template publiée : adoptable d'un clic
		Manual        bool // demandée par F5 : hors du cycle de 10 min
	}
	TplAdoptedMsg struct {
		Tag    string
		Synced bool // init-project.md téléchargé
	}
)

func UpdTickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return UpdTickMsg{} })
}

func cacheBase() string { return filepath.Dir(scriptPath()) }

func TplCommitPath() string  { return filepath.Join(cacheBase(), ".template-commit") }
func TplVersionPath() string { return filepath.Join(cacheBase(), ".template-version") }

func ReadTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var scriptVerRe = regexp.MustCompile(`^SCRIPT_VERSION="([^"]+)"`)

// localLauncherVersion : SCRIPT_VERSION de ~/claude-launcher.sh (redirection).
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
func VersionNewer(a, b string) bool {
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
var ApiBase = func() string {
	if b := os.Getenv("CL_API_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://api.github.com"
}()

func firstTag(repo string) (name, sha string, err error) {
	return pickTag(repo, func([]tagRef) int { return 0 })
}

type tagRef struct {
	Name   string `json:"name"`
	Commit struct {
		Sha string `json:"sha"`
	} `json:"commit"`
}

// tplTag : dernier tag du template dont la version majeure ne dépasse pas
// celle du launcher (template et launcher partagent leur majeure depuis la v5 :
// un template d'une majeure supérieure suppose un launcher plus récent).
// CL_FAKE_LATEST_TPL le remplace, pour les tests.
func tplTag() (name, sha string, err error) {
	if v := os.Getenv("CL_FAKE_LATEST_TPL"); v != "" {
		return v, "fake-" + v, nil
	}
	return pickTag(TemplateRepo, func(tags []tagRef) int { return bestTagUpTo(tags, verParts(NgVersion)[0]) })
}

// bestTagUpTo : index du tag vX.Y.Z le plus élevé avec X ≤ major (-1 : aucun).
// major 0 (version « dev », compilée sans -ldflags) : pas de plafond.
func bestTagUpTo(tags []tagRef, major int) int {
	best := -1
	for i, t := range tags {
		if !strings.HasPrefix(t.Name, "v") || (major > 0 && verParts(t.Name)[0] > major) {
			continue
		}
		if best < 0 || VersionNewer(tags[best].Name, t.Name) {
			best = i
		}
	}
	return best
}

func pickTag(repo string, choose func([]tagRef) int) (name, sha string, err error) {
	req, _ := http.NewRequest("GET", ApiBase+"/repos/"+repo+"/tags?per_page=100", nil)
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
	var tags []tagRef
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil || len(tags) == 0 {
		return "", "", fmt.Errorf("pas de tag")
	}
	i := choose(tags)
	if i < 0 {
		return "", "", fmt.Errorf("pas de tag compatible")
	}
	return tags[i].Name, tags[i].Commit.Sha, nil
}

// checkUpdates interroge GitHub. Un nouveau tag du template n'est pas adopté
// d'office : il l'est d'un clic (adoptTplCmd), sauf sans cache (première
// installation).
func CheckUpdates(manual bool) tea.Cmd {
	return func() tea.Msg {
		out := UpdMsg{Manual: manual}
		if tag, _, err := latestTag(LauncherRepo, "CL_FAKE_LATEST_LAUNCHER"); err == nil {
			out.LauncherTag, out.Ok = tag, true
			if VersionNewer(NgVersion, tag) {
				out.LauncherReady = ReleasePublished(tag)
			}
		}
		if tag, sha, err := tplTag(); err == nil {
			out.Ok = true
			switch cached := ReadTrim(TplCommitPath()); {
			case sha == "" || sha == cached:
			case cached == "":
				adoptTemplate(tag, sha)
			default:
				out.TplTag, out.TplSha = tag, sha
				out.TplReady = tplReleased(tag)
			}
		}
		// init-project.md : à chaque démarrage (comme le sync_init_project du bash),
		// au commit adopté
		if c := ReadTrim(TplCommitPath()); !InitSynced && c != "" && syncInitProject(c) {
			InitSynced = true
		}
		return out
	}
}

// adoptTemplate : les caches partagés avec le bash (.template-commit /
// .template-version) passent au tag, et init-project.md est resynchronisé ;
// les badges ⚠ des projets en retard en découlent.
func adoptTemplate(tag, sha string) (synced bool) {
	_ = os.WriteFile(TplCommitPath(), []byte(sha), 0o644)
	_ = os.WriteFile(TplVersionPath(), []byte(tag), 0o644)
	if syncInitProject(sha) {
		InitSynced = true
		return true
	}
	return false
}

// adoptTplCmd : adoption lancée d'un clic sur l'annonce verte du template.
func AdoptTplCmd(tag, sha string) tea.Cmd {
	return func() tea.Msg { return TplAdoptedMsg{Tag: tag, Synced: adoptTemplate(tag, sha)} }
}

// tplReleased : la release GitHub du tag de template est-elle publiée ?
// CL_FAKE_TPL_RELEASED=<tag> la simule, pour les tests.
func tplReleased(tag string) bool {
	if v := os.Getenv("CL_FAKE_TPL_RELEASED"); v != "" {
		return v == tag
	}
	req, _ := http.NewRequest("GET", ApiBase+"/repos/"+TemplateRepo+"/releases/tags/"+tag, nil)
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
var RawBase = func() string {
	if b := os.Getenv("CL_RAW_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://raw.githubusercontent.com"
}()

// initSynced : init-project.md a été resynchronisé depuis le démarrage.
var InitSynced bool

func InitProjectPath() string { return filepath.Join(cacheBase(), "init-project.md") }

// syncInitProject télécharge init-project.md au commit taggé du template et le
// dépose à côté du script (là où le lancement d'un projet le copie dans
// .claude/commands/). Le cache n'est écrasé qu'en cas de réponse non vide : hors
// ligne, l'ancienne version est conservée. Le script bash le faisait à chaque
// lancement ; avec la redirection vers le NG, c'est maintenant le NG.
func syncInitProject(ref string) bool {
	url := RawBase + "/" + TemplateRepo + "/" + ref + "/init-project.md"
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
	tmp := InitProjectPath() + ".tmp"
	if os.WriteFile(tmp, body, 0o600) != nil {
		return false
	}
	return os.Rename(tmp, InitProjectPath()) == nil
}

// latestTag : dernier tag GitHub. La variable d'environnement envKey (ex.
// CL_FAKE_LATEST_TPL=v9.9.9) simule une nouvelle version, pour les tests.
func latestTag(repo, envKey string) (name, sha string, err error) {
	if v := os.Getenv(envKey); v != "" {
		return v, "fake-" + v, nil
	}
	return firstTag(repo)
}

func OrQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
