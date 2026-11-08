package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

// Contrat avec le template : le panneau milestone des projets lit (et alimente)
// ~/.cache/claude-launcher/usage.json (et usage.json.err) — chemin partagé avec
// le launcher v4, PAS dans le cache propre à NG2. Seul ce fichier y reste.
func usageCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claude-launcher", "usage.json")
}

// ── Cache partagé entre hubs et conteneurs ─────────────────────────────────
//
// ~/.cache/claude-launcher n'est partagé que sur une même machine : le $HOME d'un
// conteneur lui est propre, si bien que le panneau milestone de chaque projet en
// conteneur appelait l'endpoint de son côté, sans voir le cache ni les 429 des
// autres (appels cumulés → 429 sans fin). Le dossier devient un lien vers
// <config claude>/claude-launcher-cache : la config claude est montée au même
// chemin dans tous les conteneurs, et l'endpoint est propre au compte, donc à
// cette config. Template inchangé (il suit le lien).

// sharedUsageDir : dossier du cache de consommation commun à tous.
func sharedUsageDir() string { return filepath.Join(claudeDir(), "claude-launcher-cache") }

// linkUsageCache : ~/.cache/claude-launcher → sharedUsageDir (sur la machine du
// hub). Un dossier existant n'est remplacé que s'il ne contient que le cache de
// consommation (usage.json*).
func linkUsageCache() {
	link := filepath.Dir(usageCachePath())
	target := sharedUsageDir()
	if os.MkdirAll(target, 0o755) != nil {
		return
	}
	if t, err := os.Readlink(link); err == nil {
		if t == target {
			return
		}
		_ = os.Remove(link)
	} else if es, err := os.ReadDir(link); err == nil {
		for _, e := range es {
			if !strings.HasPrefix(e.Name(), "usage.json") {
				return // autre chose que le cache : on n'y touche pas
			}
		}
		// données encore utiles : reprises si le cache commun n'en a pas
		if _, err := os.Stat(filepath.Join(target, "usage.json")); err != nil {
			_ = os.Rename(filepath.Join(link, "usage.json"), filepath.Join(target, "usage.json"))
		}
		_ = os.RemoveAll(link)
	}
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	_ = os.Symlink(target, link)
}

// linkUsageScript : même lien, dans un conteneur (en utilisateur, à chaque ouverture).
// Un conteneur local créé avant la v5.4 monte ~/.cache/claude-launcher : ce
// montage, qui désigne déjà le dossier commun (-ef), est laissé tel quel.
func linkUsageScript() string {
	return `d="$HOME/.cache/claude-launcher"; t=` + shq(sharedUsageDir()) + `
mkdir -p "$HOME/.cache" "$t"
# déjà le dossier commun (lien, ou montage d'un conteneur local créé avant la v5.4) : rien
if [ "$(readlink "$d" 2>/dev/null)" != "$t" ] && ! [ "$d" -ef "$t" ]; then rm -rf "$d"; ln -sfn "$t" "$d"; fi
`
}

type Limit struct {
	Kind     string  `json:"kind"`
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	Severity string  `json:"severity"`
	ResetsAt string  `json:"resets_at"`
	Scope    struct {
		Surface string `json:"surface"`
		Model   struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

func (l Limit) Label() string {
	switch l.Kind {
	case "session":
		return "Session"
	case "weekly_all":
		return "Semaine"
	}
	s := l.Scope.Model.DisplayName
	if s == "" {
		s = l.Scope.Model.ID
	}
	if s == "" {
		s = l.Scope.Surface
	}
	weekly := l.Kind == "weekly_scoped" || l.Group == "weekly"
	switch {
	case weekly && s != "":
		return s + " sem."
	case weekly:
		return "Semaine (autre)"
	case s != "":
		return s + " " + l.Kind
	}
	return l.Kind
}

func (l Limit) rank() int {
	switch l.Kind {
	case "session":
		return 0
	case "weekly_all":
		return 1
	case "weekly_scoped":
		return 2
	}
	return 9
}

// Usage est l'état affiché : contenu du cache + éventuel message d'échec.
type Usage struct {
	Limits  []Limit
	ReadAt  time.Time // mtime du cache
	Err     string
	Loading bool
	Expired bool      // connexion claude.ai expirée ou refusée : un /login s'impose
	LoginBy time.Time // fin de validité de la connexion, si elle approche (alerte)
}

// loginWarn : délai avant expiration de la connexion à partir duquel on prévient.
const loginWarn = 3 * 24 * time.Hour

// loginExpiry : fin de validité de la connexion claude.ai. Le jeton d'accès ne dure
// que quelques heures et claude le renouvelle seul ; c'est l'expiration du jeton de
// renouvellement (~30 jours) qui impose un /login. Zéro si inconnue (clé API…).
func loginExpiry() time.Time {
	b, err := os.ReadFile(credentialsPath())
	if err != nil {
		return time.Time{}
	}
	var creds struct {
		O struct {
			ExpiresAt        int64 `json:"expiresAt"`
			RefreshExpiresAt int64 `json:"refreshTokenExpiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &creds) != nil {
		return time.Time{}
	}
	ms := creds.O.RefreshExpiresAt
	if ms == 0 {
		ms = creds.O.ExpiresAt
	}
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// authErr : message d'échec écrit par fetchUsage sur un HTTP 401/403.
const authErr = "non authentifié — lancer claude puis /login"

func readUsage() Usage {
	path := usageCachePath()
	var u Usage
	if b, err := os.ReadFile(path + ".err"); err == nil {
		u.Err = strings.TrimSpace(string(b))
	}
	if exp := loginExpiry(); !exp.IsZero() && time.Until(exp) < loginWarn {
		u.LoginBy = exp
	}
	u.Expired = u.Err == authErr || (!u.LoginBy.IsZero() && !time.Now().Before(u.LoginBy))
	st, err := os.Stat(path)
	if err != nil {
		u.Loading = u.Err == ""
		return u
	}
	u.ReadAt = st.ModTime()
	b, _ := os.ReadFile(path)
	var doc struct {
		Limits []Limit `json:"limits"`
	}
	if json.Unmarshal(b, &doc) == nil {
		for _, l := range doc.Limits {
			if l.Kind != "" {
				u.Limits = append(u.Limits, l)
			}
		}
	}
	sort.SliceStable(u.Limits, func(i, j int) bool {
		a, b := u.Limits[i], u.Limits[j]
		if a.rank() != b.rank() {
			return a.rank() < b.rank()
		}
		return a.Label() < b.Label()
	})
	return u
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

// needsFetch applique la politique du bash : pas d'appel si le cache est frais
// (< 1,5 × intervalle) ou si un échec récent suspend les appels.
func needsFetch(refreshSec int) bool {
	path := usageCachePath()
	interval := time.Duration(refreshSec) * time.Second
	backoff := 2 * interval
	if backoff < 5*time.Minute {
		backoff = 5 * time.Minute
	}
	// Échec récent (sa date peut être dans le futur : attente prolongée après un
	// 429). Un /login réécrit les identifiants : l'échec ne vaut plus, on réessaie
	// — sauf après un 429 (claude renouvelle seul son jeton toutes
	// les quelques heures, ce qui lèverait l'attente).
	if st, err := os.Stat(path + ".err"); err == nil && time.Since(st.ModTime()) < backoff {
		b, _ := os.ReadFile(path + ".err")
		cs, cerr := os.Stat(credentialsPath())
		if strings.HasPrefix(string(b), "trop d") || cerr != nil || !cs.ModTime().After(st.ModTime()) {
			return false
		}
	}
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < interval*3/2 {
		return false
	}
	return true
}

// fetchUsage interroge l'endpoint et met à jour le cache de façon atomique.
func fetchUsage(refreshSec int) {
	path := usageCachePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	fail := func(msg string) { _ = os.WriteFile(path+".err", []byte(msg), 0o644) }

	cb, err := os.ReadFile(credentialsPath())
	var creds struct {
		O struct {
			Token string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err != nil || json.Unmarshal(cb, &creds) != nil || creds.O.Token == "" {
		fail("pas de connexion claude.ai (clé API ?) — lancer claude puis /login")
		return
	}
	req, _ := http.NewRequest("GET", usageURL, nil)
	req.Header.Set("Authorization", "Bearer "+creds.O.Token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		fail("endpoint indisponible")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == 200 && json.Valid(body) && strings.Contains(string(body), `"limits"`):
		tmp := fmt.Sprintf("%s.%d", path, os.Getpid())
		if os.WriteFile(tmp, body, 0o644) == nil && os.Rename(tmp, path) == nil {
			_ = os.Remove(path + ".err")
			_ = os.Remove(path + ".wait")
		}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		fail(authErr)
	case resp.StatusCode == 429:
		wait := tooMany(path, resp.Header.Get("Retry-After"))
		fail("trop d’appels à l’endpoint, nouvel essai vers " + time.Now().Add(wait).Format("15:04"))
		// .err daté de façon que l'attente commune (5 min après sa date, pour le
		// launcher comme pour le panneau du template) finisse dans « wait »
		at := time.Now().Add(wait - usageMinBackoff)
		_ = os.Chtimes(path+".err", at, at)
	default:
		fail(fmt.Sprintf("endpoint indisponible (HTTP %d)", resp.StatusCode))
	}
}

// openLogin ouvre dans la session des projets une fenêtre « login » qui lance
// claude /login ; elle se ferme quand on quitte claude.
func openLogin(session string) error {
	sess := targetSession(session)
	if tmuxWindows(sess)["login"] {
		return dtmux("select-window", "-t", exactW(sess, "login")).Run()
	}
	return dtmux("new-window", "-t", exactS(sess)+":", "-n", "login", claudeBin()+" /login").Run()
}

var jours = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}

// formatReset : « 42 min », « 15:00 », « demain 12:00 », « mer. 12:00 ».
func formatReset(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	t = t.Add(30 * time.Second).Truncate(time.Minute).Local()
	now := time.Now()
	if !t.After(now) {
		return "passée"
	}
	diff := int(math.Round(t.Sub(now).Minutes()))
	switch {
	case diff <= 0:
		return "imminent"
	case diff < 60:
		return fmt.Sprintf("%d min", diff)
	}
	hm := t.Format("15:04")
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return hm
	case t.YearDay() == now.AddDate(0, 0, 1).YearDay() && y1 == now.AddDate(0, 0, 1).Year():
		return "demain " + hm
	}
	return jours[t.Weekday()] + " " + hm
}

// usageMinBackoff : pause minimale après un échec (launcher et panneau du template).
const usageMinBackoff = 5 * time.Minute

// usageMaxWait : attente maximale entre deux essais après des 429 successifs.
var usageMaxWait = 30 * time.Minute

// tooMany : attente après un 429 — Retry-After s'il est donné (secondes), sinon
// le double de la précédente (5 min, 10, 20, puis 30 au plus) ; notée dans
// <cache>.wait, effacée au premier succès.
func tooMany(path, retryAfter string) time.Duration {
	wait := usageMinBackoff
	if n, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && n > 0 {
		wait = time.Duration(n) * time.Second
	} else if b, err := os.ReadFile(path + ".wait"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			wait = 2 * time.Duration(n) * time.Second
		}
	}
	if wait < usageMinBackoff {
		wait = usageMinBackoff
	}
	if wait > usageMaxWait {
		wait = usageMaxWait
	}
	_ = os.WriteFile(path+".wait", []byte(strconv.Itoa(int(wait.Seconds()))), 0o644)
	return wait
}
