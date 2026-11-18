package core

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
	"sync"
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

// NgVersion : version du launcher NG, surchargée au build :
//
//	go build -ldflags "-X claude-launcher-ng2/core.NgVersion=v3.0.1"
//
// Les alertes de mise à jour la comparent au dernier tag de CCoupel/Claude-Launcher.
// Sans -X (build à la main), « dev » : pas de mise à jour automatique.
var NgVersion = "dev"

// ngVersionMu : NgVersion est lue par des goroutines (vérifications de mise à jour, serveurs de fichiers, instantanés)
// pendant que des tests la remplacent : tout accès passe par Version / SetNgVersion (course de données sous -race).
// Seul le -X du lien (avant main) l'écrit sans verrou.
var ngVersionMu sync.RWMutex

// Version : version du launcher NG (NgVersion), lecture sûre.
func Version() string {
	ngVersionMu.RLock()
	defer ngVersionMu.RUnlock()
	return NgVersion
}

// SetNgVersion : remplace la version (tests) ; rend l'ancienne.
func SetNgVersion(v string) (old string) {
	ngVersionMu.Lock()
	defer ngVersionMu.Unlock()
	old, NgVersion = NgVersion, v
	return old
}

type (
	UpdTickMsg struct{}
	UpdMsg     struct {
		Ok            bool
		LauncherTag   string
		LauncherReady bool   // release publiée (SHA256SUMS et binaire listés par l'API)
		TplTag        string // tag du template plus récent que le cache, pas encore adopté
		TplSha        string
		TplReady      bool      // release du template publiée : adoptable d'un clic
		Manual        bool      // demandée par F5 : hors du cycle de 10 min
		Err           string    // "" | "quota" | "network" (#199) : alerte visible en continu
		RetryAt       time.Time // reprise annoncée quand Err == "quota" (X-RateLimit-Reset)
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

// LatestTemplateKnown : dernière version publiée du template connue du poste (résultat de la vérification
// partagé, à défaut la version adoptée) ; "" si inconnue.
func LatestTemplateKnown() string {
	if t := readUpdateCheck().Template.Tag; t != "" {
		if adopted := ReadTrim(TplVersionPath()); adopted != "" && VersionNewer(t, adopted) {
			return adopted
		}
		return t
	}
	return ReadTrim(TplVersionPath())
}

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

// VersionNewer : b est-il strictement plus récent que a ? (vX.Y.Z)
func VersionNewer(a, b string) bool {
	pa, pb := verParts(a), verParts(b)
	for i := 0; i < 3; i++ {
		if pb[i] != pa[i] {
			return pb[i] > pa[i]
		}
	}
	return false
}

// verParts : au plus 4 champs X.Y.Z.a ; seuls les trois premiers comptent dans VersionNewer (« 5.11.1.3 » → [5 11 1 3]).
func verParts(v string) [4]int {
	var out [4]int
	for i, p := range strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 4) {
		n, _ := strconv.Atoi(strings.TrimFunc(p, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

// ApiBase : API GitHub (CL_API_BASE la remplace, pour les tests).
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

// tplChoose : choisit le dernier tag du template dont la version majeure ne dépasse pas
// celle du launcher (template et launcher partagent leur majeure depuis la v5 :
// un template d'une majeure supérieure suppose un launcher plus récent).
func tplChoose(tags []tagRef) int { return bestTagUpTo(tags, verParts(Version())[0]) }

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
	name, sha, _, _, err = pickTagCond(repo, "", choose)
	return name, sha, err
}

// pickTagCond : liste des tags avec requête conditionnelle (etag mémorisé). notMod : 304, rien n'est lu (le
// résultat mémorisé reste valable). Les échecs sont des *ghError (quota, réseau).
func pickTagCond(repo, etag string, choose func([]tagRef) int) (name, sha, newEtag string, notMod bool, err error) {
	req, _ := http.NewRequest("GET", ApiBase+"/repos/"+repo+"/tags?per_page=100", nil)
	ghHeaders(req)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "", "", "", false, &ghError{Kind: UpdErrNetwork, Msg: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && etag != "" {
		return "", "", etag, true, nil
	}
	if qe := quotaError(resp); qe != nil {
		return "", "", "", false, qe
	}
	if resp.StatusCode != 200 {
		return "", "", "", false, &ghError{Kind: UpdErrNetwork, Msg: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	var tags []tagRef
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil || len(tags) == 0 {
		return "", "", "", false, &ghError{Kind: UpdErrNetwork, Msg: "pas de tag"}
	}
	i := choose(tags)
	if i < 0 {
		return "", "", "", false, &ghError{Kind: UpdErrNetwork, Msg: "pas de tag compatible"}
	}
	return tags[i].Name, tags[i].Commit.Sha, resp.Header.Get("ETag"), false, nil
}

// CheckUpdates interroge GitHub. Un nouveau tag du template n'est pas adopté
// d'office : il l'est d'un clic (adoptTplCmd), sauf sans cache (première
// installation).
func CheckUpdates(manual bool) tea.Cmd {
	return func() tea.Msg { return CheckUpdatesNow(manual) }
}

// CheckUpdatesNow : la recherche elle-même, sans bubbletea (interface native). Résultat partagé par poste
// (update-check.json, 10 min), requêtes conditionnelles, pause jusqu'à la reprise quand le quota est épuisé (#199).
func CheckUpdatesNow(manual bool) UpdMsg {
	out := UpdMsg{Manual: manual}
	fake := os.Getenv("CL_FAKE_LATEST_TPL") != "" || os.Getenv("CL_FAKE_LATEST_LAUNCHER") != ""
	prev := readUpdateCheck()
	if !fake {
		if until := prev.retryAt(); prev.Error == UpdErrQuota && time.Now().Before(until) {
			out.Err, out.RetryAt = UpdErrQuota, until // aucun appel avant la reprise, F5 compris
			ensureInitSynced()
			return out
		}
		if at, err := time.Parse(time.RFC3339, prev.CheckedAt); !manual && err == nil && prev.Error == "" &&
			time.Since(at) < updateCheckTTL && prev.Launcher.Tag != "" {
			fillUpd(&out, prev) // un autre processus vient de vérifier
			ensureInitSynced()
			return out
		}
	}
	cur := prev
	cur.Error, cur.RetryAfter = "", ""
	var failed *ghError
	fail := func(err error) {
		if ge, ok := err.(*ghError); ok && failed == nil {
			failed = ge
		}
	}

	// launcher
	if v := os.Getenv("CL_FAKE_LATEST_LAUNCHER"); v != "" {
		cur.Launcher = updRepo{Tag: v, Sha: "fake-" + v, Released: cur.Launcher.Released}
	} else if tag, sha, et, notMod, err := pickTagCond(LauncherRepo, prev.Launcher.Etag, func([]tagRef) int { return 0 }); err != nil {
		fail(err)
	} else if !notMod {
		cur.Launcher = updRepo{Tag: tag, Sha: sha, Etag: et}
	}
	if failed == nil && VersionNewer(Version(), cur.Launcher.Tag) && (!cur.Launcher.Released || cur.Launcher.Tag != prev.Launcher.Tag) {
		cur.Launcher.Released = ReleasePublished(cur.Launcher.Tag)
	}

	// template
	if failed == nil {
		if v := os.Getenv("CL_FAKE_LATEST_TPL"); v != "" {
			cur.Template = updRepo{Tag: v, Sha: "fake-" + v}
		} else if tag, sha, et, notMod, err := pickTagCond(TemplateRepo, prev.Template.Etag, tplChoose); err != nil {
			fail(err)
		} else if !notMod {
			cur.Template = updRepo{Tag: tag, Sha: sha, Etag: et}
		}
		if failed == nil && cur.Template.Tag != "" && (!prev.Template.Released || cur.Template.Tag != prev.Template.Tag) {
			cur.Template.Released = tplReleased(cur.Template.Tag)
		} else if cur.Template.Tag == prev.Template.Tag {
			cur.Template.Released = prev.Template.Released
		}
	}

	if failed != nil {
		cur.Error = failed.Kind
		out.Err = failed.Kind
		if failed.Kind == UpdErrQuota {
			cur.RetryAfter = failed.Retry.UTC().Format(time.RFC3339)
			out.RetryAt = failed.Retry
		}
		if !fake {
			writeUpdateCheck(cur)
		}
		ensureInitSynced()
		return out
	}
	cur.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	if !fake {
		writeUpdateCheck(cur)
	}
	fillUpd(&out, cur)
	ensureInitSynced()
	return out
}

// fillUpd : annonces déduites d'un résultat de vérification (frais ou partagé). Le template n'est annoncé que si
// sa version est strictement plus récente que la version adoptée ; sans cache, première installation : adoption.
func fillUpd(out *UpdMsg, c updCheck) {
	out.Ok = true
	out.LauncherTag = c.Launcher.Tag
	if VersionNewer(Version(), c.Launcher.Tag) {
		out.LauncherReady = c.Launcher.Released
	}
	tag, sha := c.Template.Tag, c.Template.Sha
	if tag == "" {
		return
	}
	cached, adopted := ReadTrim(TplCommitPath()), ReadTrim(TplVersionPath())
	switch {
	case sha == "" || sha == cached:
	case cached == "":
		adoptTemplate(tag, sha)
	case adopted != "" && !VersionNewer(adopted, tag):
		// jamais d'annonce d'une cible égale ou plus ancienne que la version adoptée (#199)
	default:
		out.TplTag, out.TplSha = tag, sha
		out.TplReady = c.Template.Released
	}
}

// adoptTemplate : les caches partagés avec le bash (.template-commit /
// .template-version) passent au tag, et init-project.md est resynchronisé ;
// les badges ⚠ des projets en retard en découlent.
func adoptTemplate(tag, sha string) (synced bool) {
	_ = os.WriteFile(TplCommitPath(), []byte(sha), 0o644)
	_ = os.WriteFile(TplVersionPath(), []byte(tag), 0o644)
	if syncInitProject(sha) {
		initSyncedSet()
		return true
	}
	return false
}

// AdoptTplCmd : adoption lancée d'un clic sur l'annonce verte du template.
func AdoptTplCmd(tag, sha string) tea.Cmd {
	return func() tea.Msg { return AdoptTemplate(tag, sha) }
}

// AdoptTemplate : l'adoption elle-même, sans bubbletea (interface native).
func AdoptTemplate(tag, sha string) TplAdoptedMsg {
	return TplAdoptedMsg{Tag: tag, Synced: adoptTemplate(tag, sha)}
}

// tplReleased : la release GitHub du tag de template est-elle publiée ?
// CL_FAKE_TPL_RELEASED=<tag> la simule, pour les tests.
func tplReleased(tag string) bool {
	if v := os.Getenv("CL_FAKE_TPL_RELEASED"); v != "" {
		return v == tag
	}
	req, err := http.NewRequest("GET", ApiBase+"/repos/"+TemplateRepo+"/releases/tags/"+tag, nil)
	if err != nil {
		return false
	}
	ghHeaders(req)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// RawBase : serveur des fichiers bruts GitHub (CL_RAW_BASE le remplace, pour les tests).
var RawBase = func() string {
	if b := os.Getenv("CL_RAW_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "https://raw.githubusercontent.com"
}()

// SetRawBase : remplace RawBase sous initMu (tests) — une vérification de mise à jour encore en cours ne lit jamais
// RawBase pendant qu'il change (course de données, #187) ; rend l'ancienne valeur.
func SetRawBase(v string) (old string) {
	initMu.Lock()
	defer initMu.Unlock()
	old, RawBase = RawBase, v
	return old
}

// InitSynced : init-project.md a été resynchronisé depuis le démarrage. Lu et écrit par des
// vérifications de mises à jour concurrentes (F5, tour périodique) : seulement par
// initSyncedGet / initSyncedSet (initMu) ; les tests la remplacent à l'arrêt de toute vérification.
var InitSynced bool

// initMu protège InitSynced et sérialise syncInitProject (fichier temporaire commun).
var initMu sync.Mutex

func initSyncedGet() bool {
	initMu.Lock()
	defer initMu.Unlock()
	return InitSynced
}

func initSyncedSet() {
	initMu.Lock()
	InitSynced = true
	initMu.Unlock()
}

// ensureInitSynced : init-project.md au commit adopté, une fois par démarrage (comme le
// sync_init_project du bash).
func ensureInitSynced() {
	if c := ReadTrim(TplCommitPath()); c != "" && !initSyncedGet() && syncInitProject(c) {
		initSyncedSet()
	}
}

func InitProjectPath() string { return filepath.Join(cacheBase(), "init-project.md") }

// syncInitProject télécharge init-project.md au commit taggé du template et le
// dépose à côté du script (là où le lancement d'un projet le copie dans
// .claude/commands/). Le cache n'est écrasé qu'en cas de réponse non vide : hors
// ligne, l'ancienne version est conservée. Le script bash le faisait à chaque
// lancement ; avec la redirection vers le NG, c'est maintenant le NG.
func syncInitProject(ref string) bool {
	initMu.Lock() // un seul téléchargement à la fois : le fichier temporaire est commun
	defer initMu.Unlock()
	url := RawBase + "/" + TemplateRepo + "/" + ref + "/init-project.md"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil { // adresse invalide : pas de synchronisation (jamais de panique sur req nil, #187)
		return false
	}
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
